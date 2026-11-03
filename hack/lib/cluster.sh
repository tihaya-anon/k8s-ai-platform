# Cluster-provider plumbing for hack/set-up-cluster.sh, hack/shutdown-cluster.sh, hack/dashboard.sh
# and hack/load-image.sh.
#
# Two providers are supported, and they differ in exactly the places this file covers:
#
#   kind  -- the cluster is a set of Docker containers this repository creates, stops and deletes.
#            Its node cannot be trusted to reach registries through a host proxy, so images are
#            pulled by the host Docker daemon and imported into the node.
#   k3s   -- the cluster is the host itself: a systemd service installed system-wide by the k3s
#            installer, with its containerd configured to pull through the host proxy. This
#            repository uses it but does not own it, so it never creates or deletes it.
#
# Everything above the cluster -- Helm releases, readiness gates, the platform's own manifests --
# is identical on both, which is the point: the scripts call these functions and never test the
# provider themselves. The same rule as Go code ("no `if kind`"), applied to the shell.
#
# Sourced, never executed. The caller must set, before calling anything below:
#
#   CLUSTER_NAME   the kind cluster name; unused on k3s, which has exactly one cluster
#
# and must define:
#
#   log            print a progress line
#   fail           print an error and exit non-zero
#
# The provider comes from CLUSTER_PROVIDER (kind or k3s). When it is unset, a running k3s service
# selects k3s and anything else selects kind, so an existing kind setup keeps working unchanged.
# Call resolve_cluster_provider once, before any other function here.

# shellcheck shell=bash
# SC2154 is off for the same reason as in hack/lib/registry.sh: CLUSTER_NAME is the caller-set
# contract listed above, not an oversight.
# shellcheck disable=SC2154

# The context name the k3s installer writes into /etc/rancher/k3s/k3s.yaml, and so into any
# ~/.kube/config copied from it. Overridable for a kubeconfig that renamed it.
readonly K3S_CONTEXT="${K3S_CONTEXT:-default}"

resolve_cluster_provider() {
  if [ -z "${CLUSTER_PROVIDER:-}" ]; then
    if command -v k3s >/dev/null 2>&1 && systemctl is-active --quiet k3s 2>/dev/null; then
      CLUSTER_PROVIDER=k3s
    else
      CLUSTER_PROVIDER=kind
    fi
  fi

  case "$CLUSTER_PROVIDER" in
    kind | k3s) ;;
    *) fail "CLUSTER_PROVIDER must be kind or k3s, got '${CLUSTER_PROVIDER}'" ;;
  esac

  readonly CLUSTER_PROVIDER
  export CLUSTER_PROVIDER
  log "cluster provider: $CLUSTER_PROVIDER (set CLUSTER_PROVIDER to override)"
}

# The tools a provider needs on top of kubectl. k3s needs none of its own for kubectl access; its
# `k3s ctr` is only reached through sudo when an image has to be imported.
cluster_cli_tools() {
  case "$CLUSTER_PROVIDER" in
    kind) printf '%s\n' docker kind ;;
    k3s) printf '%s\n' k3s ;;
  esac
}

# For messages. On k3s the repository's cluster name names nothing, so it is not printed.
cluster_describe() {
  case "$CLUSTER_PROVIDER" in
    kind) printf 'kind cluster %s\n' "$CLUSTER_NAME" ;;
    k3s) printf "the host's k3s cluster\n" ;;
  esac
}

cluster_context() {
  case "$CLUSTER_PROVIDER" in
    kind) printf 'kind-%s\n' "$CLUSTER_NAME" ;;
    k3s) printf '%s\n' "$K3S_CONTEXT" ;;
  esac
}

# Capture first, match second: `kind get clusters | grep -q` lets grep close the pipe on its first
# match and SIGPIPE kind.
cluster_exists() {
  case "$CLUSTER_PROVIDER" in
    kind)
      local clusters
      clusters="$(kind get clusters 2>/dev/null || true)"
      printf '%s\n' "$clusters" | grep -Fxq -- "$CLUSTER_NAME"
      ;;
    k3s)
      systemctl cat k3s >/dev/null 2>&1
      ;;
  esac
}

# The version is optional: without one, kind uses the node image its own release defaults to, which
# is what the e2e cluster has always done.
cluster_create() {
  local kubernetes_version="${1:-}"

  case "$CLUSTER_PROVIDER" in
    kind)
      local -a image_args=()
      if [ -n "$kubernetes_version" ]; then
        image_args=(--image "kindest/node:${kubernetes_version}")
      fi
      log "creating kind cluster $CLUSTER_NAME${kubernetes_version:+ on Kubernetes $kubernetes_version}"
      kind create cluster --name "$CLUSTER_NAME" "${image_args[@]}"
      ;;
    k3s)
      # Installing k3s changes the host -- a system service, a CNI, iptables rules -- which is a
      # decision for whoever owns the machine, not a side effect of a repository script.
      fail "k3s is not installed; install it with the k3s installer (https://docs.k3s.io/quick-start), then re-run"
      ;;
  esac
}

