# Development sandbox for AI coding agents.
#
# This is intentionally separate from the product Dockerfile. The product image stays scratch and
# offline; this image is only for editing, building, and testing this repository in a bounded
# workspace.
# Defaults are human-readable tags; hack/agent.sh resolves them to immutable RepoDigests before
# building so a moving Docker Hub tag does not silently change the sandbox.
ARG NODE_IMAGE=node:24-bookworm
ARG GO_IMAGE=golang:1.25-bookworm
FROM ${NODE_IMAGE} AS node

FROM ${GO_IMAGE} AS base

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

FROM base AS editor-tools

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

# Neovim, starship and ble.sh, from upstream release tarballs rather than apt: Debian stable's
# Neovim is ancient and neither starship nor ble.sh is packaged at all.
#
# Neovim here is $EDITOR, not an IDE. `git commit` without -m, `git rebase -i` and an agent
# shelling out all need an editor to exist; editing this repository happens on the host, against
# the same inodes through the bind mount. The IDE configuration and its language servers were
# removed for that reason: the container editor is for commit messages and emergency terminal
# edits, not for carrying the project's IDE policy.
#
# TARGETARCH comes from BuildKit and is the only architecture switch in the image.
ARG TARGETARCH
RUN set -eu; \
    case "${TARGETARCH:-amd64}" in \
      amd64) nvim_arch=linux-x86_64; star_arch=x86_64-unknown-linux-gnu ;; \
      arm64) nvim_arch=linux-arm64;  star_arch=aarch64-unknown-linux-gnu ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    curl --http1.1 --retry 5 --retry-delay 2 --retry-all-errors -fsSL -o /tmp/nvim.tar.gz \
      "${GITHUB_MIRROR}/neovim/neovim/releases/download/v${NVIM_VERSION}/nvim-${nvim_arch}.tar.gz"; \
    tar -C /opt -xzf /tmp/nvim.tar.gz; \
    mv "/opt/nvim-${nvim_arch}" /opt/nvim; \
    ln -sf /opt/nvim/bin/nvim /usr/local/bin/nvim; \
    curl --http1.1 --retry 5 --retry-delay 2 --retry-all-errors -fsSL -o /tmp/starship.tar.gz \
      "${GITHUB_MIRROR}/starship/starship/releases/download/v${STARSHIP_VERSION}/starship-${star_arch}.tar.gz"; \
    tar -C /usr/local/bin -xzf /tmp/starship.tar.gz starship; \
    curl --http1.1 --retry 5 --retry-delay 2 --retry-all-errors -fsSL -o /tmp/blesh.tar.xz \
      "${GITHUB_MIRROR}/akinomyoga/ble.sh/releases/download/nightly/ble-nightly-${BLESH_NIGHTLY}.tar.xz"; \
    mkdir -p /usr/share/blesh; \
    tar -C /usr/share/blesh --strip-components=1 -xJf /tmp/blesh.tar.xz; \
    rm -rf /tmp/nvim.tar.gz /tmp/starship.tar.gz /tmp/blesh.tar.xz; \
    nvim --version | head -1; \
    starship --version | head -1

FROM base AS agent-cli

# Declared in this stage, and deliberately. A Pi/Codex/Claude/OpenCode pin change should rebuild
# only the CLI stage, not the editor-tool stage with the Neovim/starship/ble.sh downloads.
#
# These track what the host actually runs, so a container session and a host session differ in
# their sandbox and in nothing else. Read the host's own answer (`pi --version`) rather than a
# remembered one. The image installs each CLI once and never self-updates it, so the way to move
# a version is to change the pin and rebuild -- see DISABLE_AUTOUPDATER below.
ARG CLAUDE_VERSION=2.1.278
ARG PI_VERSION=1.4.0
ARG CODEX_VERSION=0.154.0
ARG OPENCODE_VERSION=2.1.4
ARG MARKDOWNLINT_VERSION=0.23.2
ARG PRETTIER_VERSION=3.9.6

# The second package mirror, for the same reason as APT_MIRROR: the agent CLIs are by far the
# largest downloads in the image and the default registry is the slow path on some networks.
ARG NPM_REGISTRY=https://registry.npmjs.org

