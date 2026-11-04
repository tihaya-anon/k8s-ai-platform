# VS Code devcontainer image: bake the "download once" tools that post-install.sh used to
# fetch on every rebuild, so a Rebuild is Docker layer cache instead of a fresh download.
#
# The user creation, sudo and the docker socket are still handled by the devcontainer
# features in devcontainer.json (common-utils, docker-outside-of-docker, git): those run
# after this image builds, and docker-outside-of-docker in particular resolves the host's
# docker group GID at container start, which an image build cannot know. This file only
# owns the tools that belong in a layer -- including the Docker CLI and the package set
# common-utils would otherwise install, for the reasons at those two layers below.
#
# Kept separate from agent.Dockerfile on purpose: that image carries the AI CLIs, Neovim and
# the rest of the agent sandbox; this one is the lighter editor image. It does borrow that file's
# shape: every download sits in a stage of its own, off the base image rather than off a common
# apt layer, so BuildKit runs them concurrently with each other and with the apt work in the final
# stage, and a version bump in one of them invalidates one stage instead of every layer below it.
# Each stage leaves its output under /opt, and the final stage is COPY plus apt and nothing else.
#
# Built by hack/devcontainer.sh, not by the devcontainer CLI: the script pins the base to a
# digest, passes the host's proxy and mirror settings through, and stamps the revision label that
# `sync` reads back from the registry. It builds on the host network by default so that a proxy
# bound to the host's loopback is reachable from the build; DEVCONTAINER_BUILD_NETWORK overrides
# that on a host where the downloads below work directly.

ARG GO_IMAGE=golang:1.27-bookworm
# Same pin as agent.Dockerfile. Node is here only for the repository's formatter toolchain:
# Prettier's JSON key sorting is a plugin, and a plugin has to be resolvable from the repository
# rather than from whichever global install is on PATH, so package.json owns the pins and
# post-install.sh restores them with `npm ci`.
ARG NODE_IMAGE=node:24-bookworm
FROM ${NODE_IMAGE} AS node

# The Go language-server tools, compiled in a stage of their own and copied in as binaries. They
# could be `go install`ed directly into the final image -- this file used to -- but that leaves
# root's artifacts behind in GOPATH: /go/pkg/mod is a cache mount and vanishes, while
# /go/pkg/sumdb is not and stays in the layer as root:0755. The agent user (uid 1000, created
# later by common-utils) then downloads modules fine and dies verifying them, which fails
# onCreateCommand, which makes VS Code skip the rest of the lifecycle -- including installing the
# extensions from customizations.vscode. Building here keeps /go untouched in the final image, at
# the 1777 the golang image sets, the same shape agent.Dockerfile's go-tools stage produces.
#
# gopls tracks the newest Go closely -- v0.22 needs Go 1.26, v0.24 needs 1.27 -- so GOPLS_VERSION
# moves with GO_IMAGE, never ahead of it. Two separate `go install` calls: a single command cannot
# name two modules at different versions.
FROM ${GO_IMAGE} AS go-tools
ARG http_proxy
ARG https_proxy
ARG no_proxy
ARG GOPLS_VERSION=v0.24.0
ARG DELVE_VERSION=v1.27.2
ARG GOPROXY=https://proxy.golang.org,direct
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    set -eu; \
    GOPROXY="${GOPROXY}" GOBIN=/opt/go-tools/bin go install "golang.org/x/tools/gopls@${GOPLS_VERSION}"; \
    GOPROXY="${GOPROXY}" GOBIN=/opt/go-tools/bin go install "github.com/go-delve/delve/cmd/dlv@${DELVE_VERSION}"

