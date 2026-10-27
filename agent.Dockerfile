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
        zsh \
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
# Neither zsh plugin cuts releases often enough to track the host's clones, so pin the commits the
# host installer checked out. Debian's packages would be older than the host and drift silently.
ARG ZSH_AUTOSUGGESTIONS_COMMIT=85919cd1ffa7d2d5412f6d3fe437ebdbeeec4fc5
ARG ZSH_SYNTAX_HIGHLIGHTING_COMMIT=e6683f20e5c38d3e5447f45845d04ddda75d0f23

# Every download below comes from GitHub releases. Point this at a mirror when the build network
# cannot reach github.com directly, e.g. --build-arg GITHUB_MIRROR=https://ghproxy.net/https://github.com
ARG GITHUB_MIRROR=https://github.com

# Neovim, starship and the zsh plugins, from upstream tarballs rather than apt: Debian stable's
# Neovim is ancient, starship is not packaged at all, and the plugin packages lag the host.
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
    for plugin in "zsh-autosuggestions:${ZSH_AUTOSUGGESTIONS_COMMIT}" \
                  "zsh-syntax-highlighting:${ZSH_SYNTAX_HIGHLIGHTING_COMMIT}"; do \
      name="${plugin%%:*}"; commit="${plugin#*:}"; \
      curl --http1.1 --retry 5 --retry-delay 2 --retry-all-errors -fsSL -o "/tmp/${name}.tar.gz" \
        "${GITHUB_MIRROR}/zsh-users/${name}/archive/${commit}.tar.gz"; \
      mkdir -p "/opt/zsh-plugins/${name}"; \
      tar -C "/opt/zsh-plugins/${name}" --strip-components=1 -xzf "/tmp/${name}.tar.gz"; \
      test -r "/opt/zsh-plugins/${name}/${name}.zsh"; \
    done; \
    rm -rf /tmp/nvim.tar.gz /tmp/starship.tar.gz /tmp/zsh-*.tar.gz; \
    nvim --version | head -1; \
    starship --version | head -1

FROM base AS agent-cli

# Declared in this stage, and deliberately. A Pi/Codex/Claude/OpenCode pin change should rebuild
# only the CLI stage, not the editor-tool stage with the Neovim/starship/zsh-plugin downloads.
#
# These track what the host actually runs, so a container session and a host session differ in
# their sandbox and in nothing else. Read the host's own answer (`pi --version`) rather than a
# remembered one. The image installs each CLI once and never self-updates it, so the way to move
# a version is to change the pin and rebuild -- see DISABLE_AUTOUPDATER below.
#
# One ARG and one RUN per CLI, in increasing order of how often the pin moves. BuildKit keys a
# layer on its expanded command string, so a single RUN naming every version meant that bumping
# prettier -- 10MB -- reinstalled all 1.3GB. Split, a bump only invalidates its own layer and the
# layers after it, which is why Claude Code is last: it is the pin that moves most often. Moving an
# ARG above a layer that does not use it costs nothing, so the ARGs sit with their installs.
#
# Separate commands are also load-bearing for a second reason. npm drops the per-platform
# optionalDependencies of codex and claude-code when several roots are installed in one global
# command: it creates node_modules/@anthropic-ai/ and leaves it empty, the postinstall then finds
# no binary to copy, and both CLIs ship as stubs that fail on first use. Installed one at a time,
# the same versions and the same registry resolve correctly. Verified in this image, not assumed.

# The second package mirror, for the same reason as APT_MIRROR: the agent CLIs are by far the
# largest downloads in the image and the default registry is the slow path on some networks.
ARG NPM_REGISTRY=https://registry.npmjs.org

# The cache mount on every layer below is not an optimisation, it is what makes a retry survivable.
# codex and claude-code ship their real binaries as per-platform optionalDependencies -- around
# 570MB unpacked between them -- and without a cache every interrupted build starts the download
# again. Cache mounts live outside the image, so the layers' size is unchanged.

# The formatters, first because their pins move least. Two roots in one command is safe here: both
# are plain packages with no per-platform binary to lose.
ARG MARKDOWNLINT_VERSION=0.23.2
ARG PRETTIER_VERSION=3.9.6
RUN --mount=type=cache,target=/root/.npm set -eu; \
    npm install -g --prefix /opt/agent-cli --registry="${NPM_REGISTRY}" \
        "markdownlint-cli2@${MARKDOWNLINT_VERSION}" \
        "prettier@${PRETTIER_VERSION}"

