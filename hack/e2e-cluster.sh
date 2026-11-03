#!/usr/bin/env bash
#
# Bring the e2e cluster up or down for `make test-e2e`, on whichever provider
# hack/lib/cluster.sh selects.
#
# On kind, `up` creates the throwaway cluster named by KIND_CLUSTER and `down`
# deletes it, which is what the Kubebuilder scaffold did inline in the Makefile.
# On k3s there is one cluster and it belongs to the host, so `up` only checks it
# is there and `down` leaves it running: the suite's AfterAll already undeploys
# what it deployed.
#
# Either way `up` switches kubectl to the e2e cluster's context, because the
# suite talks to whatever the current context is.
#
# Usage:
#   hack/e2e-cluster.sh up|down

set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER:-k8s-ai-platform-test-e2e}"
readonly CLUSTER_NAME

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

# shellcheck source=lib/cluster.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/cluster.sh"

do_up() {
  if cluster_exists; then
    log "$(cluster_describe) already exists"
  elif cluster_is_disposable; then
    # No version: the e2e cluster takes the node image kind itself defaults to.
    cluster_create ""
  else
    fail "$(cluster_describe) does not exist and is not created by this repository"
  fi

  local context
  context="$(cluster_context)"
  kubectl config use-context "$context" >/dev/null \
    || fail "no kubeconfig context $context for the e2e cluster"
}

do_down() {
  if ! cluster_is_disposable; then
    log "leaving $(cluster_describe) running; it is not a throwaway e2e cluster"
    return 0
  fi
  if cluster_exists; then
    cluster_delete
  fi
}

main() {
  resolve_cluster_provider

  local cmd
  while IFS= read -r cmd; do
    command -v "$cmd" >/dev/null 2>&1 || fail "$cmd is not installed"
  done < <(cluster_cli_tools)

  case "${1:-}" in
    up) do_up ;;
    down) do_down ;;
    *) fail "usage: $0 up|down" ;;
  esac
}

main "$@"
