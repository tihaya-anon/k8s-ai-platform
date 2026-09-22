# Development sandbox for AI coding agents.
#
# This is intentionally separate from the product Dockerfile. The product image stays scratch and
# offline; this image is only for editing, building, and testing this repository in a bounded
# workspace.
FROM node:24-bookworm AS node

FROM golang:1.25-bookworm

ARG USERNAME=agent
ARG USER_UID=1000
ARG USER_GID=1000
ARG PI_VERSION=0.85.1
ARG CODEX_VERSION=0.154.0
ARG MARKDOWNLINT_VERSION=0.23.2
ARG PRETTIER_VERSION=3.9.6

# The editor side of the sandbox. Pinned to the versions the host runs, so a session in the
# container behaves like a session outside it rather than like a slightly different editor.
ARG NVIM_VERSION=0.12.5
ARG STARSHIP_VERSION=1.25.0
# ble.sh cuts tagged releases rarely; the host runs 0.4.0-devel4+b99cadb, which upstream ships as
# a dated nightly asset. Pinning the asset, not "nightly", keeps the build reproducible.
ARG BLESH_NIGHTLY=20260310+b99cadb

# Every download below comes from GitHub releases. Point this at a mirror when the build network
# cannot reach github.com directly, e.g. --build-arg GITHUB_MIRROR=https://ghproxy.net/https://github.com
ARG GITHUB_MIRROR=https://github.com

# Configurable package mirror; override for whatever network the image is built on, e.g.
#   docker build --build-arg APT_MIRROR=deb.debian.org -f Dockerfile.agent .
ARG APT_MIRROR=mirrors.aliyun.com

ENV DEBIAN_FRONTEND=noninteractive \
    GOPATH=/home/agent/go \
    GOCACHE=/home/agent/.cache/go-build \
    GOMODCACHE=/home/agent/go/pkg/mod