# The cache mount is not an optimisation here, it is what makes a retry survivable. codex and
# claude-code ship their real binaries as per-platform optionalDependencies -- around 570MB
# unpacked between them -- and without a cache every interrupted build starts the download again.
# Cache mounts live outside the image, so the layer's size is unchanged.
# One `npm install -g` per CLI, not one command listing five packages, and that is the whole
# reason this block looks repetitive. npm drops the per-platform optionalDependencies of codex and
# claude-code when several roots are installed in a single global command: it creates
# node_modules/@anthropic-ai/ and leaves it empty, the postinstall then finds no binary to copy,
# and both CLIs ship as stubs that fail on first use. Installed one at a time, the same versions
# and the same registry resolve correctly. Verified in this image, not assumed.
#
# codex needs one more step than that. Its binary is an *aliased* optionalDependency
# (@openai/codex-linux-x64 -> npm:@openai/codex@<version>-linux-x64), and npm installs no aliased
# optional dependency at all under -g -- claude-code's plain scoped one installs fine, which is how
# the two were told apart. So the alias is installed explicitly, by the name codex resolves:
# bin/codex.js calls require.resolve("@openai/codex-<arch>/package.json"), and the module walk
# reaches the configured npm prefix's lib/node_modules, so installing the alias into the same
# prefix makes it visible.
#
# --include=optional is npm's default, and is written out anyway: it is what pulls those binaries,
# and a default this load-bearing should be visible in the file rather than inherited.
#
# --allow-scripts is not optional either. npm 11.19 -- the version in node:24-bookworm -- stopped
# running dependency install scripts unless the package is named, and because strict-allow-scripts
# defaults to false it *warns and continues* rather than failing. Claude Code copies its native
# binary into place from its own postinstall, so without this the build succeeds and the image
# ships a stub. The list is exactly one package: codex's binary arrives as an aliased
# optionalDependency rather than a script, and pi runs fine with esbuild's and protobufjs's
# scripts skipped. Install scripts are arbitrary code from the registry, so it stays that short.
#
# Pi gets --ignore-scripts in addition, which is what upstream's own install instructions use.
# Naming a package in --allow-scripts is an exception to a denial; pi needs no exception at all,
# so its scripts are refused rather than merely not granted. The version is pinned by PI_VERSION
# and enforced by the CLI gate below, so nothing is lost by not letting its dependencies build
# themselves.
#
# OpenCode 2 is @opencode/cli, not opencode-ai: the latter still points at OpenCode 1 and has no
# 2.x releases. Its wrapper package pulls every matching platform build (x64 baseline, musl, ...)
# as optionalDependencies -- about 900MB to end up running one 230MB binary. The official V2
# installer instead downloads only the platform package, and so does this layer. The command names
# the architecture-specific package explicitly; the wrapper's postinstall is never needed.
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.npm set -eu; \
    case "${TARGETARCH:-amd64}" in \
      amd64) codex_arch=linux-x64; opencode_pkg=@opencode/cli-linux-x64 ;; \
      arm64) codex_arch=linux-arm64; opencode_pkg=@opencode/cli-linux-arm64 ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    if [ "$opencode_pkg" = "@opencode/cli-linux-x64" ] && ! grep -qwi avx2 /proc/cpuinfo 2>/dev/null; then \
      opencode_pkg=@opencode/cli-linux-x64-baseline; \
    fi; \
    npm install -g --prefix /opt/agent-cli --include=optional --registry="${NPM_REGISTRY}" \
        --allow-scripts=@anthropic-ai/claude-code \
        "@anthropic-ai/claude-code@${CLAUDE_VERSION}"; \
    npm install -g --prefix /opt/agent-cli --include=optional --registry="${NPM_REGISTRY}" \
        "@openai/codex@${CODEX_VERSION}"; \
    npm install -g --prefix /opt/agent-cli --registry="${NPM_REGISTRY}" \
        "@openai/codex-${codex_arch}@npm:@openai/codex@${CODEX_VERSION}-${codex_arch}"; \
    npm install -g --prefix /opt/agent-cli --include=optional --registry="${NPM_REGISTRY}" \
        --ignore-scripts \
        "@earendil-works/pi-coding-agent@${PI_VERSION}"; \
    npm install --prefix /opt/opencode --no-save --no-package-lock --ignore-scripts \
        --registry="${NPM_REGISTRY}" "${opencode_pkg}@${OPENCODE_VERSION}"; \
    mkdir -p /opt/opencode/bin; \
    ln -sf "../node_modules/${opencode_pkg}/bin/opencode" /opt/opencode/bin/opencode; \
    npm install -g --prefix /opt/agent-cli --registry="${NPM_REGISTRY}" \
        "markdownlint-cli2@${MARKDOWNLINT_VERSION}" \
        "prettier@${PRETTIER_VERSION}"
