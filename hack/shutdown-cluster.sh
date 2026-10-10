#!/usr/bin/env bash
#
# Stop, start, or delete the local kind cluster.
#
# Default is `stop`, because `delete` throws away every cert-manager CA, KServe
# webhook certificate, and Kueue quota object in the cluster and the next
# hack/set-up-cluster.sh run has to rebuild all of it. Deleting is the right
# move often enough to be one word away, but not the default.
#
# Usage:
#   hack/shutdown-cluster.sh [stop|start|delete]

set -euo pipefail

readonly CLUSTER_NAME="k8s-ai-platform"

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

ensure_cli_tools() {
  local cmd
  for cmd in docker kind; do
    command -v "$cmd" >/dev/null 2>&1 || fail "$cmd is not installed"
  done
}

cluster_exists() {
  local clusters
  clusters="$(kind get clusters 2>/dev/null || true)"
  printf '%s\n' "$clusters" | grep -Fxq -- "$CLUSTER_NAME"
}

# kind has no stop/start of its own: a cluster is a set of Docker containers, so
# stopping the containers is stopping the cluster. Node state lives in the
# container filesystems and survives.
cluster_nodes() {
  kind get nodes --name "$CLUSTER_NAME" 2>/dev/null || true
}

do_stop() {
  if ! cluster_exists; then
    log "cluster $CLUSTER_NAME does not exist; nothing to stop"
    return 0
  fi

  local nodes
  nodes="$(cluster_nodes)"
  [ -n "$nodes" ] || fail "cluster $CLUSTER_NAME has no nodes; delete it instead"

  # `docker stop` on an already-stopped container is a no-op success, so running
  # this twice is safe.
  log "stopping nodes of cluster $CLUSTER_NAME"
  # shellcheck disable=SC2086
  docker stop $nodes >/dev/null
  log "cluster $CLUSTER_NAME stopped; restart it with: $0 start"
}

do_start() {
  cluster_exists || fail "cluster $CLUSTER_NAME does not exist; create it with hack/set-up-cluster.sh"

  local nodes
  nodes="$(cluster_nodes)"
  [ -n "$nodes" ] || fail "cluster $CLUSTER_NAME has no nodes"

  log "starting nodes of cluster $CLUSTER_NAME"
  # shellcheck disable=SC2086
  docker start $nodes >/dev/null

  # The API server gets a new container IP on restart, so the kubeconfig entry
  # written at create time is stale until kind rewrites it.
  log "refreshing kubeconfig for kind-$CLUSTER_NAME"
  kind export kubeconfig --name "$CLUSTER_NAME"

  log "cluster $CLUSTER_NAME started; controllers need a moment to settle"
}

do_delete() {
  if ! cluster_exists; then
    log "cluster $CLUSTER_NAME does not exist; nothing to delete"
    return 0
  fi

  log "deleting cluster $CLUSTER_NAME"
  kind delete cluster --name "$CLUSTER_NAME"
  log "cluster $CLUSTER_NAME deleted"
}

main() {
  ensure_cli_tools

  case "${1:-stop}" in
    stop) do_stop ;;
    start) do_start ;;
    delete) do_delete ;;
    *) fail "unknown action '${1}'; expected stop, start, or delete" ;;
  esac
}

main "$@"