# The base has moved between apt source layouts across releases (deb822 debian.sources vs a plain
# sources.list), so rewrite whichever is present. `if` rather than `[ -f ] && sed` because a
# trailing failed test would leave the loop exit status non-zero and break the `&&` chain.
RUN for f in /etc/apt/sources.list.d/debian.sources /etc/apt/sources.list; do \
        if [ -f "$f" ]; then sed -i "s|deb.debian.org|${APT_MIRROR}|g" "$f"; fi; \
    done \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        jq \
        less \
        make \
        procps \
        ripgrep \
        tree \
        bash-completion \
        fd-find \
        unzip \
        xz-utils \
    && rm -rf /var/lib/apt/lists/*

# LazyVim looks for `fd`; Debian ships the binary as `fdfind` to avoid a name clash.
RUN ln -sf "$(command -v fdfind)" /usr/local/bin/fd

COPY --from=node /usr/local/ /usr/local/

# Neovim, starship and ble.sh, from upstream release tarballs rather than apt: Debian stable's
# Neovim is ancient and neither starship nor ble.sh is packaged at all.
#
# Neovim here is $EDITOR, not an IDE. `git commit` without -m, `git rebase -i` and an agent
# shelling out all need an editor to exist; editing this repository happens on the host, against
# the same inodes through the bind mount. The IDE configuration and its language servers were
# removed for that reason -- see docs/editors.md.
#
# TARGETARCH comes from BuildKit and is the only architecture switch in the image.
ARG TARGETARCH
RUN set -eu; \
    case "${TARGETARCH:-amd64}" in \
      amd64) nvim_arch=linux-x86_64; star_arch=x86_64-unknown-linux-gnu ;; \
      arm64) nvim_arch=linux-arm64;  star_arch=aarch64-unknown-linux-gnu ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl -fsSL -o /tmp/nvim.tar.gz \
      "${GITHUB_MIRROR}/neovim/neovim/releases/download/v${NVIM_VERSION}/nvim-${nvim_arch}.tar.gz"; \
    tar -C /opt -xzf /tmp/nvim.tar.gz; \
    mv "/opt/nvim-${nvim_arch}" /opt/nvim; \
    ln -sf /opt/nvim/bin/nvim /usr/local/bin/nvim; \
    curl -fsSL -o /tmp/starship.tar.gz \
      "${GITHUB_MIRROR}/starship/starship/releases/download/v${STARSHIP_VERSION}/starship-${star_arch}.tar.gz"; \
    tar -C /usr/local/bin -xzf /tmp/starship.tar.gz starship; \
    curl -fsSL -o /tmp/blesh.tar.xz \
      "${GITHUB_MIRROR}/akinomyoga/ble.sh/releases/download/nightly/ble-nightly-${BLESH_NIGHTLY}.tar.xz"; \
    mkdir -p /usr/share/blesh; \
    tar -C /usr/share/blesh --strip-components=1 -xJf /tmp/blesh.tar.xz; \
    rm -rf /tmp/nvim.tar.gz /tmp/starship.tar.gz /tmp/blesh.tar.xz; \
    nvim --version | head -1; \
    starship --version | head -1

# Declared here rather than with the version pins at the top of the file, and deliberately. Docker
# invalidates every layer after a changed instruction, so a pin that lives above the Neovim
# download costs a rebuild of it every time this one moves.
ARG CLAUDE_VERSION=2.1.278

# The second package mirror, for the same reason as APT_MIRROR: the agent CLIs are by far the
# largest downloads in the image and the default registry is the slow path on some networks.
ARG NPM_REGISTRY=https://registry.npmjs.org

# The cache mount is not an optimisation here, it is what makes a retry survivable. codex and
# claude-code ship their real binaries as per-platform optionalDependencies -- around 570MB
# unpacked between them -- and without a cache every interrupted build starts the download again.
# Cache mounts live outside the image, so the layer's size is unchanged.
RUN --mount=type=cache,target=/root/.npm \
    npm install -g --registry="${NPM_REGISTRY}" \
        "@earendil-works/pi-coding-agent@${PI_VERSION}" \
        "@openai/codex@${CODEX_VERSION}" \
        "@anthropic-ai/claude-code@${CLAUDE_VERSION}" \
        "markdownlint-cli2@${MARKDOWNLINT_VERSION}" \
        "prettier@${PRETTIER_VERSION}"
# No `npm cache clean` any more: the cache is a build mount, so it never reached the image, and
# cleaning it would only throw away what makes the next build fast.

# CLAUDE_CONFIG_DIR is set explicitly so every piece of Claude Code state -- credentials, session
# history, installed skills and its config file -- lands under one directory, which agent.Makefile
# backs with a single named volume. Left to the default, the config file sits beside the directory
# at $HOME and is lost on every `docker run --rm`.
#
# LANG is unset in a container, and an unset LANG means the C locale: ble.sh warns about it, and
# anything that draws a box or a glyph -- starship's prompt, Neovim's UI -- degrades to ASCII.
# C.UTF-8 is built into glibc, so this needs no locales package.
ENV CLAUDE_CONFIG_DIR=/home/agent/.claude \
    LANG=C.UTF-8

RUN groupadd --gid "${USER_GID}" "${USERNAME}" \
    && useradd \
        --uid "${USER_UID}" \
        --gid "${USER_GID}" \
        --create-home \
        --shell /bin/bash \
        "${USERNAME}" \
    && mkdir -p \
        /workspace/k8s-ai-platform \
        "${GOPATH}/pkg/mod" \
        "${GOCACHE}" \
        /home/agent/.pi/agent \
        /home/agent/.codex \
        /home/agent/.claude \
    && chown -R "${USERNAME}:${USERNAME}" /workspace /home/agent

# Shell and editor configuration, tracked in hack/agent-home/ so the container's environment is
# reviewed in the same diff as the code. Copied, not mounted: the read-only targets mount only the
# workspace, and a shell that loses its prompt and its editor under --read-only is not usable.
COPY --chown=${USER_UID}:${USER_GID} hack/agent-home/ /home/agent/

# Baked in rather than run from the mount: the read-only targets cannot execute a script they
# mount :ro without also trusting the mount, and the entrypoint has to work before the
# workspace is known to be present at all.
COPY hack/agent-entrypoint.sh /usr/local/bin/agent-entrypoint
RUN chmod 0755 /usr/local/bin/agent-entrypoint

USER ${USERNAME}
WORKDIR /workspace/k8s-ai-platform

# Arms core.hooksPath on every entry, then hands over to CMD or to the docker run arguments.
ENTRYPOINT ["/usr/local/bin/agent-entrypoint"]
CMD ["/bin/bash"]
