#!/usr/bin/env bash
# Development container entry point: build the image, and run this repository's work inside it.
#
# This replaced an agent.Makefile. Make earns its keep for the product build, where targets have
# real file dependencies, but every target here was .PHONY and composed one `docker run` out of
# string variables -- so the flags could not be shared by a function, arguments were not
# expressible, and every `$` had to be written `$$`. A script says the same thing once.
#
# Usage: hack/agent.sh [-v] <command> [--creds [LIST]] [--mount SPEC ...] [-- COMMAND ...]
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root" || exit 1

# The image identity, registry probes and sync rules are shared with hack/devcontainer.sh; only
# the Dockerfile and its build arguments are this script's own. $prog is the prefix the library
# puts on its messages, so it has to be set before the source.
prog=agent.sh
# shellcheck source=lib/registry.sh
source "$repo_root/hack/lib/registry.sh"

image_repo=${AGENT_IMAGE_REPOSITORY:-k8s-ai-platform-agent}
image=${AGENT_IMAGE:-$image_repo:latest}
# Where `hack/agent.sh sync` publishes this repository's image. The local repository stays
# unqualified, so every other command keeps using the short name; only sync knows the registry.
# The publish tag is the commit sha the local build already uses, which is what makes "is this
# commit published?" a tag lookup rather than an image comparison.
remote_image_repo=${AGENT_IMAGE_REMOTE_REPOSITORY:-chihayaanon0908/k8s-ai-platform-agent}
name=${AGENT_NAME:-k8s-ai-agent}
workspace=/workspace/k8s-ai-platform

# OpenCode's managed service defaults to port 49374, the same port the host service uses, and
# hack/agent.sh runs the container with host networking. Give every container its own port from a
# range instead, so a second agent container does not collide with the first. The entrypoint writes
# the chosen port into ~/.config/opencode/service.json.
opencode_port_range=${AGENT_OPENCODE_PORT_RANGE:-49376-49400}

# The container inherits whatever mirrors the host already trusts, rather than hardcoding any.
go_proxy=${GO_PROXY:-$(go env GOPROXY 2>/dev/null || printf direct)}
npm_registry=${NPM_REGISTRY:-$(npm config get registry 2>/dev/null || printf 'https://registry.npmjs.org')}

# Give throwaway containers a Git identity for commits. Prefer the host's configured identity so
# commits remain attributable; fall back to a clearly synthetic project-local identity.
#
# Precedence is git's own, in git's own order: this repository's .git/config first, then the user's
# global config, then the fallback. Reading --global first would mean a per-repository identity --
# the usual way to keep a personal address off work commits -- was silently ignored in the
# container while being honored on the host.
git_user_name=${AGENT_GIT_USER_NAME:-$(git config --local user.name 2>/dev/null || git config --global user.name 2>/dev/null || printf 'AI Platform Agent')}
git_user_email=${AGENT_GIT_USER_EMAIL:-$(git config --local user.email 2>/dev/null || git config --global user.email 2>/dev/null || printf 'agent@k8s-ai-platform.local')}

# Named volumes for everything expensive to rebuild and pointless to keep in the image.
caches=(
  -v k8s-ai-go-pkg:/home/agent/go/pkg
  -v k8s-ai-go-build:/home/agent/.cache/go-build
)

# LazyVim installs its plugin tree into ~/.local/share/nvim, and the first start in a fresh container
# would otherwise pull roughly 500MB of plugins again on every `docker run --rm`. A named volume
# makes that a one-time cost; `docker volume rm k8s-ai-nvim` forces a clean reinstall.
nvim_volume=(-v k8s-ai-nvim:/home/agent/.local/share/nvim)

# The generated Makefile installs controller-gen, setup-envtest, kustomize and goimports into
# LOCALBIN (default $(pwd)/bin) and links each one with an absolute path from realpath. The checkout
# has a different absolute path on the host and inside the container, and the bind mount makes both
# see the same bin/, so whichever side ran make last leaves the other a dangling link. Rather than
# edit generated files, the container gets its own LOCALBIN on a named volume: same Makefile, links
# that are valid where they were written, and no host-built tool binary is executed across the
# boundary. LOCALBIN is a `?=` assignment, so the environment wins. Emptying the volume re-downloads
# the tools.
tools_volume=(-v k8s-ai-tools:/home/agent/.cache/k8s-ai-platform)
container_localbin=/home/agent/.cache/k8s-ai-platform/bin

