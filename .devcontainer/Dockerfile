# VS Code devcontainer image: bake the "download once" tools that post-install.sh used to
# fetch on every rebuild, so a Rebuild is Docker layer cache instead of a fresh download.
#
# The user creation, sudo and the docker socket are still handled by the devcontainer
# features in devcontainer.json (common-utils, docker-outside-of-docker, git): those run
# after this image builds, and docker-outside-of-docker in particular resolves the host's
# docker group GID at container start, which an image build cannot know. This file only
# owns the tools that belong in a layer.
#
# Kept separate from agent.Dockerfile on purpose: that image carries the AI CLIs, Neovim and
# the rest of the agent sandbox; this one is the lighter editor image.
#
# Built by hack/devcontainer.sh, not by the devcontainer CLI: the script pins the base to a
# digest, passes the host's proxy and mirror settings through, and stamps the revision label that
# `sync` reads back from the registry. It builds on the host network by default so that a proxy
# bound to the host's loopback is reachable from the build; DEVCONTAINER_BUILD_NETWORK overrides
# that on a host where the downloads below work directly.

ARG GO_IMAGE=golang:1.25-bookworm

# The Go language-server tools, compiled in a stage of their own and copied in as binaries. They
# could be `go install`ed directly into the final image -- this file used to -- but that leaves
# root's artifacts behind in GOPATH: /go/pkg/mod is a cache mount and vanishes, while
# /go/pkg/sumdb is not and stays in the layer as root:0755. The agent user (uid 1000, created
# later by common-utils) then downloads modules fine and dies verifying them, which fails
# onCreateCommand, which makes VS Code skip the rest of the lifecycle -- including installing the
# extensions from customizations.vscode. Building here keeps /go untouched in the final image, at
# the 1777 the golang image sets, the same shape agent.Dockerfile's go-tools stage produces.
#
# gopls v0.22+ needs Go 1.26, so v0.21.x is the newest this toolchain builds. Two separate
# `go install` calls: a single command cannot name two modules at different versions.
FROM ${GO_IMAGE} AS go-tools
ARG http_proxy
ARG https_proxy
ARG no_proxy
ARG GOPLS_VERSION=v0.21.1
ARG DELVE_VERSION=v1.27.2
ARG GOPROXY=https://proxy.golang.org,direct
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    set -eu; \
    GOPROXY="${GOPROXY}" GOBIN=/opt/go-tools/bin go install "golang.org/x/tools/gopls@${GOPLS_VERSION}"; \
    GOPROXY="${GOPROXY}" GOBIN=/opt/go-tools/bin go install "github.com/go-delve/delve/cmd/dlv@${DELVE_VERSION}"

FROM ${GO_IMAGE}

# Proxy passthrough. With --network=host, 127.0.0.1 is the host, so the proxy is reachable and
# github.com / proxy.golang.org go through it. Overridable via --build-arg for other networks.
ARG http_proxy
ARG https_proxy
ARG no_proxy

# Same pins as agent.Dockerfile, so the container matches the host.
ARG KIND_VERSION=v0.32.0
ARG KUBEBUILDER_VERSION=4.9.0
ARG KUBECTL_VERSION=v1.34.8
ARG K9S_VERSION=v0.51.0
ARG STARSHIP_VERSION=1.25.0
ARG ZSH_AUTOSUGGESTIONS_COMMIT=85919cd1ffa7d2d5412f6d3fe437ebdbeeec4fc5
ARG ZSH_SYNTAX_HIGHLIGHTING_COMMIT=e6683f20e5c38d3e5447f45845d04ddda75d0f23
ARG GITHUB_MIRROR=https://github.com

# GOPLS_VERSION, DELVE_VERSION and GOPROXY are declared in the go-tools stage instead: an ARG is
# scoped to the stage that declares it, so repeating them here would only add build-cache keys to
# a stage that never reads them.