# OpenCode 2 is @opencode/cli, not opencode-ai: the latter still points at OpenCode 1 and has no
# 2.x releases. Its wrapper package pulls every matching platform build (x64 baseline, musl, ...)
# as optionalDependencies -- about 900MB to end up running one 230MB binary. The official V2
# installer instead downloads only the platform package, and so does this layer. The command names
# the architecture-specific package explicitly; the wrapper's postinstall is never needed.
ARG OPENCODE_VERSION=2.1.4
ARG TARGETARCH
RUN --mount=type=cache,target=/root/.npm set -eu; \
    case "${TARGETARCH:-amd64}" in \
      amd64) opencode_pkg=@opencode/cli-linux-x64 ;; \
      arm64) opencode_pkg=@opencode/cli-linux-arm64 ;; \
      *) echo "unsupported TARGETARCH: ${TARGETARCH}" >&2; exit 1 ;; \
    esac; \
    if [ "$opencode_pkg" = "@opencode/cli-linux-x64" ] && ! grep -qwi avx2 /proc/cpuinfo 2>/dev/null; then \
      opencode_pkg=@opencode/cli-linux-x64-baseline; \
    fi; \
    npm install --prefix /opt/opencode --no-save --no-package-lock --ignore-scripts \
        --registry="${NPM_REGISTRY}" "${opencode_pkg}@${OPENCODE_VERSION}"; \
    mkdir -p /opt/opencode/bin; \
    ln -sf "../node_modules/${opencode_pkg}/bin/opencode" /opt/opencode/bin/opencode

# Pi gets --ignore-scripts, which is what upstream's own install instructions use. Naming a package
# in --allow-scripts is an exception to a denial; pi needs no exception at all, so its scripts are
# refused rather than merely not granted. The version is pinned by PI_VERSION and enforced by the
# CLI gate below, so nothing is lost by not letting its dependencies build themselves.
ARG PI_VERSION=1.4.0
RUN --mount=type=cache,target=/root/.npm set -eu; \
    npm install -g --prefix /opt/agent-cli --include=optional --registry="${NPM_REGISTRY}" \
        --ignore-scripts \
        "@earendil-works/pi-coding-agent@${PI_VERSION}"

# --include=optional is npm's default, and is written out anyway: it is what pulls the per-platform
# binary, and a default this load-bearing should be visible in the file rather than inherited.
#
# This layer used to install codex's binary a second time, as an alias
# (@openai/codex-linux-x64 -> npm:@openai/codex@<version>-linux-x64), on the belief that npm
# installs no aliased optionalDependency under -g. That was wrong, and expensively so: npm does
# install it, nested under the codex root rather than hoisted, and node resolves the nested copy.
# `require.resolve` from bin/codex.js in the built image returns
# @openai/codex/node_modules/@openai/codex-linux-x64, so the hoisted alias was 324MB -- a quarter
# of what the CLIs occupy -- that nothing ever loaded. If a future npm really does drop it, the gate below
# fails on `codex --version` rather than shipping a stub, which is the whole reason the gate runs
# the binary instead of checking for a path.
ARG CODEX_VERSION=0.154.0
RUN --mount=type=cache,target=/root/.npm set -eu; \
    npm install -g --prefix /opt/agent-cli --include=optional --registry="${NPM_REGISTRY}" \
        "@openai/codex@${CODEX_VERSION}"

# Last, because this pin moves most often and everything above should survive a bump.
#
# --allow-scripts is not optional here. npm 11.19 -- the version in node:24-bookworm -- stopped
# running dependency install scripts unless the package is named, and because strict-allow-scripts
# defaults to false it *warns and continues* rather than failing. Claude Code copies its native
# binary into place from its own postinstall, so without this the build succeeds and the image
# ships a stub. The list is exactly one package: install scripts are arbitrary code from the
# registry, so it stays that short.
ARG CLAUDE_VERSION=2.1.278
RUN --mount=type=cache,target=/root/.npm set -eu; \
    npm install -g --prefix /opt/agent-cli --include=optional --registry="${NPM_REGISTRY}" \
        --allow-scripts=@anthropic-ai/claude-code \
        "@anthropic-ai/claude-code@${CLAUDE_VERSION}"

# No `npm cache clean` any more: the cache is a build mount, so it never reached the image, and
# cleaning it would only throw away what makes the next build fast.

