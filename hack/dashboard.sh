#!/usr/bin/env bash
#
# Open a web console for the local cluster (kind or the host's k3s; see hack/lib/cluster.sh):
# Headlamp by default, Grafana when the optional observability stack is installed.
#
# Both consoles need the same two things, and they are the two things this script does: credentials
# to log in with, and a local port forwarded to the Service.
#
# Neither is exposed as a NodePort. A kind node's host ports have to be mapped when the cluster is
# created, and a console on a fixed host port is reachable by anything on the host for as long as
# the cluster runs; a port-forward lives and dies with the process holding it.
#
# That process is detached rather than held in the foreground, because the terminal that starts a
# console is usually the terminal you then want to run kubectl in. The price of detaching is that
# something has to remember the process: its pid and its output go to .run/, and `stop` reads them
# back. A pidfile is only ever a hint -- the pid may have exited, or been reused -- so every read
# of it checks that the live process is still the kubectl port-forward this script started.
#
# Usage:
#   hack/dashboard.sh [headlamp|grafana] [local-port]   start, in the background
#   hack/dashboard.sh stop [headlamp|grafana|all]       stop a background console
#   hack/dashboard.sh status                            what is running

set -euo pipefail

readonly CLUSTER_NAME="k8s-ai-platform"

readonly HEADLAMP_NAMESPACE="headlamp"
readonly HEADLAMP_DEPLOYMENT="headlamp"
readonly HEADLAMP_SERVICE="headlamp"
readonly HEADLAMP_PORT="8080"
readonly TOKEN_DURATION="24h"

readonly OBSERVABILITY_NAMESPACE="observability"
readonly GRAFANA_DEPLOYMENT="kube-prometheus-stack-grafana"
readonly GRAFANA_SERVICE="kube-prometheus-stack-grafana"
readonly GRAFANA_PORT="3000"

readonly WAIT_TIMEOUT="120s"

# How long to watch a freshly started port-forward before declaring it up. kubectl prints
# "Forwarding from ..." once it is listening, and exits within the same moment when the port is
# taken or the Service is gone, so a short poll separates the two without guessing.
readonly FORWARD_READY_TIMEOUT="10"

# Repository-local rather than /tmp: it is the same directory on every shell and in the
# devcontainer, it is gitignored next to .cache/, and a stale file in it is visible to whoever
# wonders why a port is busy.
RUN_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/.run"
readonly RUN_DIR

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

# shellcheck source=lib/cluster.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/cluster.sh"

ensure_cli_tools() {
  command -v kubectl >/dev/null 2>&1 || fail "kubectl is not installed"
}

# Same reasoning as hack/set-up-cluster.sh: name the context rather than trusting whichever one is
# current. Forwarding a port into the wrong cluster is quiet and confusing.
ensure_context() {
  local context
  context="$(cluster_context)"
  kubectl config use-context "$context" >/dev/null 2>&1 \
    || fail "no kubeconfig context ${context}; create the cluster with hack/set-up-cluster.sh"
}

wait_for_deployment() {
  local namespace="$1" deployment="$2" hint="$3"

  kubectl get deployment "$deployment" --namespace "$namespace" >/dev/null 2>&1 \
    || fail "deployment/$deployment is not in namespace $namespace; $hint"

  log "waiting for deployment/$deployment in $namespace"
  kubectl wait --namespace "$namespace" \
    --for=condition=Available "deployment/$deployment" \
    --timeout="$WAIT_TIMEOUT"
}

# Read the account off the Deployment instead of hardcoding it: the name comes from the chart's
# fullname template, so it follows the release name, and a rename should not silently mint a token
# for an account that no longer exists.
service_account_name() {
  local namespace="$1" deployment="$2"

  local name
  name="$(kubectl get deployment "$deployment" --namespace "$namespace" \
    -o jsonpath='{.spec.template.spec.serviceAccountName}')"
  [ -n "$name" ] || fail "deployment/$deployment declares no serviceAccountName"
  printf '%s\n' "$name"
}