# apt: zsh and the small tools the golang image does not ship. git/curl/make are already in
# the base. The sources are rewritten to a China mirror: the CLI builds the features (common-utils
# etc.) on the bridge network, where deb.debian.org is ~15 kB/s, and the rewrite also covers that
# feature build because it inherits this image's sources.
ARG APT_MIRROR=mirrors.aliyun.com
RUN set -eu; \
    for f in /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list; do \
        if [ -f "$f" ]; then sed -i "s|deb.debian.org|${APT_MIRROR}|g" "$f"; fi; \
    done; \
    apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        fd-find \
        jq \
        ripgrep \
        zsh \
    && rm -rf /var/lib/apt/lists/*

# Single-binary K8s tools plus starship. k9s is in here rather than in the cluster: it reads
# ~/.kube/config like kubectl does, so it needs no install into the cluster and no RBAC of its
# own -- see docs/tooling/dashboards.md. The stall guard and retries are load-bearing: a
# proxy can accept the connection and then send nothing, and without --speed-limit/--speed-time
# curl waits on that socket forever. Same reasoning as agent.Dockerfile.
ARG TARGETARCH
RUN set -eu; \
    curl_opts="--http1.1 --connect-timeout 15 --retry 5 --retry-delay 2 --retry-all-errors --speed-limit 1024 --speed-time 60"; \
    arch="${TARGETARCH:-amd64}"; \
    curl $curl_opts -fsSLo /usr/local/bin/kind \
        "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-${arch}"; \
    chmod +x /usr/local/bin/kind; \
    curl $curl_opts -fsSLo /usr/local/bin/kubectl \
        "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${arch}/kubectl"; \
    chmod +x /usr/local/bin/kubectl; \
    curl $curl_opts -fsSLo /tmp/kubebuilder \
        "${GITHUB_MIRROR}/kubernetes-sigs/kubebuilder/releases/download/v${KUBEBUILDER_VERSION}/kubebuilder_linux_${arch}"; \
    chmod +x /tmp/kubebuilder; \
    mv /tmp/kubebuilder /usr/local/bin/kubebuilder; \
    curl $curl_opts -fsSL \
        "${GITHUB_MIRROR}/derailed/k9s/releases/download/${K9S_VERSION}/k9s_Linux_${arch}.tar.gz" \
        | tar -C /usr/local/bin -xz k9s; \
    case "$arch" in \
      amd64) star_arch=x86_64-unknown-linux-gnu ;; \
      arm64) star_arch=aarch64-unknown-linux-musl ;; \
      *) echo "unsupported TARGETARCH: $arch" >&2; exit 1 ;; \
    esac; \
    curl $curl_opts -fsSL \
        "${GITHUB_MIRROR}/starship/starship/releases/download/v${STARSHIP_VERSION}/starship-${star_arch}.tar.gz" \
        | tar -C /usr/local/bin -xz starship

# Helm. hack/set-up-cluster.sh installs KServe/Kueue with it, and the VS Code Kubernetes extension
# wants it for its chart features. Pinned to the host's version. get.helm.sh is reachable through
# the proxy (this build runs on --network=host).
ARG HELM_VERSION=v3.21.4
RUN set -eu; \
    curl_opts="--http1.1 --connect-timeout 15 --retry 5 --retry-delay 2 --retry-all-errors --speed-limit 1024 --speed-time 60"; \
    arch="${TARGETARCH:-amd64}"; \
    curl $curl_opts -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-${arch}.tar.gz" \
        | tar -xz -C /tmp; \
    mv "/tmp/linux-${arch}/helm" /usr/local/bin/helm; \
    rm -rf "/tmp/linux-${arch}"; \
    helm version --short

# zsh plugins, baked to a system path. The user's $HOME does not exist yet at build time --
# common-utils creates it when the features run -- so post-install.sh symlinks these into
# $HOME/.local/share/zsh/plugins, the path hack/agent-home/.zshrc reads.
RUN set -eu; \
    curl_opts="--http1.1 --connect-timeout 15 --retry 5 --retry-delay 2 --retry-all-errors --speed-limit 1024 --speed-time 60"; \
    for plugin in "zsh-autosuggestions:${ZSH_AUTOSUGGESTIONS_COMMIT}" \
                  "zsh-syntax-highlighting:${ZSH_SYNTAX_HIGHLIGHTING_COMMIT}"; do \
      name="${plugin%%:*}"; commit="${plugin#*:}"; \
      curl $curl_opts -fsSL -o "/tmp/${name}.tar.gz" \
        "${GITHUB_MIRROR}/zsh-users/${name}/archive/${commit}.tar.gz"; \
      mkdir -p "/opt/zsh-plugins/${name}"; \
      tar -C "/opt/zsh-plugins/${name}" --strip-components=1 -xzf "/tmp/${name}.tar.gz"; \
      test -r "/opt/zsh-plugins/${name}/${name}.zsh"; \
    done; \
    rm -f /tmp/zsh-*.tar.gz

# The two binaries from the stage above. /usr/local/bin rather than /go/bin: both are on the
# golang image's PATH and that is how the Go extension finds a tool, but /go is world-writable, so
# a baked binary there is one the container's own user could overwrite.
COPY --from=go-tools /opt/go-tools/bin/gopls /usr/local/bin/gopls
COPY --from=go-tools /opt/go-tools/bin/dlv /usr/local/bin/dlv

# The commit this image was built from. `hack/devcontainer.sh sync` reads it back from the registry
# manifest to decide whether latest may move, without pulling the image. Declared last so a new
# commit invalidates one metadata layer instead of the layers above it.
ARG GIT_SHA=unknown
LABEL org.opencontainers.image.revision="${GIT_SHA}"