# Claude Code keeps credentials, history and skills here, so `claude /login` inside the container
# survives --rm and the host credential never has to be mounted in.
claude_volume=(-v k8s-ai-claude:/home/agent/.claude)

# OpenCode keeps sessions, connected accounts and its database under the XDG data directory. A
# named volume gives it the same treatment as Claude Code: connect once inside the container and
# the credential stays in the volume, with no host file mounted.
opencode_volume=(-v k8s-ai-opencode:/home/agent/.local/share/opencode)

# Shell configuration, read from the repository rather than the copy baked into the image, so
# editing it on the host takes effect in the next shell. Read-only: nothing in the container
# writes its own configuration. ~/.zsh_env is absent on purpose -- it holds real tokens on the
# host, and the .zprofile guard skips it here.
home_mounts=(
  -v "$repo_root/hack/agent-home/.zshenv:/home/agent/.zshenv:ro"
  -v "$repo_root/hack/agent-home/.zprofile:/home/agent/.zprofile:ro"
  -v "$repo_root/hack/agent-home/.zshrc:/home/agent/.zshrc:ro"
  -v "$repo_root/hack/agent-home/.zsh_aliases:/home/agent/.zsh_aliases:ro"
  -v "$repo_root/hack/agent-home/.config/starship.toml:/home/agent/.config/starship.toml:ro"
  -v "$repo_root/hack/agent-home/.config/opencode/cli.json:/home/agent/.config/opencode/cli.json:ro"
)

# OpenCode's selected model and variant are state, not cli.json. Mount the host's model.json
# read-only and let the entrypoint copy it into the container's writable state directory. A host
# global opencode.json(c), when present, is mounted read-only as well, so configuration-based
# defaults and the project config agree.
opencode_config_dir=${XDG_CONFIG_HOME:-$HOME/.config}/opencode
opencode_state_dir=${XDG_STATE_HOME:-$HOME/.local/state}/opencode
opencode_config_mounts=()
opencode_state_mounts=()
for config_name in opencode.json opencode.jsonc; do
  if [[ -f $opencode_config_dir/$config_name ]]; then
    opencode_config_mounts+=(-v "$opencode_config_dir/$config_name:/home/agent/.config/opencode/$config_name:ro")
  fi
done
if [[ -f $opencode_state_dir/model.json ]]; then
  opencode_state_mounts=(--mount "type=bind,src=$opencode_state_dir/model.json,dst=/run/agent-host/opencode-model.json,readonly")
fi

# The credential files the agent CLIs read, as name:host:container. Opt-in through --creds and
# never mounted otherwise, because "no host secrets unless you asked for one" is the property the
# container is for -- see docs/tooling/agent-container.md.
#
# Claude Code is deliberately absent: one `claude /login` inside the container puts its credential
# in the k8s-ai-claude volume, so there is nothing on the host to mount and nothing to revoke
# beyond the volume. CODEX_HOME is honoured because codex documents it; pi has no equivalent.
#
# OpenCode is absent too, but for a different reason: its saved accounts live in SQLite, not in a
# file. --creds opencode runs `opencode auth export` on the host into a temporary JSON file and
# mounts that; the entrypoint imports it into the container's own database.
credentials=(
  # Pi rewrites auth.json when OAuth-backed providers refresh or when provider attribution metadata
  # changes. Mount the host secret somewhere inert and let the entrypoint copy it into the
  # container's writable Pi home; otherwise Pi fails with EROFS on a read-only bind mount.
  "pi:${HOME}/.pi/agent/auth.json:/run/agent-creds/pi-auth.json"
  "codex:${CODEX_HOME:-$HOME/.codex}/auth.json:/home/agent/.codex/auth.json"
)

sandbox=(--cap-drop ALL --security-opt no-new-privileges)

# Setup output -- the image build and the volume chown -- is captured rather than printed, because
# it is noise on every run but the first and the first is the one nobody is watching anyway. -v
# prints it live, along with the exact docker commands. A failure prints what it captured either
# way: hiding the output of a step that went wrong would be the one unforgivable version of this.
verbose=0

