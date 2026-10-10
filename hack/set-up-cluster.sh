#!/usr/bin/env bash
#
# Create or converge the local kind cluster used for platform development.
#
# Re-running this script is safe and is the normal way to apply a version bump:
# every step declares desired state rather than branching on "did I already do
# this", so the script stays the source of truth for the cluster.

set -euo pipefail

# Pinned. "Latest" is not a version: a cluster you cannot rebuild identically
# tomorrow cannot reproduce a bug you hit today.
readonly CLUSTER_NAME="k8s-ai-platform"
readonly KUBERNETES_VERSION="v1.36.1"
readonly CERT_MANAGER_VERSION="v1.17.0"
readonly KSERVE_VERSION="v0.20.0"
readonly KUEUE_VERSION="0.19.5"
readonly GATEWAY_API_VERSION="v1.3.0"

# Deployments that must report Available before the next layer is installed.
# Helm's --wait only covers resources in its own chart, so cross-chart ordering
# (KServe's webhooks need cert-manager actually answering) is enforced here.
readonly CERT_MANAGER_DEPLOYMENTS=(cert-manager cert-manager-webhook cert-manager-cainjector)
readonly KSERVE_DEPLOYMENTS=(kserve-controller-manager)
readonly KUEUE_DEPLOYMENTS=(kueue-controller-manager)

readonly WAIT_TIMEOUT="300s"

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

ensure_cli_tools() {
  local cmd
  local missing=0

  for cmd in docker kubectl kind helm; do
    if command -v "$cmd" >/dev/null 2>&1; then
      printf '  %-8s ok\n' "$cmd"
    else
      printf '  %-8s MISSING\n' "$cmd" >&2
      missing=1
    fi
  done

  if [ "$missing" -ne 0 ]; then
    fail "install the missing tools above before running this script"
  fi
}

ensure_cluster() {
  # Capture first, match second: `kind get clusters | grep -q` lets grep close
  # the pipe on its first match and SIGPIPE kind.
  local clusters
  clusters="$(kind get clusters 2>/dev/null || true)"

  if printf '%s\n' "$clusters" | grep -Fxq -- "$CLUSTER_NAME"; then
    log "kind cluster $CLUSTER_NAME already exists"
  else
    log "creating kind cluster $CLUSTER_NAME on Kubernetes $KUBERNETES_VERSION"
    kind create cluster \
      --name "$CLUSTER_NAME" \
      --image "kindest/node:${KUBERNETES_VERSION}"
  fi

  # Point kubectl at this cluster explicitly. Relying on "whatever the current
  # context happens to be" is how you install KServe into a production cluster.
  kubectl config use-context "kind-${CLUSTER_NAME}" >/dev/null

  local actual
  actual="$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.kubeletVersion}')"
  if [ "$actual" != "$KUBERNETES_VERSION" ]; then
    fail "cluster $CLUSTER_NAME runs $actual, expected $KUBERNETES_VERSION; delete it with hack/shutdown-cluster.sh and re-run"
  fi
}

# Upsert a Helm release. `helm upgrade --install` installs when the release is
# absent and converges it when it is present, so a changed version or value here
# is applied on the next run instead of being silently skipped.
helm_release() {
  local release="$1" chart="$2" namespace="$3"
  shift 3

  # `helm status` exits 0 even for a half-installed release. An interrupted
  # install leaves `pending-install`, which `helm upgrade` refuses to touch, so
  # clear it rather than failing the whole script on every subsequent run.
  # `|| true`: helm exits non-zero when the release is absent, which is the
  # normal first-run case, and errexit would otherwise kill the script here.
  # No `exit` in the awk program either: quitting early closes the pipe and
  # kills helm with SIGPIPE, which pipefail then reports as a failed pipeline.
  local status
  status="$(helm status "$release" --namespace "$namespace" 2>/dev/null \
    | awk '$1 == "STATUS:" { print $2 }' || true)"

  case "$status" in
    pending-install | pending-upgrade | pending-rollback | failed)
      log "release $release is in state '$status'; uninstalling before reinstall"
      helm uninstall "$release" --namespace "$namespace" --wait || true
      ;;
  esac

  log "applying Helm release $release ($chart) in $namespace"
  helm upgrade --install "$release" "$chart" \
    --namespace "$namespace" \
    --create-namespace \
    --wait --timeout "$WAIT_TIMEOUT" \
    "$@"
}

wait_for_deployments() {
  local namespace="$1"
  shift

  local deployment
  for deployment in "$@"; do
    log "waiting for deployment/$deployment in $namespace"
    # Two waits on purpose: `--for=create` so a slow chart does not race us into
    # "NotFound", then the real readiness gate.
    kubectl wait --namespace "$namespace" \
      --for=create "deployment/$deployment" \
      --timeout="$WAIT_TIMEOUT"
    kubectl wait --namespace "$namespace" \
      --for=condition=Available "deployment/$deployment" \
      --timeout="$WAIT_TIMEOUT"
  done
}

ensure_gateway_api() {
  # KServe's Standard deployment mode programs Gateway API objects, and the API
  # server rejects those if the CRDs are absent. Gateway API ships as plain
  # manifests, not a chart; `kubectl apply` is already idempotent.
  log "applying Gateway API CRDs $GATEWAY_API_VERSION"
  kubectl apply -f \
    "https://github.com/kubernetes-sigs/gateway-api/releases/download/${GATEWAY_API_VERSION}/standard-install.yaml"
}

ensure_cert_manager() {
  # KServe generates its webhook serving certificates through cert-manager, so
  # cert-manager must be answering before KServe's webhooks are registered.
  helm_release cert-manager oci://quay.io/jetstack/charts/cert-manager cert-manager \
    --version "$CERT_MANAGER_VERSION" \
    --set crds.enabled=true
  wait_for_deployments cert-manager "${CERT_MANAGER_DEPLOYMENTS[@]}"
}

ensure_kserve() {
  # CRDs first, as their own release: Helm never upgrades CRDs that live in a
  # chart's crds/ directory, which is exactly why KServe publishes them as a
  # separate chart whose CRDs are ordinary templates.
  helm_release kserve-crd oci://ghcr.io/kserve/charts/kserve-crd kserve \
    --version "$KSERVE_VERSION"

  helm_release kserve oci://ghcr.io/kserve/charts/kserve-resources kserve \
    --version "$KSERVE_VERSION" \
    --set kserve.controller.deploymentMode=Standard \
    --set kserve.controller.gateway.ingressGateway.enableGatewayApi=true \
    --set kserve.controller.gateway.ingressGateway.kserveGateway=kserve/kserve-ingress-gateway

  wait_for_deployments kserve "${KSERVE_DEPLOYMENTS[@]}"
}

ensure_kueue() {
  helm_release kueue oci://registry.k8s.io/kueue/charts/kueue kueue-system \
    --version "$KUEUE_VERSION"
  wait_for_deployments kueue-system "${KUEUE_DEPLOYMENTS[@]}"
}

main() {
  ensure_cli_tools
  ensure_cluster
  ensure_gateway_api
  ensure_cert_manager
  ensure_kserve
  ensure_kueue
  log "cluster $CLUSTER_NAME is ready"
}

main "$@"
