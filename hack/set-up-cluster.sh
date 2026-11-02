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
readonly HEADLAMP_VERSION="0.46.0"
# Opt-in, see ensure_observability(). Pinned here anyway so that turning it on does not also mean
# picking a version under time pressure.
readonly KUBE_PROMETHEUS_STACK_VERSION="92.17.0"

# Deployments that must report Available before the next layer is installed.
# Helm's --wait only covers resources in its own chart, so cross-chart ordering
# (KServe's webhooks need cert-manager actually answering) is enforced here.
readonly CERT_MANAGER_DEPLOYMENTS=(cert-manager cert-manager-webhook cert-manager-cainjector)
readonly KSERVE_DEPLOYMENTS=(kserve-controller-manager)
readonly KUEUE_DEPLOYMENTS=(kueue-controller-manager)
readonly HEADLAMP_DEPLOYMENTS=(headlamp)

readonly HEADLAMP_NAMESPACE="headlamp"
readonly OBSERVABILITY_NAMESPACE="observability"

# Charts that are still published to a classic HTTP chart repository rather than as OCI artifacts.
# `helm pull --repo` reads the repository's index.yaml for this one command, so neither of these
# needs `helm repo add` and the host keeps no repo list this script depends on.
readonly HEADLAMP_CHART_REPO="https://kubernetes-sigs.github.io/headlamp/"
readonly PROMETHEUS_COMMUNITY_CHART_REPO="https://prometheus-community.github.io/helm-charts"

readonly WAIT_TIMEOUT="300s"
readonly REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly HELM_CACHE_DIR="${HELM_CACHE_DIR:-$REPO_ROOT/.cache/helm}"

log() {
  printf '==> %s\n' "$*"
}

fail() {
  printf 'error: %s\n' "$*" >&2
  exit 1
}

normalize_proxy_env() {
  # Some Linux desktops export uppercase proxy variables for containers as
  # host.docker.internal while lowercase variables point at the host loopback.
  # Host tools such as helm cannot resolve host.docker.internal, so prefer the
  # lowercase value when the uppercase one has that Docker Desktop hostname.
  if [[ "${HTTP_PROXY:-}" == *host.docker.internal* && -n "${http_proxy:-}" ]]; then
    export HTTP_PROXY="$http_proxy"
  fi
  if [[ "${HTTPS_PROXY:-}" == *host.docker.internal* && -n "${https_proxy:-}" ]]; then
    export HTTPS_PROXY="$https_proxy"
  fi
  if [[ "${ALL_PROXY:-}" == *host.docker.internal* && -n "${all_proxy:-}" ]]; then
    export ALL_PROXY="$all_proxy"
  fi
}