# Base image refresh policy, read by pull_base_image in hack/lib/registry.sh. Named generically
# there because hack/devcontainer.sh has its own environment variable for the same switch.
pull_policy=${AGENT_PULL:-missing}

# sync refuses to move the remote latest tag backwards. --force-latest covers the case the script
# cannot decide: a latest built before images carried the revision label, or a commit from another
# clone that is not in this one.
force_latest=0

# The container's module cache is a named volume, separate from the host's by design. That costs a
# full re-download of the Kubernetes dependency tree the first time -- fourteen minutes of network
# for three seconds of CPU, measured. $GOMODCACHE/cache/download is already laid out as a module
# proxy, so it is mounted read-only and put at the front of GOPROXY instead: the same work resolves
# from local disk in about three seconds, Go still writes only into the container's own volume, and
# anything missing falls through to the network proxy behind it.
#
# This is the one place the container reads a host path outside the checkout. It is read-only, it
# is public module source, and --no-host-mods turns it off.
use_host_mods=1

# Host-backed session directories are mounted by default. They are conversation/history state, not
# provider credentials; auth files still require --creds and are mounted read-only. Creating the
# directories on the host avoids Docker creating them as root when the first session starts.
session_mounts=()

# Filled by --mount, for extra files a session is explicitly given. Kept as a flag rather than an
# environment variable so the boundary is visible in the command that opened the shell.
extra_mounts=()
# Filled by --creds, from the registry above. Same reasoning, one flag shorter to type.
cred_mounts=()
# Temporary host file holding `opencode auth export` output, removed when the script exits.
opencode_cred_file=
creds=0
creds_select=
# Filled by everything after --, to run one command instead of an interactive shell.
command_override=()

# True when something already listens on the port in the host network namespace. The container
# shares that namespace, so ss is seeing exactly the addresses OpenCode would try to bind.
port_in_use() {
  local port=$1
  if command -v ss >/dev/null 2>&1; then
    ss -Hltn 2>/dev/null | awk -v p="$port" '$4 ~ ":" p "$" { found = 1 } END { exit found ? 0 : 1 }'
  else
    (exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null
  fi
}

# First free port in START-END, checked against the host's listeners. The gap between choosing and
# OpenCode binding is unavoidable without holding the port, but in practice a session starts its
# service before another session is opened.
choose_opencode_port() {
  local range=$1 start end port
  if [[ ! $range =~ ^([0-9]+)-([0-9]+)$ ]]; then
    die "AGENT_OPENCODE_PORT_RANGE must be START-END, got: $range"
  fi
  start=$((10#${BASH_REMATCH[1]}))
  end=$((10#${BASH_REMATCH[2]}))
  if ((start < 1 || end > 65535 || start > end)); then
    die "AGENT_OPENCODE_PORT_RANGE is outside 1-65535 or reversed: $range"
  fi

  for ((port = start; port <= end; port++)); do
    if ! port_in_use "$port"; then
      printf '%s\n' "$port"
      return 0
    fi
  done
  die "no free port in AGENT_OPENCODE_PORT_RANGE=$range"
}

parse_options() {
  while [[ $# -gt 0 ]]; do
    case $1 in
    --no-host-mods)
      use_host_mods=0
      shift
      ;;
    -v | --verbose)
      verbose=1
      shift
      ;;
    --force-latest)
      force_latest=1
      shift
      ;;
    --mount)
      [[ $# -ge 2 ]] || die "--mount needs a specification"
      extra_mounts+=(--mount "$2")
      shift 2
      ;;
    --mount=*)
      extra_mounts+=(--mount "${1#--mount=}")
      shift
      ;;
    --creds)
      creds=1
      # The value is optional. Every non-option word here would otherwise be rejected as an
      # unexpected argument, so a bare word following --creds can only be its selection.
      if [[ -n ${2:-} && ${2:-} != -* ]]; then
        creds_select=$2
        shift 2
      else
        shift
      fi
      ;;
    --creds=*)
      creds=1
      creds_select=${1#--creds=}
      shift
      ;;
    --)
      shift
      command_override=("$@")
      return
      ;;
    -*) die "unknown option: $1 (see hack/agent.sh help)" ;;
    *) die "unexpected argument: $1 (pass a command after --)" ;;
    esac
  done
}

