#!/usr/bin/env bash
# Stop or remove the local kind cluster used by this repository.
#
# Modes:
#   stop   keep all cluster state, but stop the Docker node container
#   delete delete the kind cluster; keep downloaded dependencies and images
#   clean  delete the kind cluster and remove rebuildable local dependencies

set -o errexit
set -o nounset
set -o pipefail

readonly CLUSTER_NAME="k8s-ai-platform"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
readonly REPO_ROOT
readonly DEPS_DIR="${REPO_ROOT}/deps"

usage() {
  cat <<EOF
Usage: $0 {stop|delete|clean}

Modes:
  stop    Stop the kind node container. Restart by running hack/set-up-cluster.sh again.
  delete  Delete the kind cluster, but keep deps/ and Docker images.
  clean   Delete the kind cluster and remove rebuildable deps/ checkouts/tools.
EOF
}

cluster_exists() {
  kind get clusters 2>/dev/null | grep -qx "${CLUSTER_NAME}"
}

stop_cluster() {
  if ! cluster_exists; then
    echo "cluster ${CLUSTER_NAME} does not exist"
    return 0
  fi

  docker stop "${CLUSTER_NAME}-control-plane"
  echo "cluster ${CLUSTER_NAME} stopped"
}

delete_cluster() {
  if cluster_exists; then
    kind delete cluster --name "${CLUSTER_NAME}"
  else
    echo "cluster ${CLUSTER_NAME} does not exist"
  fi
}

clean_rebuildable_content() {
  delete_cluster
  rm -rf "${DEPS_DIR}"
  echo "removed ${DEPS_DIR}"
}

if [ "$#" -ne 1 ]; then
  usage >&2
  exit 2
fi

case "$1" in
  stop)
    stop_cluster
    ;;
  delete)
    delete_cluster
    ;;
  clean)
    clean_rebuildable_content
    ;;
  -h | --help | help)
    usage
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
