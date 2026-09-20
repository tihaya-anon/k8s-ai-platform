#!/usr/bin/env bash
# Create the kind cluster and install the two adopted operators, repeatably.
# Safe to re-run: every step checks for its own result first.

set -o errexit  # stop at the first failing command
set -o nounset  # an unset variable is a bug, not an empty string
set -o pipefail # a pipeline fails if any stage fails, not just the last

# Pinned. "Latest" is not a version: a cluster you cannot rebuild identically
# is not a cluster you can hand to anyone else.
readonly CLUSTER_NAME="k8s-ai-platform"
readonly KSERVE_VERSION="v0.20.0"
readonly KUEUE_VERSION="v0.19.5"

# $0 is the path the script was invoked with, so dirname alone is relative.
# cd + pwd resolves it to an absolute path that survives the cd's below.
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
readonly REPO_ROOT
readonly DEPS_DIR="${REPO_ROOT}/deps"
readonly KSERVE_DIR="${DEPS_DIR}/kserve"

# ensure env
kubectl version --client
docker version --format '{{.Server.Version}}'
kind version

# create cluster
if kind get clusters | grep -qx "${CLUSTER_NAME}"; then
  echo "cluster ${CLUSTER_NAME} exists"
else
  kind create cluster --name "${CLUSTER_NAME}"
fi
kubectl config use-context "kind-${CLUSTER_NAME}"

# fetch kserve
# -p: no error when deps/ is already there, and it creates parents as needed.
mkdir -p "${DEPS_DIR}"
if [ -d "${KSERVE_DIR}/.git" ]; then
  echo "exist"
else
  # A tag, not a branch: master pins KSERVE_VERSION to the *next* release, so
  # its installer asks the chart registry for a version nobody has published.
  git clone --depth 1 --branch "${KSERVE_VERSION}" \
    https://github.com/kserve/kserve.git "${KSERVE_DIR}"
fi

# Re-pin even on an existing checkout, so a stale clone cannot change the install.
git -C "${KSERVE_DIR}" fetch --depth 1 origin "refs/tags/${KSERVE_VERSION}:refs/tags/${KSERVE_VERSION}"
git -C "${KSERVE_DIR}" checkout --quiet "tags/${KSERVE_VERSION}"

# install cert-manager and kserve (the script does both, and skips what is present)
"${KSERVE_DIR}/hack/setup/quick-install/kserve-standard-mode-full-install-helm.sh"

# install kueue
kubectl apply --server-side -f "https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml"
kubectl wait deploy/kueue-controller-manager -n kueue-system --for=condition=available --timeout=5m