# Both mechanisms above fail quietly: a skipped install script is a warning, and npm counts a failed
# optionalDependency as success. Either one yields an image that builds clean, passes every layer,
# and then tells whoever types `claude` that its native binary is missing. This image exists to run
# these four CLIs, so it must not be allowed to build without them.
#
# The gate runs each CLI rather than checking for a directory: it is the same code path the user
# takes, and it also catches a binary that unpacked but will not execute. It is the layer directly
# after the last install, so a failure still lands next to the layer that could have caused it;
# a per-install gate would catch it one layer earlier, at the cost of four more layers.
RUN set -eu; \
    export PATH="/opt/agent-cli/bin:/opt/opencode/bin:${PATH}"; \
    for cli in claude codex pi opencode; do \
      if ! "$cli" --version; then \
        echo >&2; \
        echo "agent.Dockerfile: '$cli --version' failed, so the install above did not produce a" >&2; \
        echo "  working CLI. Three known causes, all silent:" >&2; \
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

FROM base AS go-tools

# goimports, so a fresh container can format Go before any make target has run.
#
# hack/format.sh prefers $LOCALBIN/goimports and falls back to whatever is on PATH; this copy is
# that fallback. It is not a replacement for `make goimports`, which installs into the named
# LOCALBIN volume and wins when present -- it is what keeps the pre-commit hook from degrading to
# plain gofmt on the first run in a new container, which is how imports stopped being regrouped.
#
# Pinned to GOIMPORTS_VERSION in the Makefile, which matches the golang.org/x/tools in go.mod so
# format.sh and golangci-lint group imports identically. The two pins must move together; a
# container that formats differently from CI is worse than one with no formatter at all.
ARG GOIMPORTS_VERSION=v0.26.0

# GOPROXY is an ARG for the same reason APT_MIRROR and NPM_REGISTRY are: this image is built on
# networks where the default is the slow path, e.g. --build-arg GOPROXY=https://goproxy.cn,direct
ARG GOPROXY=https://proxy.golang.org,direct

# The module and build caches are mount caches, so nothing from the install reaches the layer
# except the binary itself. GOPATH is redirected away from /home/agent for the same reason: this
# stage runs as root, and the final image's GOPATH belongs to the agent user.
RUN --mount=type=cache,target=/root/.cache/go-build \
    --mount=type=cache,target=/go/pkg/mod \
    set -eu; \
    GOPATH=/go GOCACHE=/root/.cache/go-build GOBIN=/opt/go-tools/bin GOPROXY="${GOPROXY}" \
      go install "golang.org/x/tools/cmd/goimports@${GOIMPORTS_VERSION}"; \
    printf 'package p\n' | /opt/go-tools/bin/goimports >/dev/null

FROM base

ARG USERNAME=agent
ARG USER_UID=1000
ARG USER_GID=1000

COPY --from=agent-cli /opt/agent-cli /opt/agent-cli
COPY --from=agent-cli /opt/opencode /opt/opencode
COPY --from=editor-tools /opt/nvim /opt/nvim
COPY --from=editor-tools /usr/local/bin/starship /usr/local/bin/starship
COPY --from=go-tools /opt/go-tools/bin/goimports /usr/local/bin/goimports
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
# LANG is unset in a container, and an unset LANG means the C locale: zle miscounts multibyte
# input, and anything that draws a box or a glyph -- starship's prompt, Neovim's UI -- degrades to ASCII.
# C.UTF-8 is built into glibc, so this needs no locales package.
#
# PATH puts /usr/local/libexec/agent-bin ahead of npm's /usr/local/bin so the wrappers copied in
# below shadow `claude`, `codex` and `opencode`. Set here, in the image, rather than in .zshrc: a
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
# `xterm` advertises 8 colours: Neovim's colourscheme, starship and zsh syntax highlighting all degrade.
# Docker only fills in that fallback when TERM is absent from the image config, so setting it here
# wins. xterm-256color's terminfo entry ships in ncurses-base, already in the base image.
#
# SHELL because Docker sets none, and the agents' shell tools and Neovim's :terminal read it to
# pick a shell; unset, they fall back to sh and lose the aliases the session has.
ENV CLAUDE_CONFIG_DIR=/home/agent/.claude \
    DISABLE_AUTOUPDATER=1 \
    LANG=C.UTF-8 \
    PATH=/usr/local/libexec/agent-bin:/opt/agent-cli/bin:${PATH} \
    SHELL=/bin/zsh \
    TERM=xterm-256color

RUN groupadd --gid "${USER_GID}" "${USERNAME}" \
    && useradd \
        --uid "${USER_UID}" \
        --gid "${USER_GID}" \
        --create-home \
        --shell /bin/zsh \
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
# The path ~/.zshrc reads on the host too, so the shared file needs no container branch.
COPY --from=editor-tools --chown=${USER_UID}:${USER_GID} /opt/zsh-plugins/ /home/agent/.local/share/zsh/plugins/

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
# A login shell, so ~/.zprofile sets EDITOR and PATH exactly as a host terminal does.
CMD ["/bin/zsh", "-l"]