# The cluster CLIs, each a single binary from a release page. Off ${GO_IMAGE} rather than off a
# shared apt layer: curl, tar and the CA bundle are all this needs and the golang image has them,
# so the stage starts at once instead of waiting for the final stage's apt to finish.
#
# The stall guard and retries are load-bearing: a proxy can accept the connection and then send
# nothing, and without --speed-limit/--speed-time curl waits on that socket forever. Same
# reasoning as agent.Dockerfile.
FROM ${GO_IMAGE} AS k8s-tools
ARG http_proxy
ARG https_proxy
ARG no_proxy
ARG TARGETARCH
ARG GITHUB_MIRROR=https://github.com
ARG KIND_VERSION=v0.32.0
ARG KUBEBUILDER_VERSION=4.9.0
ARG KUBECTL_VERSION=v1.34.8
# Helm is here because hack/set-up-cluster.sh installs KServe/Kueue with it and the VS Code
# Kubernetes extension wants it for its chart features. Pinned to the host's version.
ARG HELM_VERSION=v3.21.4
RUN set -eu; \
    curl_opts="--http1.1 --connect-timeout 15 --retry 5 --retry-delay 2 --retry-all-errors --speed-limit 1024 --speed-time 60"; \
    arch="${TARGETARCH:-amd64}"; \
    mkdir -p /opt/k8s-tools/bin; \
    curl $curl_opts -fsSLo /opt/k8s-tools/bin/kind \
        "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-${arch}"; \
    curl $curl_opts -fsSLo /opt/k8s-tools/bin/kubectl \
        "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${arch}/kubectl"; \
    curl $curl_opts -fsSLo /opt/k8s-tools/bin/kubebuilder \
        "${GITHUB_MIRROR}/kubernetes-sigs/kubebuilder/releases/download/v${KUBEBUILDER_VERSION}/kubebuilder_linux_${arch}"; \
    curl $curl_opts -fsSL "https://get.helm.sh/helm-${HELM_VERSION}-linux-${arch}.tar.gz" \
        | tar -xz -C /tmp; \
    mv "/tmp/linux-${arch}/helm" /opt/k8s-tools/bin/helm; \
    rm -rf "/tmp/linux-${arch}"; \
    chmod +x /opt/k8s-tools/bin/*; \
    /opt/k8s-tools/bin/helm version --short

# The shell's own tools, in their own stage for the same reason and because their pins move on a
# different schedule from the cluster CLIs'. k9s is in here rather than in the cluster: it reads
# ~/.kube/config like kubectl does, so it needs no install into the cluster and no RBAC of its
# own -- see docs/tooling/dashboards.md.
#
# The zsh plugins land under /opt because the agent user does not exist at image build time --
# common-utils creates it when the features run -- so post-install.sh symlinks them into
# $HOME/.local/share/zsh/plugins, the path hack/agent-home/.zshrc reads.
FROM ${GO_IMAGE} AS shell-tools
ARG http_proxy
ARG https_proxy
ARG no_proxy
ARG TARGETARCH

# shellcheck publishes only .tar.xz, and `tar -J` shells out to xz, which the golang base does not
# have. Installed here rather than taken from the final stage's list, because a download stage has
# to stand on its own -- which also means repeating the mirror rewrite: that one lives in the final
# stage and this image's sources are still pointed at deb.debian.org at this point.
ARG APT_MIRROR=mirrors.aliyun.com
RUN set -eu; \
    export DEBIAN_FRONTEND=noninteractive; \
    for f in /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list; do \
        if [ -f "$f" ]; then sed -i "s|deb.debian.org|${APT_MIRROR}|g" "$f"; fi; \
    done; \
    apt-get update \
    && apt-get install -y --no-install-recommends xz-utils \
    && rm -rf /var/lib/apt/lists/*
ARG GITHUB_MIRROR=https://github.com
ARG K9S_VERSION=v0.51.0
ARG STARSHIP_VERSION=1.25.0
# Downloaded rather than apt-installed: bookworm ships 0.9.0, and the VS Code extension nags that
# it wants 0.11.0 or newer. The pin matters beyond the nag -- devcontainer.json points the
# extension at this binary and the pre-commit hook runs the same one, so the editor and the gate
# cannot disagree about what passes, and that is only true if the version is fixed here.
# agent.Dockerfile pins the same version for the same reason.
ARG SHELLCHECK_VERSION=v0.11.0
ARG ZSH_AUTOSUGGESTIONS_COMMIT=85919cd1ffa7d2d5412f6d3fe437ebdbeeec4fc5
ARG ZSH_SYNTAX_HIGHLIGHTING_COMMIT=e6683f20e5c38d3e5447f45845d04ddda75d0f23
RUN set -eu; \
    curl_opts="--http1.1 --connect-timeout 15 --retry 5 --retry-delay 2 --retry-all-errors --speed-limit 1024 --speed-time 60"; \
    arch="${TARGETARCH:-amd64}"; \
    mkdir -p /opt/shell-tools/bin; \
    curl $curl_opts -fsSL \
        "${GITHUB_MIRROR}/derailed/k9s/releases/download/${K9S_VERSION}/k9s_Linux_${arch}.tar.gz" \
        | tar -C /opt/shell-tools/bin -xz k9s; \
    case "$arch" in \
      amd64) star_arch=x86_64-unknown-linux-gnu ;; \
      arm64) star_arch=aarch64-unknown-linux-musl ;; \
      *) echo "unsupported TARGETARCH: $arch" >&2; exit 1 ;; \
    esac; \
    curl $curl_opts -fsSL \
        "${GITHUB_MIRROR}/starship/starship/releases/download/v${STARSHIP_VERSION}/starship-${star_arch}.tar.gz" \
        | tar -C /opt/shell-tools/bin -xz starship; \
    case "$arch" in \
      amd64) sc_arch=x86_64 ;; \
      arm64) sc_arch=aarch64 ;; \
    esac; \
    curl $curl_opts -fsSL \
        "${GITHUB_MIRROR}/koalaman/shellcheck/releases/download/${SHELLCHECK_VERSION}/shellcheck-${SHELLCHECK_VERSION}.linux.${sc_arch}.tar.xz" \
        | tar -C /tmp -xJ; \
    mv "/tmp/shellcheck-${SHELLCHECK_VERSION}/shellcheck" /opt/shell-tools/bin/shellcheck; \
    rm -rf "/tmp/shellcheck-${SHELLCHECK_VERSION}"; \
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

FROM ${GO_IMAGE}

# Proxy passthrough. With --network=host, 127.0.0.1 is the host, so the proxy is reachable and
# github.com / proxy.golang.org go through it. Overridable via --build-arg for other networks.
ARG http_proxy
ARG https_proxy
ARG no_proxy

# The pins live in the stages that read them. An ARG is scoped to the stage that declares it, so
# repeating one here would only add a build-cache key to a stage that never uses it.

# apt, in one invocation, and deliberately a long list.
#
# The first group is this image's own: zsh and the small tools the golang image does not ship
# (git, curl and make are already in the base). Everything after it is the package set
# ghcr.io/devcontainers/features/common-utils installs, which is the real reason this layer is
# big. That feature's install.sh builds a missing_package_list with dpkg-query and skips its apt
# block when nothing is missing, so pre-installing the set here is what keeps a Rebuild from
# spending ~150s on it -- most of that not download but the man-db, manpages-dev and locales dpkg
# triggers, which no mirror makes faster.
#
# The skip is only ever partial on bookworm, and that is not worth trying to fix: the feature also
# lists libgcc1, which Debian 12 has only as a virtual package provided by libgcc-s1. dpkg-query
# never reports a virtual package installed, so missing_package_list keeps one entry, the feature
# still runs `apt-get update`, and `apt-get install libgcc1` then resolves through Provides and
# downloads nothing. Seconds instead of minutes, which is the whole win available here.
#
# libicu72 and liblttng-ust1 are bookworm's concrete names for the feature's libicu[0-9][0-9] and
# liblttng-ust[0-9] globs -- dpkg-query matches the glob against what is installed, so the
# concrete package satisfies it. Move those two when the base moves off bookworm.
#
# DEBIAN_FRONTEND is exported inside the RUN rather than set as an ENV: locales, dialog and tzdata
# all drive debconf from their postinst and a build has no tty for the prompt, but an ENV would
# also hand noninteractive to every shell the person later opens in the container.
#
# The sources are rewritten to a China mirror: the CLI builds the features on the bridge network,
# where deb.debian.org is ~15 kB/s, and the rewrite also covers that feature build because it
# inherits this image's sources.
ARG APT_MIRROR=mirrors.aliyun.com
RUN set -eu; \
    export DEBIAN_FRONTEND=noninteractive; \
    for f in /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list; do \
        if [ -f "$f" ]; then sed -i "s|deb.debian.org|${APT_MIRROR}|g" "$f"; fi; \
    done; \
    apt-get update \
    && apt-get install -y --no-install-recommends \
        fd-find \
        ripgrep \
        zsh \
        apt-transport-https \
        apt-utils \
        bash-completion \
        bubblewrap \
        bzip2 \
        ca-certificates \
        curl \
        dialog \
        dirmngr \
        gnupg2 \
        htop \
        init-system-helpers \
        iproute2 \
        iptables \
        jq \
        less \
        libgssapi-krb5-2 \
        libicu72 \
        libkrb5-3 \
        liblttng-ust1 \
        libssl3 \
        libstdc++6 \
        locales \
        lsb-release \
        lsof \
        man-db \
        manpages \
        manpages-dev \
        nano \
        ncdu \
        net-tools \
        openssh-client \
        procps \
        psmisc \
        rsync \
        slirp4netns \
        socat \
        strace \
        sudo \
        tree \
        unzip \
        util-linux \
        vim-tiny \
        wget \
        xz-utils \
        zip \
        zlib1g \
    && rm -rf /var/lib/apt/lists/*

# The Docker CLI, buildx and the compose plugin, baked rather than left to the
# docker-outside-of-docker feature. The feature is still in devcontainer.json and still does the
# part only it can do -- resolving the host's docker group GID when the container starts -- but its
# *install* step pulls these three packages from download.docker.com, and the feature build runs on
# the bridge network where no proxy is reachable, so that download is the slowest layer of a
# Rebuild. install.sh skips the download when `docker` is already on PATH, so putting them in the
# image collapses that step to its entrypoint work.
#
# A layer of its own rather than part of the apt list above: the Docker repository is a second apt
# source and its packages move on Docker's release schedule, not Debian's.
#
# Through the same China mirror as the Debian sources above, for the same reason. The repository's
# architecture names (amd64/arm64) are the same strings TARGETARCH uses, so no translation table is
# needed here -- unlike the starship download in the shell-tools stage.
#
# The update is scoped to docker.list: the layer above ends with `rm -rf /var/lib/apt/lists/*`, so a
# bare `apt-get update` here would re-fetch the whole Debian index to install three packages that
# need nothing from it -- all three are static Go binaries whose only dependency, libc6, is already
# in the base, and --no-install-recommends drops the git/pigz/xz-utils they merely suggest.
# SourceParts=- is the half that does the work: without it apt reads sources.list.d in full and the
# Debian entries come back. Drop both -o flags if a package added here ever needs Debian's index.
ARG TARGETARCH
ARG DOCKER_MIRROR=https://mirrors.aliyun.com/docker-ce
RUN set -eu; \
    export DEBIAN_FRONTEND=noninteractive; \
    arch="${TARGETARCH:-amd64}"; \
    install -m 0755 -d /etc/apt/keyrings; \
    curl --http1.1 --connect-timeout 15 --retry 5 --retry-delay 2 --retry-all-errors \
        -fsSL "${DOCKER_MIRROR}/linux/debian/gpg" -o /etc/apt/keyrings/docker.asc; \
    chmod a+r /etc/apt/keyrings/docker.asc; \
    echo "deb [arch=${arch} signed-by=/etc/apt/keyrings/docker.asc] ${DOCKER_MIRROR}/linux/debian bookworm stable" \
        > /etc/apt/sources.list.d/docker.list; \
    apt-get update \
        -o Dir::Etc::SourceList=/etc/apt/sources.list.d/docker.list \
        -o Dir::Etc::SourceParts=- \
    && apt-get install -y --no-install-recommends \
        docker-ce-cli \
        docker-buildx-plugin \
        docker-compose-plugin \
    && rm -rf /var/lib/apt/lists/*; \
    docker --version

# node, npm and npx. A whole-prefix copy, the shape agent.Dockerfile uses: the golang image's
# /usr/local/go is untouched because COPY merges into the directory rather than replacing it.
COPY --from=node /usr/local/ /usr/local/

# The download stages' output. /usr/local/bin rather than /go/bin: both are on the golang image's
# PATH and that is how the Go extension finds a tool, but /go is world-writable, so a baked binary
# there is one the container's own user could overwrite.
COPY --from=k8s-tools /opt/k8s-tools/bin/ /usr/local/bin/
COPY --from=shell-tools /opt/shell-tools/bin/ /usr/local/bin/
COPY --from=shell-tools /opt/zsh-plugins/ /opt/zsh-plugins/
COPY --from=go-tools /opt/go-tools/bin/gopls /usr/local/bin/gopls
COPY --from=go-tools /opt/go-tools/bin/dlv /usr/local/bin/dlv

# The commit this image was built from. `hack/devcontainer.sh sync` reads it back from the registry
# manifest to decide whether latest may move, without pulling the image. Declared last so a new
# commit invalidates one metadata layer instead of the layers above it.
ARG GIT_SHA=unknown
LABEL org.opencontainers.image.revision="${GIT_SHA}"