# kind pins the node image, so the version must match exactly. k3s is upgraded by its own
# installer and reports versions like v1.36.5+k3s1, so only the minor version is held to the pin:
# that is the level at which API removals and behavior changes happen.
cluster_version_matches() {
  local actual="$1" expected="$2"

  case "$CLUSTER_PROVIDER" in
    kind) [ "$actual" = "$expected" ] ;;
    k3s)
      local actual_minor expected_minor
      actual_minor="$(printf '%s\n' "$actual" | sed -E 's/^(v[0-9]+\.[0-9]+).*/\1/')"
      expected_minor="$(printf '%s\n' "$expected" | sed -E 's/^(v[0-9]+\.[0-9]+).*/\1/')"
      [ "$actual_minor" = "$expected_minor" ]
      ;;
  esac
}

cluster_version_hint() {
  case "$CLUSTER_PROVIDER" in
    kind) printf 'delete it with hack/shutdown-cluster.sh delete and re-run\n' ;;
    k3s) printf 'reinstall k3s with INSTALL_K3S_VERSION set to a %s release\n' "$1" ;;
  esac
}

# Whether chart images are pulled on the host and imported before Helm schedules pods. kind needs
# it: its node may inherit a proxy pointing at host.docker.internal, which a Linux kind node cannot
# resolve. k3s does not: its containerd pulls through the proxy set in its own service
# environment, so preloading would only duplicate every image into Docker's store as well.
cluster_preloads_images() {
  [ "$CLUSTER_PROVIDER" = kind ]
}

cluster_has_image() {
  local image="$1"

  case "$CLUSTER_PROVIDER" in
    kind) docker exec "${CLUSTER_NAME}-control-plane" crictl inspecti "$image" >/dev/null 2>&1 ;;
    k3s) sudo k3s crictl inspecti "$image" >/dev/null 2>&1 ;;
  esac
}

# Read an image archive (`docker save` output) on stdin into the cluster's containerd. Both
# providers run containerd and both keep Kubernetes images in its k8s.io namespace; they differ only
# in how its socket is reached -- inside a node container for kind, as root on the host for k3s.
cluster_import_image_archive() {
  case "$CLUSTER_PROVIDER" in
    kind)
      docker exec -i "${CLUSTER_NAME}-control-plane" \
        ctr --namespace=k8s.io images import - >/dev/null
      ;;
    k3s)
      sudo k3s ctr --namespace=k8s.io images import - >/dev/null
      ;;
  esac
}

# Copy an image from the host Docker daemon into the cluster. The image must already be local:
# pulled by the caller, or built by `make docker-build`.
cluster_load_image() {
  local image="$1"

  if docker save "$image" | cluster_import_image_archive; then
    return
  fi

  # Docker's containerd image store can export an incomplete archive for some OCI-indexed images.
  # Fall back to flattening the image filesystem while preserving the runtime fields Kubernetes
  # needs to start the container.
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
  docker save "$image" | cluster_import_image_archive
}

# Whether a throwaway cluster can be created for a test run and deleted after it. kind clusters are
# cheap and isolated; the host's k3s is neither, so e2e runs against it and leaves it running.
cluster_is_disposable() {
  [ "$CLUSTER_PROVIDER" = kind ]
}

# kind has no stop/start of its own: a cluster is a set of Docker containers, so stopping the
# containers is stopping the cluster. Node state lives in the container filesystems and survives.
#
# k3s is a service, but `systemctl stop k3s` deliberately leaves the pods' containers running so
# that a k3s upgrade does not restart workloads. Stopping the cluster therefore means k3s's own
# killall script, which stops the service, the containers and the network it set up. State in
# /var/lib/rancher/k3s survives, and the service still starts at boot.
cluster_stop() {
  case "$CLUSTER_PROVIDER" in
    kind)
      local nodes
      nodes="$(kind get nodes --name "$CLUSTER_NAME" 2>/dev/null || true)"
      [ -n "$nodes" ] || fail "cluster $CLUSTER_NAME has no nodes; delete it instead"
      # `docker stop` on an already-stopped container is a no-op success, so running this twice
      # is safe.
      # shellcheck disable=SC2086
      docker stop $nodes >/dev/null
      ;;
    k3s)
      sudo /usr/local/bin/k3s-killall.sh >/dev/null
      ;;
  esac
}

cluster_start() {
  case "$CLUSTER_PROVIDER" in
    kind)
      local nodes
      nodes="$(kind get nodes --name "$CLUSTER_NAME" 2>/dev/null || true)"
      [ -n "$nodes" ] || fail "cluster $CLUSTER_NAME has no nodes"
      # shellcheck disable=SC2086
      docker start $nodes >/dev/null
      # The API server gets a new container IP on restart, so the kubeconfig entry written at
      # create time is stale until kind rewrites it.
      log "refreshing kubeconfig for kind-$CLUSTER_NAME"
      kind export kubeconfig --name "$CLUSTER_NAME"
      ;;
    k3s)
      # The API server listens on the host's 127.0.0.1:6443 across restarts, so the kubeconfig
      # stays valid and needs no refresh.
      sudo systemctl start k3s
      ;;
  esac
}

cluster_delete() {
  case "$CLUSTER_PROVIDER" in
    kind)
      log "deleting $(cluster_describe)"
      kind delete cluster --name "$CLUSTER_NAME"
      ;;
    k3s)
      # Deleting k3s is uninstalling it from the host, which removes every workload on it -- not
      # only this repository's. Name the command and leave the decision to the machine's owner.
      fail "k3s is the host's own cluster; this script will not uninstall it. To remove it, run /usr/local/bin/k3s-uninstall.sh yourself"
      ;;
  esac
}