# No `npm cache clean` any more: the cache is a build mount, so it never reached the image, and
# cleaning it would only throw away what makes the next build fast.

# Both mechanisms above fail quietly: a skipped install script is a warning, and npm counts a failed
# optionalDependency as success. Either one yields an image that builds clean, passes every layer,
# and then tells whoever types `claude` that its native binary is missing. This image exists to run
# these four CLIs, so it must not be allowed to build without them.
#
# The gate runs each CLI rather than checking for a directory: it is the same code path the user
# takes, and it also catches a binary that unpacked but will not execute. It is deliberately the
# layer right after the install, so the failure lands next to its cause.
RUN set -eu; \
    export PATH="/opt/agent-cli/bin:/opt/opencode/bin:${PATH}"; \
    for cli in claude codex pi opencode; do \
      if ! "$cli" --version; then \
        echo >&2; \
        echo "agent.Dockerfile: '$cli --version' failed, so the install above did not produce a" >&2; \
        echo "  working CLI. Two known causes, both silent:" >&2; \
        echo "    - its postinstall was skipped; add it to --allow-scripts on the npm install" >&2; \
        echo "    - its per-platform optionalDependency was dropped; keep one npm install -g" >&2; \
        echo "      per CLI, since npm discards them when several roots share one command" >&2; \
        echo "    - NPM_REGISTRY=${NPM_REGISTRY} does not serve it; try registry.npmjs.org" >&2; \
        exit 1; \
      fi; \
    done

FROM base AS pi-extensions

ARG PI_READER_VERSION=0.3.3
ARG PI_BACKGROUND_TASKS_VERSION=2.6.10
ARG NPM_REGISTRY=https://registry.npmjs.org

# Same rule as the CLI pins: mirror the host. The specifier in the host's settings.json is not the
# answer -- it says `latest` for pi-background-tasks -- so read the installed version out of the
# host's own package tree instead. The specs written below are exact, because the container must
# not drift onto a newer release than the host runs.

# These are personal Pi extensions that are part of this repository's agent experience, not project
# source. Bake the packages into Pi's own npm directory so a fresh container can load them without
# mutating the user's host Pi settings; credentials are still mounted separately and explicitly by
# hack/agent.sh --creds.
RUN --mount=type=cache,target=/root/.npm set -eu; \
    npm install --prefix /home/agent/.pi/agent/npm --registry="${NPM_REGISTRY}" \
        "@inobit/pi-reader@${PI_READER_VERSION}" \
        "pi-background-tasks@${PI_BACKGROUND_TASKS_VERSION}"; \
    printf '%s\n' \
        '{' \
        '  "packages": [' \
        "    \"npm:@inobit/pi-reader@${PI_READER_VERSION}\"," \
        "    \"npm:pi-background-tasks@${PI_BACKGROUND_TASKS_VERSION}\"" \
        '  ]' \
        '}' \
        > /home/agent/.pi/agent/settings.json

FROM base

ARG USERNAME=agent
ARG USER_UID=1000
ARG USER_GID=1000

COPY --from=agent-cli /opt/agent-cli /opt/agent-cli
COPY --from=agent-cli /opt/opencode /opt/opencode
COPY --from=editor-tools /opt/nvim /opt/nvim
COPY --from=editor-tools /usr/local/bin/starship /usr/local/bin/starship
COPY --from=editor-tools /usr/share/blesh /usr/share/blesh
RUN set -eu; \
    ln -sf /opt/nvim/bin/nvim /usr/local/bin/nvim; \
    for cli in claude codex pi markdownlint-cli2 prettier; do \
      ln -sf "/opt/agent-cli/bin/${cli}" "/usr/local/bin/${cli}"; \
    done; \
    ln -sf /opt/opencode/bin/opencode /usr/local/bin/opencode

