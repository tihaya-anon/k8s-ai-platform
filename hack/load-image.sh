#!/usr/bin/env bash
#
# Copy a locally built Docker image into the cluster's containerd, so a Pod with
# imagePullPolicy IfNotPresent runs it without a registry. This is what
# `kind load docker-image` does, for whichever provider hack/lib/cluster.sh
# selects; the e2e suite calls it after `make docker-build`.
#
# KIND_CLUSTER names the kind cluster, as it does for the Makefile's e2e
# targets; it defaults to the development cluster. On k3s the import goes
# through sudo, because k3s's containerd socket belongs to root.
#
# Usage:
#   hack/load-image.sh <image>

set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER:-k8s-ai-platform}"
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

main() {
  [ "$#" -eq 1 ] || fail "usage: $0 <image>"
  local image="$1"

  resolve_cluster_provider
  command -v docker >/dev/null 2>&1 || fail "docker is not installed"
  docker image inspect "$image" >/dev/null 2>&1 \
    || fail "image $image is not in the local Docker daemon; build it first"
  cluster_exists || fail "$(cluster_describe) does not exist"

  log "loading $image into $(cluster_describe)"
  cluster_load_image "$image"
}

main "$@"