# Turn --creds into mount flags. Read-only without exception: the container has no business
# rewriting a token the host owns, and a refresh written inside it would die with --rm anyway.
#
# The two halves are deliberately asymmetric. A named credential that is absent is an error,
# because `--creds codex` silently opening a shell without it is exactly the failure worth an
# error message. A bare --creds takes whatever is there and says what it took.
resolve_sessions() {
  local pi_sessions codex_sessions
  pi_sessions=${PI_SESSIONS_DIR:-$HOME/.pi/agent/sessions}
  codex_sessions=${CODEX_SESSIONS_DIR:-${CODEX_HOME:-$HOME/.codex}/sessions}

  mkdir -p "$pi_sessions" "$codex_sessions"
  session_mounts=(
    --mount "type=bind,src=$pi_sessions,dst=/home/agent/.pi/agent/sessions"
    --mount "type=bind,src=$codex_sessions,dst=/home/agent/.codex/sessions"
  )
}

# Write the host's saved OpenCode accounts to a temporary JSON file for the container to import.
# `auth export` does not include environment-only providers, by design: those are not saved accounts
# and have nothing on disk to transfer.
export_opencode_credentials() {
  local json
  command -v opencode >/dev/null 2>&1 || return 1

  json=$(opencode auth export --standalone 2>/dev/null) || return 1
  [[ -n $json && $json != "[]" ]] || return 1

  opencode_cred_file=$(mktemp "${TMPDIR:-/tmp}/opencode-auth.XXXXXX") || return 1
  chmod 0600 "$opencode_cred_file"
  printf '%s\n' "$json" >"$opencode_cred_file"
  cred_mounts+=(--mount "type=bind,src=$opencode_cred_file,dst=/run/agent-creds/opencode-auth.json,readonly")
  printf 'agent.sh: mounting opencode credential, read-only: host auth export\n' >&2
}

