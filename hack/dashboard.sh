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
# the cluster runs; a port-forward lives and dies with this process.
#
# Usage:
#   hack/dashboard.sh [headlamp|grafana] [local-port]

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

forward() {
  local namespace="$1" service="$2" service_port="$3" port="$4"

  # Bound to loopback on purpose, which is also kubectl's default: the devcontainer runs with
  # --network=host, so 127.0.0.1 here is the loopback the browser on the host opens, and nothing
  # else on the network can reach it.
  log "forwarding localhost:${port} to svc/${service} in ${namespace}"
  exec kubectl port-forward --namespace "$namespace" \
    --address 127.0.0.1 \
    "svc/${service}" "${port}:${service_port}"
}

do_headlamp() {
  local port="${1:-$HEADLAMP_PORT}"

  wait_for_deployment "$HEADLAMP_NAMESPACE" "$HEADLAMP_DEPLOYMENT" \
    "run hack/set-up-cluster.sh"

  printf '\nHeadlamp: http://localhost:%s\n\n' "$port"
  headlamp_credentials
  printf '\nStop the console with Ctrl-C.\n\n'

  forward "$HEADLAMP_NAMESPACE" "$HEADLAMP_SERVICE" 80 "$port"
}

do_grafana() {
  local port="${1:-$GRAFANA_PORT}"

  wait_for_deployment "$OBSERVABILITY_NAMESPACE" "$GRAFANA_DEPLOYMENT" \
    "the observability stack is optional; install it with WITH_OBSERVABILITY=1 hack/set-up-cluster.sh"

  printf '\nGrafana: http://localhost:%s\n\n' "$port"
  grafana_credentials
  printf '\nStop the console with Ctrl-C.\n\n'

  forward "$OBSERVABILITY_NAMESPACE" "$GRAFANA_SERVICE" 80 "$port"
}

main() {
  resolve_cluster_provider
  ensure_cli_tools
  ensure_context

  case "${1:-headlamp}" in
    headlamp) do_headlamp "${2:-}" ;;
    grafana) do_grafana "${2:-}" ;;
    *) fail "unknown console '${1}'; expected headlamp or grafana" ;;
  esac
}

main "$@"