ensure_cli_tools() {
  local cmd
  local missing=0

  for cmd in docker kubectl kind helm curl; do
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

# Download a chart once and reuse the pinned archive on later runs. The cache is deliberately
# outside Git: charts are reproducible inputs, not source files, and can be reacquired from their
# pinned upstream versions when the cache is removed.
# HELM_REPO, when set by the caller, names a classic HTTP chart repository and $chart is then the
# plain chart name inside it. Passed as an environment variable rather than another positional so
# that the OCI callers below -- the majority -- keep reading as they did.
helm_chart() {
  local chart="$1" version="$2"
  local name="${chart##*/}"
  local cached="$HELM_CACHE_DIR/${name}-${version}.tgz"

  if [ -s "$cached" ]; then
    printf '%s\n' "$cached"
    return
  fi

  mkdir -p "$HELM_CACHE_DIR"
  local temporary archive
  temporary="$(mktemp -d "$HELM_CACHE_DIR/.download.XXXXXX")"
  log "caching Helm chart $name $version" >&2
  local -a pull_args=(--version "$version" --destination "$temporary")
  if [ -n "${HELM_REPO:-}" ]; then
    pull_args+=(--repo "$HELM_REPO")
  fi
  helm pull "$chart" "${pull_args[@]}" >/dev/null
  archive="$(find "$temporary" -maxdepth 1 -type f -name '*.tgz' -print -quit)"
  if [ -z "$archive" ]; then
    rm -rf "$temporary"
    fail "helm pull produced no chart archive for $chart@$version"
  fi
  mv "$archive" "$cached"
  rm -rf "$temporary"
  printf '%s\n' "$cached"
}

# Load chart images into the kind node before Kubernetes schedules pods. In this
# development setup the host Docker daemon can reach registries, while the kind
# node may inherit a proxy that points at host.docker.internal, which Linux kind
# nodes cannot resolve by default. Preloading keeps the cluster setup independent
# of in-node registry access.
preload_image() {
  local image="$1"

  log "preloading image $image into kind cluster"
  docker pull "$image" >/dev/null

  if docker save "$image" \
    | docker exec -i "${CLUSTER_NAME}-control-plane" \
        ctr --namespace=k8s.io images import - >/dev/null; then
    return
  fi

  # Docker's containerd image store can export an incomplete archive for some
  # OCI-indexed images. Fall back to flattening the image filesystem while
  # preserving the runtime fields Kubernetes needs to start the container.
  log "docker save could not be imported for $image; flattening local image"
  local entrypoint cmd user workdir container
  local -a changes envs
  changes=()
  entrypoint="$(docker image inspect "$image" --format '{{json .Config.Entrypoint}}')"
  cmd="$(docker image inspect "$image" --format '{{json .Config.Cmd}}')"
  user="$(docker image inspect "$image" --format '{{.Config.User}}')"
  workdir="$(docker image inspect "$image" --format '{{.Config.WorkingDir}}')"
  mapfile -t envs < <(docker image inspect "$image" --format '{{range .Config.Env}}{{println .}}{{end}}')

  if [ "$entrypoint" != "null" ]; then
    changes+=(--change "ENTRYPOINT $entrypoint")
  fi
  if [ "$cmd" != "null" ]; then
    changes+=(--change "CMD $cmd")
  fi
  if [ -n "$user" ]; then
    changes+=(--change "USER $user")
  fi
  if [ -n "$workdir" ]; then
    changes+=(--change "WORKDIR $workdir")
  fi
  local env
  for env in "${envs[@]}"; do
    if [ -n "$env" ]; then
      changes+=(--change "ENV $env")
    fi
  done

  container="$(docker create "$image")"
  docker export "$container" | docker import "${changes[@]}" - "$image" >/dev/null
  docker rm "$container" >/dev/null
  docker save "$image" \
    | docker exec -i "${CLUSTER_NAME}-control-plane" \
        ctr --namespace=k8s.io images import - >/dev/null
}

preload_helm_images() {
  local release="$1" chart="$2" namespace="$3"
  shift 3

  local images image
  mapfile -t images < <(
    helm template "$release" "$chart" --namespace "$namespace" "$@" \
      | sed -n -E \
        -e 's/^[[:space:]]*image:[[:space:]]*"?([^" #]+)"?.*/\1/p' \
        -e 's/^[[:space:]]*-[[:space:]]*--[^=]*image=([^" ]+).*/\1/p' \
      | sort -u
  )

  if [ "${#images[@]}" -eq 0 ]; then
    return
  fi

  for image in "${images[@]}"; do
    if docker exec "${CLUSTER_NAME}-control-plane" crictl inspecti "$image" >/dev/null 2>&1; then
      continue
    fi
    preload_image "$image"
  done
}

# Upsert a Helm release. `helm upgrade --install` installs when the release is
# absent and converges it when it is present, so a changed version or value here
# is applied on the next run instead of being silently skipped.
helm_release() {
  local release="$1" chart="$2" namespace="$3" version="$4"
  shift 4
  chart="$(helm_chart "$chart" "$version")"
  preload_helm_images "$release" "$chart" "$namespace" "$@"

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
  # manifests, not a chart; cache it for repeatable local setup.
  local manifest="$HELM_CACHE_DIR/gateway-api-${GATEWAY_API_VERSION}.yaml"
  mkdir -p "$HELM_CACHE_DIR"
  if [ ! -s "$manifest" ]; then
    local temporary="${manifest}.tmp.$$"
    log "caching Gateway API CRDs $GATEWAY_API_VERSION"
    curl --fail --location --silent --show-error \
      "https://github.com/kubernetes-sigs/gateway-api/releases/download/${GATEWAY_API_VERSION}/standard-install.yaml" \
      -o "$temporary"
    mv "$temporary" "$manifest"
  fi
  log "applying Gateway API CRDs $GATEWAY_API_VERSION"
  kubectl apply -f "$manifest"
}

ensure_cert_manager() {
  # KServe generates its webhook serving certificates through cert-manager, so
  # cert-manager must be answering before KServe's webhooks are registered.
  helm_release cert-manager oci://quay.io/jetstack/charts/cert-manager cert-manager "$CERT_MANAGER_VERSION" \
    --set crds.enabled=true
  wait_for_deployments cert-manager "${CERT_MANAGER_DEPLOYMENTS[@]}"
}

ensure_kserve() {
  # CRDs first, as their own release: Helm never upgrades CRDs that live in a
  # chart's crds/ directory, which is exactly why KServe publishes them as a
  # separate chart whose CRDs are ordinary templates.
  helm_release kserve-crd oci://ghcr.io/kserve/charts/kserve-crd kserve "$KSERVE_VERSION"

  helm_release kserve oci://ghcr.io/kserve/charts/kserve-resources kserve "$KSERVE_VERSION" \
    --set kserve.controller.deploymentMode=Standard \
    --set kserve.controller.imagePullPolicy=IfNotPresent \
    --set kserve.controller.gateway.ingressGateway.enableGatewayApi=true \
    --set kserve.controller.gateway.ingressGateway.kserveGateway=kserve/kserve-ingress-gateway

  wait_for_deployments kserve "${KSERVE_DEPLOYMENTS[@]}"
}

ensure_kueue() {
  helm_release kueue oci://registry.k8s.io/kueue/charts/kueue kueue-system "$KUEUE_VERSION"
  wait_for_deployments kueue-system "${KUEUE_DEPLOYMENTS[@]}"
}

# Headlamp is a read-mostly web console for the objects in this cluster: Deployments, Pods, Events,
# and -- the reason it is here rather than the Kubernetes Dashboard -- the custom resources this
# repository defines, which it renders from their CRD schemas without a plugin.
#
# It runs in the cluster because that is what makes its service account the thing the API server
# authenticates; the browser reaches it through hack/dashboard.sh, which port-forwards rather than
# exposing a NodePort. A kind node's ports are only reachable at all if they were mapped at create
# time, and a console bound to a host port is a console anything on the host can talk to.
#
# The chart's default clusterRoleBinding gives its service account cluster-admin. That is accepted
# here and nowhere else: this is a local single-node training cluster, and the alternative --
# curating a read-only ClusterRole -- would hide exactly the RBAC errors a platform under
# development should surface.
ensure_headlamp() {
  HELM_REPO="$HEADLAMP_CHART_REPO" \
    helm_release headlamp headlamp "$HEADLAMP_NAMESPACE" "$HEADLAMP_VERSION"
  wait_for_deployments "$HEADLAMP_NAMESPACE" "${HEADLAMP_DEPLOYMENTS[@]}"
}

# Prometheus, Grafana, node-exporter and kube-state-metrics, off by default.
#
# Off because it is the only layer here that is not needed to run the platform: it answers "how is
# the reconciler behaving over time", which is a question worth asking once there is a reconciler
# worth watching. It also costs roughly a gigabyte of memory on a single kind node, which is a poor
# trade on a laptop that is mostly running envtest.
#
# Turn it on with WITH_OBSERVABILITY=1 hack/set-up-cluster.sh. Turning it off again is
# `helm uninstall kube-prometheus-stack -n observability`: this script converges what is asked for
# and does not remove what a previous run installed.
ensure_observability() {
  if [ "${WITH_OBSERVABILITY:-0}" != "1" ]; then
    log "skipping the observability stack; enable it with WITH_OBSERVABILITY=1"
    return
  fi

  # The chart ships its CRDs in crds/, which Helm installs but never upgrades. A version bump that
  # changes the Prometheus CRDs therefore needs `kubectl apply` of the new ones, or a reinstall --
  # the same trap KServe sidesteps by publishing kserve-crd as its own chart.
  HELM_REPO="$PROMETHEUS_COMMUNITY_CHART_REPO" \
    helm_release kube-prometheus-stack kube-prometheus-stack \
      "$OBSERVABILITY_NAMESPACE" "$KUBE_PROMETHEUS_STACK_VERSION" \
      --set grafana.defaultDashboardsTimezone=browser \
      --set prometheus.prometheusSpec.retention=24h
}

main() {
  normalize_proxy_env
  ensure_cli_tools
  ensure_cluster
  ensure_gateway_api
  ensure_cert_manager
  ensure_kserve
  ensure_kueue
  ensure_headlamp
  ensure_observability
  log "cluster $CLUSTER_NAME is ready"
  log "open the web console with: hack/dashboard.sh"
}

main "$@"