resolve_credentials() {
  ((creds)) || return 0

  local wanted=() entry name host container known_names=(pi codex opencode)
  [[ -n $creds_select ]] && IFS=, read -ra wanted <<<"$creds_select"

  for name in ${wanted[@]+"${wanted[@]}"}; do
    local known=0
    for entry in "${known_names[@]}"; do
      [[ $entry == "$name" ]] && known=1
    done
    ((known)) || die "unknown credential: $name (known: ${known_names[*]})"
  done

  for entry in "${credentials[@]}"; do
    name=${entry%%:*}
    host=${entry#*:}
    host=${host%:*}
    container=${entry##*:}

    if ((${#wanted[@]})) && ! printf '%s\n' "${wanted[@]}" | grep -qxF "$name"; then
      continue
    fi

    if [[ ! -f $host ]]; then
      ((${#wanted[@]})) && die "no $name credential at $host"
      continue
    fi

    cred_mounts+=(--mount "type=bind,src=$host,dst=$container,readonly")
    # Printed unconditionally, not under -v. Handing a host secret to a container is the one
    # thing this script does that the scrollback should record without being asked.
    printf 'agent.sh: mounting %s credential, read-only: %s\n' "$name" "$host" >&2
  done

  local want_opencode=0
  if ((${#wanted[@]})); then
    for name in "${wanted[@]}"; do
      [[ $name == opencode ]] && want_opencode=1
    done
  else
    want_opencode=1
  fi

  if ((want_opencode)); then
    if ! export_opencode_credentials; then
      ((${#wanted[@]})) && die "no saved OpenCode credentials on this host (connect one with /connect or opencode auth login)"
    fi
  fi
}

build_image() {
  local git_sha commit_image
  git_sha=$(current_git_sha)
  commit_image="$image_repo:$git_sha"

  local node_image go_image node_digest go_digest
  node_image=${AGENT_NODE_IMAGE:-node:24-bookworm}
  go_image=${AGENT_GO_IMAGE:-golang:1.25-bookworm}

  # Resolve moving tags to immutable digests before the build. BuildKit otherwise checks Docker Hub
  # for node:24-bookworm/golang:1.25-bookworm on every cold builder and may pull a different base
  # even when an older agent image exists locally. The digest comes from the local image when one
  # is there, so the build stays pinned without the pull.
  pull_base_image "$node_image"
  pull_base_image "$go_image"
  node_digest=$(base_image_digest "$node_image")
  go_digest=$(base_image_digest "$go_image")

  local build_flags=(--network "${AGENT_BUILD_NETWORK:-host}")
  local build_args=(
    --build-arg NODE_IMAGE="$node_digest"
    --build-arg GO_IMAGE="$go_digest"
    --build-arg USER_UID="$(id -u)"
    --build-arg USER_GID="$(id -g)"
    --build-arg NPM_REGISTRY="$npm_registry"
    --build-arg GOPROXY="$go_proxy"
    --build-arg GIT_SHA="$git_sha"
  )
  # Package and release mirrors keep their defaults in the Dockerfile; passing them only when the
  # host sets them leaves one source of truth while letting a slow network override them the way it
  # already overrides NPM_REGISTRY and GOPROXY.
  local mirror_var
  for mirror_var in APT_MIRROR GITHUB_MIRROR NVIM_RELEASE_BASE; do
    if [[ -n ${!mirror_var:-} ]]; then
      build_args+=(--build-arg "$mirror_var=${!mirror_var}")
    fi
  done
  local proxy_var
  for proxy_var in HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY http_proxy https_proxy all_proxy no_proxy; do
    if [[ -n ${!proxy_var:-} ]]; then
      build_args+=(--build-arg "$proxy_var=${!proxy_var}")
    fi
  done

  # A cold build fetches most of a gigabyte, so say something before going quiet for minutes.
  ((verbose)) || printf 'agent.sh: building %s and %s (-v to follow)\n' "$commit_image" "$image" >&2
  run_step docker build \
    "${build_flags[@]}" \
    -f agent.Dockerfile \
    "${build_args[@]}" \
    -t "$commit_image" \
    -t "$image" .
}

ensure_image() {
  if docker image inspect "$image" >/dev/null 2>&1; then
    return 0
  fi

  die "no agent image found: $image (run ./hack/agent.sh image first)"
}

# The named volumes are created root-owned by the daemon; the agent user has to be able to write
# them. Idempotent, and the only place this script needs root.
prepare_volumes() {
  ensure_image
  run_step docker run --rm --user root \
    "${caches[@]}" \
    "${nvim_volume[@]}" \
    "${tools_volume[@]}" \
    "${claude_volume[@]}" \
    "${opencode_volume[@]}" \
    "$image" \
    sh -c 'mkdir -p /home/agent/go/pkg/mod /home/agent/.cache/go-build /home/agent/.cache/k8s-ai-platform/bin /home/agent/.claude /home/agent/.local/share/opencode /home/agent/.local/share/nvim \
      && chown -R agent:agent /home/agent/go/pkg /home/agent/.cache /home/agent/.claude /home/agent/.local/share/opencode /home/agent/.local/share/nvim'
}

# One `docker run`, parameterised by how the workspace is mounted and what runs inside. Everything
# above it exists so this is the only place the flags are written down.
#
#   $1  rw|ro   how to mount the checkout
#   $2  tty|batch
#   rest        the command, when not overridden by --
agent_run() {
  local access=$1 mode=$2
  shift 2

  local opencode_port
  opencode_port=$(choose_opencode_port "$opencode_port_range")

  local mount_flags=(-v "$repo_root:$workspace")
  [[ $access == ro ]] && mount_flags=(-v "$repo_root:$workspace:ro")

  # Neovim's configuration is part of the checkout, so it follows the session's access mode instead
  # of the read-only home mounts: LazyVim rewrites lazy-lock.json when it installs plugins into a
  # fresh volume, and a review session must not write to the tree. The plugin tree itself lives on
  # the k8s-ai-nvim volume, so only the config directory is bound.
  local nvim_flags=(-v "$repo_root/hack/agent-home/.config/nvim:/home/agent/.config/nvim")
  [[ $access == ro ]] && nvim_flags=(-v "$repo_root/hack/agent-home/.config/nvim:/home/agent/.config/nvim:ro")

  local mode_flags=(--rm)
  if [[ $mode == tty ]]; then
    mode_flags+=(--name "$name" --pids-limit 512)
    # -t only when there is a terminal to attach to. `shell -- some-command` from a script, a hook
    # or CI has no tty, and docker refuses outright rather than degrading.
    [[ -t 0 ]] && mode_flags+=(-it)
  fi

  # Assembled here rather than at the top, so --no-host-mods is honoured whichever side of the
  # command it was typed.
  local proxy_chain=$go_proxy
  local hostmod_flags=()
  if ((use_host_mods)); then
    local hostmods
    hostmods=$(go env GOMODCACHE 2>/dev/null || true)
    if [[ -n $hostmods && -d $hostmods/cache/download ]]; then
      hostmod_flags=(
        -v "$hostmods/cache/download:/hostproxy:ro"
        -e GOSUMDB="$(go env GOSUMDB 2>/dev/null || printf sum.golang.org)"
      )
      proxy_chain="file:///hostproxy,$go_proxy"
    fi
  fi

  local proxy_flags=() proxy_var
  for proxy_var in HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY http_proxy https_proxy all_proxy no_proxy; do
    if [[ -n ${!proxy_var:-} ]]; then
      proxy_flags+=(-e "$proxy_var=${!proxy_var}")
    fi
  done

  local cmd=("$@")
  [[ ${#command_override[@]} -gt 0 ]] && cmd=("${command_override[@]}")

  run docker run \
    "${mode_flags[@]}" \
    --network "${AGENT_RUN_NETWORK:-host}" \
    "${sandbox[@]}" \
    -e GOPROXY="$proxy_chain" \
    -e LOCALBIN="$container_localbin" \
    -e AGENT_OPENCODE_PORT="$opencode_port" \
    -e AGENT_GIT_USER_NAME="$git_user_name" \
    -e AGENT_GIT_USER_EMAIL="$git_user_email" \
    "${proxy_flags[@]}" \
    "${hostmod_flags[@]}" \
    "${mount_flags[@]}" \
    "${nvim_flags[@]}" \
    "${caches[@]}" \
    "${nvim_volume[@]}" \
    "${tools_volume[@]}" \
    "${claude_volume[@]}" \
    "${opencode_volume[@]}" \
    "${session_mounts[@]}" \
    "${home_mounts[@]}" \
    ${opencode_config_mounts[@]+"${opencode_config_mounts[@]}"} \
    ${opencode_state_mounts[@]+"${opencode_state_mounts[@]}"} \
    ${cred_mounts[@]+"${cred_mounts[@]}"} \
    "${extra_mounts[@]}" \
    -w "$workspace" \
    "$image" \
    "${cmd[@]}"
}

usage() {
  cat <<'EOF'
Usage: hack/agent.sh [-v] <command> [--creds [LIST]] [--mount SPEC ...] [-- COMMAND ...]

Commands:
  image     build the development container image locally, tagged with the commit sha and latest
  sync      align the local and published images for this commit: pull, build, or push as needed
  prepare   make the named volumes writable using the latest image
  deps      download this repository's Go dependencies into the cache volume
  build     verify the source builds and tests, from a read-only checkout
  fmt       format the tree with the container's pinned formatters
  lint      run the markdown gate with the container's pinned markdownlint
  shell     open a development shell; the checkout is writable
  review    open a review shell; the checkout is read-only
  help      this text

Options:
  -v, --verbose  print the docker commands, and the output of the image build and volume
                 setup that is otherwise captured. A step that fails prints its output
                 regardless.
  --force-latest move the remote latest tag to this commit even when sync cannot prove this
                 checkout is newer. The guard normally treats an older checkout's sync as a
                 rollback and leaves latest alone.
  --no-host-mods Do not mount the host module cache as a local GOPROXY. The container then
                 downloads every dependency over the network into its own volume, which is
                 slower by minutes but touches no host path outside the checkout.
  --creds [LIST] mount the agent CLI credentials this host has, read-only. Nothing is
                 mounted without it. LIST is a comma-separated selection of
                 pi,codex,opencode; omit it to take whichever of them exist. A name given
                 explicitly must exist, a missing one under a bare --creds is skipped, and
                 either way what was handed over is printed. Claude Code needs none of
                 this: log in once inside the container and the k8s-ai-claude volume keeps
                 it. OpenCode's saved accounts are exported from its database rather than
                 mounted as a file.
  --mount SPEC   passed to docker as --mount, repeatable. The escape hatch for anything
                 --creds does not know about, e.g.
                   --mount type=bind,src=$HOME/.config/x/token,dst=/home/agent/.config/x/token,readonly
  -- COMMAND     run COMMAND in the container instead of the command's default, e.g.
                   hack/agent.sh shell -- go test ./internal/...

Environment:
  CODEX_HOME     codex configuration directory --creds reads (default: ~/.codex)
  PI_SESSIONS_DIR
                 host directory mounted at /home/agent/.pi/agent/sessions by default
                 (default: ~/.pi/agent/sessions)
  CODEX_SESSIONS_DIR
                 host directory mounted at /home/agent/.codex/sessions by default
                 (default: $CODEX_HOME/sessions or ~/.codex/sessions)
  AGENT_IMAGE    image tag to use and update during image builds (default: k8s-ai-platform-agent:latest)
  AGENT_IMAGE_REPOSITORY
                 local image repository for commit-tagged builds (default: k8s-ai-platform-agent)
  AGENT_IMAGE_REMOTE_REPOSITORY
                 repository that `sync` publishes to and pulls from, including the Docker Hub
                 account (default: chihayaanon0908/k8s-ai-platform-agent)
  AGENT_NAME     container name for the interactive shells (default: k8s-ai-agent)
  AGENT_BUILD_NETWORK
                 docker build network mode (default: host, so localhost proxies work)
  AGENT_PULL     base image refresh policy: missing (default) resolves node/golang from a local
                 image and pulls only when it is absent; always pulls the moving tags every build
  APT_MIRROR     Debian package mirror for the image build (default: the Dockerfile's, which is
                 mirrors.aliyun.com)
  GITHUB_MIRROR  prefix for GitHub release downloads in the image build (default: the
                 Dockerfile's, https://github.com)
  NVIM_RELEASE_BASE
                 directory holding the Neovim release tarballs, for a mirror that keeps them under
                 its own layout (default: the upstream GitHub release for NVIM_VERSION)
  AGENT_RUN_NETWORK
                 docker run network mode (default: host, so localhost proxies work)
  GO_PROXY       module proxy (default: the host's `go env GOPROXY`)
  NPM_REGISTRY   npm registry for the image build (default: the host's `npm config get registry`)
  AGENT_GIT_USER_NAME
  AGENT_GIT_USER_EMAIL
                 git commit identity used inside the container (default: host git config,
                 then AI Platform Agent <agent@k8s-ai-platform.local>)
  AGENT_OPENCODE_PORT_RANGE
                 host port range assigned to this container's OpenCode service (default:
                 49376-49400). The container uses host networking, so each concurrent
                 agent container needs its own free port.
EOF
}

main() {
  # An if, not `[[ ]] && rm`: under set -e a false test as the trap's last status becomes the
  # script's exit status, and every command that mounted no OpenCode credential exited 1.
  cleanup() {
    if [[ -n ${opencode_cred_file:-} ]]; then
      rm -f -- "$opencode_cred_file"
    fi
  }
  trap cleanup EXIT HUP INT TERM

  # -v is accepted before the command as well as after it, because it is the flag most likely to
  # be added to a command someone has already typed.
  if [[ ${1:-} == -v || ${1:-} == --verbose ]]; then
    verbose=1
    shift
  fi

  local command=${1:-help}
  [[ $# -gt 0 ]] && shift
  parse_options "$@"
  resolve_sessions
  # Before prepare_volumes, so `--creds codx` fails in a second instead of after an image build.
  resolve_credentials

  case $command in
  image) build_image ;;
  sync) sync_image ;;
  prepare) prepare_volumes ;;
  deps)
    prepare_volumes
    agent_run ro batch go mod download
    ;;
  build)
    prepare_volumes
    agent_run ro batch bash -c 'go build ./... && go test ./...'
    ;;
  fmt)
    prepare_volumes
    agent_run rw batch ./hack/format.sh
    ;;
  lint)
    prepare_volumes
    # deps/ is gitignored upstream checkouts read as reference; linting them reported 7536 issues
    # in other people's documentation and buried this repository's own.
    agent_run ro batch markdownlint-cli2 '**/*.md' '!deps/**'
    ;;
  shell)
    prepare_volumes
    agent_run rw tty
    ;;
  review)
    prepare_volumes
    agent_run ro tty
    ;;
  help | -h | --help) usage ;;
  *)
    printf 'agent.sh: unknown command: %s\n\n' "$command" >&2
    usage >&2
    exit 1
    ;;
  esac
}

main "$@"
