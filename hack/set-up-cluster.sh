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
readonly KIND_NODE_IMAGE="kindest/node:v1.36.1"
readonly KUBERNETES_VERSION="v1.36.1"
readonly KUEUE_IMAGE="registry.k8s.io/kueue/kueue:${KUEUE_VERSION}"
readonly CERT_MANAGER_VERSION="v1.17.0"

# $0 is the path the script was invoked with, so dirname alone is relative.
# cd + pwd resolves it to an absolute path that survives the cd's below.
REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
readonly REPO_ROOT
readonly DEPS_DIR="${REPO_ROOT}/deps"
readonly KSERVE_DIR="${DEPS_DIR}/kserve"

show_namespace_diagnostics() {
  namespace="$1"
  echo "${namespace} did not become ready. Current state:" >&2
  kubectl get deploy,pods -n "${namespace}" -o wide >&2 || true

  kubectl get deploy -n "${namespace}" -o name | while read -r deploy; do
    echo "--- describe ${deploy} ---" >&2
    kubectl describe -n "${namespace}" "${deploy}" >&2 || true
  done

  kubectl get pods -n "${namespace}" -o name | while read -r pod; do
    echo "--- describe ${pod} ---" >&2
    kubectl describe -n "${namespace}" "${pod}" >&2 || true
    echo "--- logs ${pod} ---" >&2
    kubectl logs -n "${namespace}" "${pod}" --all-containers=true --tail=100 >&2 || true
  done
}

show_kueue_diagnostics() {
  show_namespace_diagnostics kueue-system
}

show_cert_manager_diagnostics() {
  show_namespace_diagnostics cert-manager
}

kind_node_platform() {
  node_arch="$(docker exec "${CLUSTER_NAME}-control-plane" uname -m)"
  case "${node_arch}" in
    x86_64) echo "linux/amd64" ;;
    aarch64 | arm64) echo "linux/arm64" ;;
    *)
      echo "unsupported kind node architecture: ${node_arch}" >&2
      exit 1
      ;;
  esac
}

ensure_kind_cluster_running() {
  node_container="${CLUSTER_NAME}-control-plane"

  if docker inspect "${node_container}" >/dev/null 2>&1; then
    running="$(docker inspect -f '{{.State.Running}}' "${node_container}")"
    if [ "${running}" != "true" ]; then
      echo "starting stopped kind node ${node_container}"
      docker start "${node_container}" >/dev/null
    fi
  fi
}

wait_for_api_server() {
  attempts=60
  while [ "${attempts}" -gt 0 ]; do
    if kubectl get nodes >/dev/null 2>&1; then
      return 0
    fi
    attempts=$((attempts - 1))
    sleep 1
  done

  echo "cluster ${CLUSTER_NAME} exists, but its API server did not become reachable" >&2
  echo "try: ./hack/shutdown-cluster.sh delete && ./hack/set-up-cluster.sh" >&2
  exit 1
}

load_standard_image_into_kind() {
  image="$1"
  platform="$(kind_node_platform)"

  if docker exec "${CLUSTER_NAME}-control-plane" \
    ctr --namespace=k8s.io images ls -q | grep -Fx "${image}" >/dev/null; then
    echo "${image} is already loaded in kind"
    return 0
  fi

  local_platform="$(docker image inspect "${image}" \
    --format '{{.Os}}/{{.Architecture}}' 2>/dev/null || true)"
  if [ "${local_platform}" != "${platform}" ]; then
    docker pull --platform "${platform}" "${image}"
  else
    echo "${image} is already present locally for ${platform}"
  fi

  image_archive="$(mktemp)"
  trap 'rm -f "${image_archive}"' RETURN
  docker save "${image}" -o "${image_archive}"
  docker exec --privileged -i "${CLUSTER_NAME}-control-plane" \
    ctr --namespace=k8s.io images import - <"${image_archive}"
  docker exec "${CLUSTER_NAME}-control-plane" \
    ctr --namespace=k8s.io images ls -q | grep -Fx "${image}" >/dev/null
  rm -f "${image_archive}"
  trap - RETURN
}

load_cert_manager_images() {
  load_standard_image_into_kind "quay.io/jetstack/cert-manager-controller:${CERT_MANAGER_VERSION}"
  load_standard_image_into_kind "quay.io/jetstack/cert-manager-cainjector:${CERT_MANAGER_VERSION}"
  load_standard_image_into_kind "quay.io/jetstack/cert-manager-webhook:${CERT_MANAGER_VERSION}"
  load_standard_image_into_kind "quay.io/jetstack/cert-manager-startupapicheck:${CERT_MANAGER_VERSION}"
}

wait_for_cert_manager() {
  if ! kubectl get namespace cert-manager >/dev/null 2>&1; then
    return 0
  fi

  load_cert_manager_images

  if ! kubectl wait deploy/cert-manager -n cert-manager --for=condition=available --timeout=1s >/dev/null 2>&1 ||
    ! kubectl wait deploy/cert-manager-cainjector -n cert-manager --for=condition=available --timeout=1s >/dev/null 2>&1 ||
    ! kubectl wait deploy/cert-manager-webhook -n cert-manager --for=condition=available --timeout=1s >/dev/null 2>&1; then
    kubectl delete pod -n cert-manager --all --ignore-not-found
  fi

  echo "waiting for cert-manager to be ready"
  if ! kubectl wait deploy/cert-manager -n cert-manager --for=condition=available --timeout=2m ||
    ! kubectl wait deploy/cert-manager-cainjector -n cert-manager --for=condition=available --timeout=2m ||
    ! kubectl wait deploy/cert-manager-webhook -n cert-manager --for=condition=available --timeout=2m ||
    ! kubectl wait --for=jsonpath='{.subsets[0].addresses}' \
      endpoints/cert-manager-webhook -n cert-manager --timeout=60s; then
    show_cert_manager_diagnostics
    exit 1
  fi
}

