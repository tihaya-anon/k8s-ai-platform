#!/usr/bin/env bash
# The full product loop from docs/draft.md: dataset -> training -> artifact -> evaluation ->
# gated promotion -> canary -> stable, with a rollback rehearsal at the end.
set -euo pipefail

cd "$(dirname "$0")/.."
PLAT=${PLAT:-./bin/platform}
SUFFIX=${SUFFIX:-$(date +%H%M%S)}
MODEL=${MODEL:-support-assistant}
export PLATFORM_API=${PLATFORM_API:-http://localhost:8080}
export PLATFORM_GATEWAY=${PLATFORM_GATEWAY:-http://localhost:8081}

step() { printf '\n\033[1;36m==> %s\033[0m\n' "$*"; }

# A NodePort can accept connections a moment before the pod behind it does, so wait for both
# planes to answer before starting.
wait_healthy() {
  for _ in $(seq 1 60); do
    if curl -sf -m 2 "$1/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  echo "timed out waiting for $1" >&2
  return 1
}

step "0. wait for the control plane and gateway"
wait_healthy "$PLATFORM_API"
wait_healthy "$PLATFORM_GATEWAY"

step "1. register a training dataset and a golden evaluation dataset"
$PLAT ctl dataset create -name docs -version "v${SUFFIX}" -owner ml-team -rows 400
$PLAT ctl dataset create -name golden-support -version "v${SUFFIX}" -owner eval-team -rows 200 -golden

step "2. submit a training job"
$PLAT ctl train submit -name "ft-support-${SUFFIX}" -owner ml-team \
  -model "$MODEL" -dataset "docs:v${SUFFIX}" -base base-small -steps 12 -checkpoint-every 4
$PLAT ctl wait -kind training -name "ft-support-${SUFFIX}" -timeout 5m

VERSION=$($PLAT ctl train get -name "ft-support-${SUFFIX}" | sed -n 's/.*"modelRef": "[^:]*:\(v[0-9]*\)".*/\1/p')
step "3. training produced ${MODEL}:${VERSION}"
$PLAT ctl model list

step "4. evaluate it against the golden dataset (promotion gates apply here)"
$PLAT ctl eval run -name "eval-${SUFFIX}" -owner eval-team \
  -model "${MODEL}:${VERSION}" -dataset "golden-support:v${SUFFIX}" -min-accuracy 0.55
$PLAT ctl wait -kind eval -name "eval-${SUFFIX}" -timeout 5m
$PLAT ctl eval list

step "5. start a 20% canary"
$PLAT ctl canary -model "$MODEL" -version "$VERSION" -percent 20
# The gateway holds traffic on stable until the canary has a ready replica. Ask the gateway, not
# the control plane: its route table lags by one refresh interval, and that lag is what decides
# whether a request can be served right now.
echo "waiting for the gateway to see a ready canary replica..."
for _ in $(seq 1 60); do
  ready=$(curl -s "$PLATFORM_GATEWAY/v1/routes" | grep -o '"canaryReady":[0-9]*' | cut -d: -f2 | head -1 || true)
  if [ "${ready:-0}" != "0" ]; then
    break
  fi
  sleep 2
done
# Every gateway replica keeps its own table; give the others one refresh interval to catch up.
sleep 3

step "6. send traffic through the gateway"
$PLAT ctl infer -model "$MODEL" -input "how do I reset my device" -n 40

step "7. promote the canary to stable"
$PLAT ctl promote -model "$MODEL"
$PLAT ctl route list

step "8. platform status"
$PLAT ctl status