# CLAUDE_CONFIG_DIR is set explicitly so every piece of Claude Code state -- credentials, session
# history, installed skills and its config file -- lands under one directory, which hack/agent.sh
# backs with a single named volume. Left to the default, the config file sits beside the directory
# at $HOME and is lost on every `docker run --rm`.
#
# LANG is unset in a container, and an unset LANG means the C locale: ble.sh warns about it, and
# anything that draws a box or a glyph -- starship's prompt, Neovim's UI -- degrades to ASCII.
# C.UTF-8 is built into glibc, so this needs no locales package.
#
# PATH puts /usr/local/libexec/agent-bin ahead of npm's /usr/local/bin so the wrappers copied in
# below shadow `claude`, `codex` and `opencode`. Set here, in the image, rather than in .bashrc: a
# session is also started non-interactively -- `hack/agent.sh shell -- claude -p ...` from a hook
# or CI reads no rc file -- and the two must not disagree about which binary runs.
#
# DISABLE_AUTOUPDATER for the same reason, and because the update cannot succeed by design: the
# CLIs are installed into the root-owned /opt/agent-cli and the session runs as ${USERNAME}, so
# Claude Code's updater only reaches "no write permission to npm prefix" and warns on every start.
# The version is pinned by CLAUDE_VERSION above; an in-place self-update would defeat that pin, so
# the right answer is to switch the updater off, not to make the prefix writable. Bump the ARG and
# rebuild to move versions.
#
# TERM because `docker run -t` falls back to plain `xterm` when the image leaves it unset, and
# `xterm` advertises 8 colours: Neovim's colourscheme, starship and ble.sh highlighting all degrade.
# Docker only fills in that fallback when TERM is absent from the image config, so setting it here
# wins. xterm-256color's terminfo entry ships in ncurses-base, already in the base image.
ENV CLAUDE_CONFIG_DIR=/home/agent/.claude \
    DISABLE_AUTOUPDATER=1 \
    LANG=C.UTF-8 \
    PATH=/usr/local/libexec/agent-bin:/opt/agent-cli/bin:${PATH} \
    TERM=xterm-256color

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
        /home/agent/.pi/agent/npm \
        /home/agent/.codex \
        /home/agent/.claude \
        /home/agent/.local/share/opencode \
        /home/agent/.local/state \
        /home/agent/.cache/opencode \
    && chown -R "${USERNAME}:${USERNAME}" /workspace /home/agent

COPY --from=pi-extensions --chown=${USER_UID}:${USER_GID} /home/agent/.pi/ /home/agent/.pi/

# Shell and editor configuration, tracked in hack/agent-home/ so the container's environment is
# reviewed in the same diff as the code. Copied, not mounted: the read-only targets mount only the
# workspace, and a shell that loses its prompt and its editor under --read-only is not usable.
COPY --chown=${USER_UID}:${USER_GID} hack/agent-home/ /home/agent/

# Baked in rather than run from the mount: the read-only targets cannot execute a script they
# mount :ro without also trusting the mount, and the entrypoint has to work before the
# workspace is known to be present at all.
COPY hack/agent-entrypoint.sh /usr/local/bin/agent-entrypoint
RUN chmod 0755 /usr/local/bin/agent-entrypoint

# The agent CLIs' own permission prompts are turned off by these wrappers, because the container
# is already the boundary -- see docs/tooling/agent-container.md. Baked in, not mounted: the read-only
# targets mount only the workspace, and an agent whose launcher disappears under --read-only is
# not usable. Root-owned and 0755, so the agent user cannot edit its own leash.
COPY hack/agent-bin/ /usr/local/libexec/agent-bin/
RUN chmod 0755 /usr/local/libexec/agent-bin/*

USER ${USERNAME}
WORKDIR /workspace/k8s-ai-platform

# Arms core.hooksPath on every entry, then hands over to CMD or to the docker run arguments.
ENTRYPOINT ["/usr/local/bin/agent-entrypoint"]
CMD ["/bin/bash"]