# Headlamp holds no accounts of its own. It takes the bearer token pasted at its login prompt and
# calls the API server *as that identity*, so every list and every edit it makes is subject to the
# same RBAC as kubectl. A console that logged you in by itself would be a console with its own idea
# of who you are.
#
# The token comes from the TokenRequest API (`kubectl create token`), not from a Secret: since
# Kubernetes 1.24 a ServiceAccount has no permanent token Secret, and tokens are minted on request,
# audience-scoped and time-limited, leaving nothing durable behind when this one expires.
headlamp_credentials() {
  local account token
  account="$(service_account_name "$HEADLAMP_NAMESPACE" "$HEADLAMP_DEPLOYMENT")"
  token="$(kubectl create token "$account" \
    --namespace "$HEADLAMP_NAMESPACE" --duration "$TOKEN_DURATION")"

  cat <<MESSAGE
Paste this token at the login prompt (valid ${TOKEN_DURATION}, service account
${HEADLAMP_NAMESPACE}/${account}):

${token}
MESSAGE
}

# Grafana does hold its own accounts, and the chart generates the admin password into a Secret at
# install time. Read it back rather than pinning one in this repository.
grafana_credentials() {
  local secret user password
  secret="$(kubectl get deployment "$GRAFANA_DEPLOYMENT" --namespace "$OBSERVABILITY_NAMESPACE" \
    -o jsonpath='{.spec.template.spec.containers[?(@.name=="grafana")].env[?(@.name=="GF_SECURITY_ADMIN_PASSWORD")].valueFrom.secretKeyRef.name}')"
  [ -n "$secret" ] || fail "could not find Grafana's admin Secret on deployment/$GRAFANA_DEPLOYMENT"

  user="$(kubectl get secret "$secret" --namespace "$OBSERVABILITY_NAMESPACE" \
    -o jsonpath='{.data.admin-user}' | base64 -d)"
  password="$(kubectl get secret "$secret" --namespace "$OBSERVABILITY_NAMESPACE" \
    -o jsonpath='{.data.admin-password}' | base64 -d)"

  printf 'Log in as %s / %s (from secret %s/%s).\n' \
    "$user" "$password" "$OBSERVABILITY_NAMESPACE" "$secret"
}

pid_file() {
  printf '%s/dashboard-%s.pid\n' "$RUN_DIR" "$1"
}

log_file() {
  printf '%s/dashboard-%s.log\n' "$RUN_DIR" "$1"
}

# The Service a console's forward points at. Used to recognize the process again, so it has to be
# the same name start_forward passes to kubectl.
console_service() {
  case "$1" in
    headlamp) printf '%s\n' "$HEADLAMP_SERVICE" ;;
    grafana) printf '%s\n' "$GRAFANA_SERVICE" ;;
    *) fail "unknown console '${1}'" ;;
  esac
}

# Print the pid of a running forward for this console, or nothing. A pid read from a file says only
# that something with that number once existed, so the command line is checked too: pids are reused,
# and killing an unrelated process would be this script's fault, not the operating system's.
forward_pid() {
  local console="$1" file pid args
  file="$(pid_file "$console")"

  [ -f "$file" ] || return 1
  pid="$(cat "$file")"
  [ -n "$pid" ] || return 1

  args="$(ps -o args= -p "$pid" 2>/dev/null || true)"
  case "$args" in
    *kubectl*port-forward*"svc/$(console_service "$console")"*) printf '%s\n' "$pid" ;;
    *)
      rm -f "$file"
      return 1
      ;;
  esac
}

# The local port a running forward is on, read back from its command line rather than remembered in
# a second file: one source of truth for what is actually listening.
forward_port() {
  ps -o args= -p "$1" 2>/dev/null | sed -nE 's/.* ([0-9]+):[0-9]+( .*)?$/\1/p'
}

start_forward() {
  local console="$1" namespace="$2" service="$3" service_port="$4" port="$5"

  local pid
  if pid="$(forward_pid "$console")"; then
    fail "$console is already forwarded on localhost:$(forward_port "$pid") (pid $pid); stop it with: $0 stop $console"
  fi

  mkdir -p "$RUN_DIR"
  local log_path
  log_path="$(log_file "$console")"

  # Bound to loopback on purpose, which is also kubectl's default: the devcontainer runs with
  # --network=host, so 127.0.0.1 here is the loopback the browser on the host opens, and nothing
  # else on the network can reach it.
  log "forwarding localhost:${port} to svc/${service} in ${namespace}"
  kubectl port-forward --namespace "$namespace" \
    --address 127.0.0.1 \
    "svc/${service}" "${port}:${service_port}" \
    >"$log_path" 2>&1 &

  pid=$!
  # Detach from this shell's job table so that the forward survives the terminal that started it;
  # the pidfile, not the parent shell, is what owns it from here.
  disown "$pid" 2>/dev/null || true
  printf '%s\n' "$pid" >"$(pid_file "$console")"

  wait_for_forward "$console" "$pid" "$log_path"
}

