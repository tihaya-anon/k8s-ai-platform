#!/usr/bin/env bash
# Development container entry point: build the image, and run this repository's work inside it.
#
# This replaced an agent.Makefile. Make earns its keep for the product build, where targets have
# real file dependencies, but every target here was .PHONY and composed one `docker run` out of
# string variables -- so the flags could not be shared by a function, arguments were not
# expressible, and every `$` had to be written `$$`. A script says the same thing once.
#
# Usage: hack/agent.sh [-v] <command> [--mount SPEC ...] [-- COMMAND ...]
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root" || exit 1

image=${AGENT_IMAGE:-k8s-ai-platform-agent}
name=${AGENT_NAME:-k8s-ai-agent}
workspace=/workspace/k8s-ai-platform

# The container inherits whatever mirrors the host already trusts, rather than hardcoding any.
go_proxy=${GO_PROXY:-$(go env GOPROXY 2>/dev/null || printf direct)}
npm_registry=${NPM_REGISTRY:-$(npm config get registry 2>/dev/null || printf 'https://registry.npmjs.org')}

# Named volumes for everything expensive to rebuild and pointless to keep in the image.
caches=(
  -v k8s-ai-go-pkg:/home/agent/go/pkg
  -v k8s-ai-go-build:/home/agent/.cache/go-build
)

# Claude Code keeps credentials, history and skills here, so `claude /login` inside the container
# survives --rm and the host credential never has to be mounted in.
claude_volume=(-v k8s-ai-claude:/home/agent/.claude)

# Shell configuration, read from the repository rather than the copy baked into the image, so
# editing it on the host takes effect in the next shell. Read-only: nothing in the container
# writes its own configuration. ~/.bash_env is absent on purpose -- it holds real tokens on the
# host, and the .bashrc guard skips it here.
home_mounts=(
  -v "$repo_root/hack/agent-home/.bashrc:/home/agent/.bashrc:ro"
  -v "$repo_root/hack/agent-home/.bash_aliases:/home/agent/.bash_aliases:ro"
  -v "$repo_root/hack/agent-home/.blerc:/home/agent/.blerc:ro"
  -v "$repo_root/hack/agent-home/.config/starship.toml:/home/agent/.config/starship.toml:ro"
)

sandbox=(--cap-drop ALL --security-opt no-new-privileges)

# Setup output -- the image build and the volume chown -- is captured rather than printed, because
# it is noise on every run but the first and the first is the one nobody is watching anyway. -v
# prints it live, along with the exact docker commands. A failure prints what it captured either
# way: hiding the output of a step that went wrong would be the one unforgivable version of this.
verbose=0

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

# Filled by --mount, for the credential files a session is explicitly given. Kept as a flag rather
# than an environment variable so the boundary is visible in the command that opened the shell.
extra_mounts=()
# Filled by everything after --, to run one command instead of an interactive shell.
command_override=()

die() {
  printf 'agent.sh: %s\n' "$1" >&2
  exit 1
}

# Echo under -v, so what happened to the boundary can be put in the scrollback. %q rather than $*,
# so what is printed is what could be pasted back into a shell -- the mount specifications and the
# `sh -c` bodies are meaningless once their quoting is flattened.
run() {
  if ((verbose)); then
    printf '+' >&2
    printf ' %q' "$@" >&2
    printf '\n' >&2
  fi
  "$@"
}

# For setup steps, whose output is worth seeing only when it fails or when asked for.
run_step() {
  if ((verbose)); then
    run "$@"
    return
  fi

  local log
  log=$(mktemp)
  if ! "$@" >"$log" 2>&1; then
    printf 'agent.sh: this step failed:' >&2
    printf ' %q' "$@" >&2
    printf '\n\n' >&2
    cat "$log" >&2
    rm -f "$log"
    exit 1
  fi
  rm -f "$log"
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
    --mount)
      [[ $# -ge 2 ]] || die "--mount needs a specification"
      extra_mounts+=(--mount "$2")
      shift 2
      ;;
    --mount=*)
      extra_mounts+=(--mount "${1#--mount=}")
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

build_image() {
  # A cold build fetches most of a gigabyte, so say something before going quiet for minutes.
  ((verbose)) || printf 'agent.sh: building %s (-v to follow)\n' "$image" >&2
  run_step docker build \
    -f agent.Dockerfile \
    --build-arg USER_UID="$(id -u)" \
    --build-arg USER_GID="$(id -g)" \
    --build-arg NPM_REGISTRY="$npm_registry" \
    -t "$image" .
}

# The named volumes are created root-owned by the daemon; the agent user has to be able to write
# them. Idempotent, and the only place this script needs root.
prepare_volumes() {
  build_image
  run_step docker run --rm --user root \
    "${caches[@]}" \
    "${claude_volume[@]}" \
    "$image" \
    sh -c 'mkdir -p /home/agent/go/pkg/mod /home/agent/.cache/go-build /home/agent/.claude \
      && chown -R agent:agent /home/agent/go/pkg /home/agent/.cache /home/agent/.claude'
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

  local mount_flags=(-v "$repo_root:$workspace")
  [[ $access == ro ]] && mount_flags=(-v "$repo_root:$workspace:ro")

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

  local cmd=("$@")
  [[ ${#command_override[@]} -gt 0 ]] && cmd=("${command_override[@]}")

  run docker run \
    "${mode_flags[@]}" \
    "${sandbox[@]}" \
    -e GOPROXY="$proxy_chain" \
    "${hostmod_flags[@]}" \
    "${mount_flags[@]}" \
    "${caches[@]}" \
    "${claude_volume[@]}" \
    "${home_mounts[@]}" \
    "${extra_mounts[@]}" \
    -w "$workspace" \
    "$image" \
    "${cmd[@]}"
}

usage() {
  cat <<'EOF'
Usage: hack/agent.sh [-v] <command> [--mount SPEC ...] [-- COMMAND ...]

Commands:
  image     build the development container image
  prepare   build the image and make the named volumes writable
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
  --no-host-mods Do not mount the host module cache as a local GOPROXY. The container then
                 downloads every dependency over the network into its own volume, which is
                 slower by minutes but touches no host path outside the checkout.
  --mount SPEC   passed to docker as --mount, repeatable. For the credential files a
                 session is explicitly given, e.g.
                   --mount type=bind,src=$HOME/.codex/auth.json,dst=/home/agent/.codex/auth.json,readonly
                 Claude Code needs none of this: log in once inside the container and the
                 k8s-ai-claude volume keeps it.
  -- COMMAND     run COMMAND in the container instead of the command's default, e.g.
                   hack/agent.sh shell -- go test ./internal/...

Environment:
  AGENT_IMAGE    image tag (default: k8s-ai-platform-agent)
  AGENT_NAME     container name for the interactive shells (default: k8s-ai-agent)
  GO_PROXY       module proxy (default: the host's `go env GOPROXY`)
  NPM_REGISTRY   npm registry for the image build (default: the host's `npm config get registry`)
EOF
}

main() {
  # -v is accepted before the command as well as after it, because it is the flag most likely to
  # be added to a command someone has already typed.
  if [[ ${1:-} == -v || ${1:-} == --verbose ]]; then
    verbose=1
    shift
  fi

  local command=${1:-help}
  [[ $# -gt 0 ]] && shift
  parse_options "$@"

  case $command in
  image) build_image ;;
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