install_kserve() {
  load_cert_manager_images
  wait_for_cert_manager

  if "${KSERVE_DIR}/hack/setup/quick-install/kserve-standard-mode-full-install-helm.sh"; then
    return 0
  fi

  echo "KServe installer failed. cert-manager may have been installed but not ready yet; retrying once." >&2
  wait_for_cert_manager
  "${KSERVE_DIR}/hack/setup/quick-install/kserve-standard-mode-full-install-helm.sh"
}

load_image_into_kind() {
  image="$1"
  platform="$(kind_node_platform)"

  if docker exec "${CLUSTER_NAME}-control-plane" \
    ctr --namespace=k8s.io images ls -q | grep -Fx "${image}" >/dev/null; then
    echo "${image} is already loaded in kind"
    return 0
  fi

  local_platform="$(docker image inspect "${image}" \
    --format '{{.Os}}/{{.Architecture}}' 2>/dev/null || true)"
  if [ "${local_platform}" != "${platform}" ]; then
    # A previous failed run may have cached only the multi-platform index. Docker
    # then says the image is "up to date" but cannot export the node platform.
    # Only remove/pull when the local runnable image is missing or wrong.
    docker image rm --force "${image}" >/dev/null 2>&1 || true
    docker pull --platform "${platform}" "${image}"
  else
    echo "${image} is already present locally for ${platform}"
  fi

  # Normalize the image before importing it. The upstream tag can leave Docker
  # holding a manifest/index shape that this kind node's containerd cannot
  # import, even though Docker can run it. Kueue's Deployment explicitly runs
  # /manager, so rebuild a tiny single-platform image from that binary and tag
  # it with the same name the manifest uses.
  image_build_dir="$(mktemp -d)"
  container_id="$(docker create "${image}" /manager)"
  trap 'docker rm "${container_id}" >/dev/null 2>&1 || true; rm -rf "${image_build_dir}"' RETURN
  docker cp "${container_id}:/manager" "${image_build_dir}/manager"
  docker rm "${container_id}" >/dev/null
  cat >"${image_build_dir}/Dockerfile" <<'EOF'
FROM scratch
COPY manager /manager
USER 65532:65532
EOF
  docker build --platform "${platform}" -t "${image}" "${image_build_dir}"

  image_archive="$(mktemp)"
  trap 'rm -f "${image_archive}"; rm -rf "${image_build_dir}"' RETURN
  docker save "${image}" -o "${image_archive}"
  docker exec --privileged -i "${CLUSTER_NAME}-control-plane" \
    ctr --namespace=k8s.io images import - <"${image_archive}"
  docker exec "${CLUSTER_NAME}-control-plane" \
    ctr --namespace=k8s.io images ls -q | grep -Fx "${image}" >/dev/null
  rm -f "${image_archive}"
  rm -rf "${image_build_dir}"
  trap - RETURN
}

# ensure env
kubectl version --client
docker version --format '{{.Server.Version}}'
kind version

# create cluster
if kind get clusters | grep -qx "${CLUSTER_NAME}"; then
  echo "cluster ${CLUSTER_NAME} exists"
  ensure_kind_cluster_running
else
  kind create cluster --name "${CLUSTER_NAME}" --image "${KIND_NODE_IMAGE}"
fi
kubectl config use-context "kind-${CLUSTER_NAME}"
wait_for_api_server

actual_kubernetes_version="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.kubeletVersion}')"
if [ "${actual_kubernetes_version}" != "${KUBERNETES_VERSION}" ]; then
  echo "cluster ${CLUSTER_NAME} is ${actual_kubernetes_version}, expected ${KUBERNETES_VERSION}" >&2
  echo "delete it with: kind delete cluster --name ${CLUSTER_NAME}" >&2
  exit 1
fi

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
install_kserve

# install kueue
# Preload the controller image into kind. Otherwise the kubelet has to pull it
# from inside the node container, which can fail even when the host can reach
# GitHub and the Kubernetes registry.
load_image_into_kind "${KUEUE_IMAGE}"
kubectl apply --server-side --force-conflicts \
  -f "https://github.com/kubernetes-sigs/kueue/releases/download/${KUEUE_VERSION}/manifests.yaml"

# This local kind setup preloads the image because the node container may not be
# able to reach registry.k8s.io. Make that contract explicit: do not let kubelet
# spend minutes retrying a registry pull that we already know can fail here.
kubectl patch deploy/kueue-controller-manager -n kueue-system --type=strategic \
  -p '{"spec":{"template":{"spec":{"containers":[{"name":"manager","imagePullPolicy":"Never"}]}}}}'
kubectl delete pod -n kueue-system -l control-plane=controller-manager --ignore-not-found

if ! kubectl wait deploy/kueue-controller-manager -n kueue-system \
  --for=condition=available --timeout=2m; then
  show_kueue_diagnostics
  exit 1
fi
