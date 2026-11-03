#!/usr/bin/env bash
#
# Stop, start, or delete the local cluster: a kind cluster, or the host's k3s.
# hack/lib/cluster.sh owns the difference; see it for how the provider is chosen.
#
# Default is `stop`, because `delete` throws away every cert-manager CA, KServe
# webhook certificate, and Kueue quota object in the cluster and the next
# hack/set-up-cluster.sh run has to rebuild all of it. Deleting is the right
# move often enough to be one word away, but not the default.
#
# On k3s, stop and start go through sudo, and delete refuses: k3s belongs to the
# host, and removing it is uninstalling it.
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

# shellcheck source=lib/cluster.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/cluster.sh"

ensure_cli_tools() {
  local cmd
  while IFS= read -r cmd; do
    command -v "$cmd" >/dev/null 2>&1 || fail "$cmd is not installed"
  done < <(cluster_cli_tools)
}

do_stop() {
  if ! cluster_exists; then
    log "$(cluster_describe) does not exist; nothing to stop"
    return 0
  fi

  log "stopping $(cluster_describe)"
  cluster_stop
  log "$(cluster_describe) stopped; restart it with: $0 start"
}

do_start() {
  cluster_exists || fail "$(cluster_describe) does not exist; create it with hack/set-up-cluster.sh"

  log "starting $(cluster_describe)"
  cluster_start
  log "$(cluster_describe) started; controllers need a moment to settle"
}

do_delete() {
  if ! cluster_exists; then
    log "$(cluster_describe) does not exist; nothing to delete"
    return 0
  fi

  cluster_delete
  log "$(cluster_describe) deleted"
}

main() {
  resolve_cluster_provider
  ensure_cli_tools

  case "${1:-stop}" in
    stop) do_stop ;;
    start) do_start ;;
    delete) do_delete ;;
    *) fail "unknown action '${1}'; expected stop, start, or delete" ;;
  esac
}

main "$@"