# kubectl prints "Forwarding from" once it is listening and exits almost immediately when it cannot
# (port already taken, Service gone), so watching both for a few seconds is the honest readiness
# check. Reporting success on a process that has already died is the failure worth avoiding here.
wait_for_forward() {
  local console="$1" pid="$2" log_path="$3" waited=0
  local deadline=$((FORWARD_READY_TIMEOUT * 2))

  while [ "$waited" -lt "$deadline" ]; do
    if grep -q 'Forwarding from' "$log_path" 2>/dev/null; then
      return 0
    fi
    if ! kill -0 "$pid" 2>/dev/null; then
      rm -f "$(pid_file "$console")"
      cat "$log_path" >&2
      fail "the port-forward for $console exited immediately; see the output above"
    fi
    sleep 0.5
    waited=$((waited + 1))
  done

  fail "the port-forward for $console did not start within ${FORWARD_READY_TIMEOUT}s; see $log_path"
}

stop_forward() {
  local console="$1" pid

  if ! pid="$(forward_pid "$console")"; then
    log "$console is not forwarded"
    return 0
  fi

  log "stopping $console (pid $pid)"
  kill "$pid" 2>/dev/null || true

  # TERM is enough for kubectl, which closes the listener and exits; the wait is so that the port is
  # free by the time this script returns and a restart on the same port cannot race it.
  local waited=0
  while kill -0 "$pid" 2>/dev/null && [ "$waited" -lt 20 ]; do
    sleep 0.5
    waited=$((waited + 1))
  done
  kill -0 "$pid" 2>/dev/null && kill -9 "$pid" 2>/dev/null || true

  rm -f "$(pid_file "$console")"
}

do_stop() {
  case "${1:-all}" in
    all)
      stop_forward headlamp
      stop_forward grafana
      ;;
    headlamp | grafana) stop_forward "$1" ;;
    *) fail "unknown console '${1}'; expected headlamp, grafana or all" ;;
  esac
}

do_status() {
  local console pid
  for console in headlamp grafana; do
    if pid="$(forward_pid "$console")"; then
      printf '%-8s running on http://localhost:%s (pid %s, log %s)\n' \
        "$console" "$(forward_port "$pid")" "$pid" "$(log_file "$console")"
    else
      printf '%-8s not running\n' "$console"
    fi
  done
}

do_headlamp() {
  local port="${1:-$HEADLAMP_PORT}"

  wait_for_deployment "$HEADLAMP_NAMESPACE" "$HEADLAMP_DEPLOYMENT" \
    "run hack/set-up-cluster.sh"

  start_forward headlamp "$HEADLAMP_NAMESPACE" "$HEADLAMP_SERVICE" 80 "$port"

  printf '\nHeadlamp: http://localhost:%s\n\n' "$port"
  headlamp_credentials
  printf '\nStop the console with: %s stop headlamp\n\n' "$0"
}

do_grafana() {
  local port="${1:-$GRAFANA_PORT}"

  wait_for_deployment "$OBSERVABILITY_NAMESPACE" "$GRAFANA_DEPLOYMENT" \
    "the observability stack is optional; install it with WITH_OBSERVABILITY=1 hack/set-up-cluster.sh"

  start_forward grafana "$OBSERVABILITY_NAMESPACE" "$GRAFANA_SERVICE" 80 "$port"

  printf '\nGrafana: http://localhost:%s\n\n' "$port"
  grafana_credentials
  printf '\nStop the console with: %s stop grafana\n\n' "$0"
}

main() {
  # stop and status act on processes on this machine, not on the cluster, so they deliberately run
  # before any provider or context check: a forward left behind by a cluster that has since been
  # deleted is exactly the one you want to be able to stop.
  case "${1:-headlamp}" in
    stop)
      do_stop "${2:-all}"
      return
      ;;
    status)
      do_status
      return
      ;;
  esac

  resolve_cluster_provider
  ensure_cli_tools
  ensure_context

  case "${1:-headlamp}" in
    headlamp) do_headlamp "${2:-}" ;;
    grafana) do_grafana "${2:-}" ;;
    *) fail "unknown console '${1}'; expected headlamp, grafana, stop or status" ;;
  esac
}

main "$@"
