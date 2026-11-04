#!/usr/bin/env bash
# VS Code devcontainer image: build it, and keep it aligned with the published copy.
#
# The counterpart to hack/agent.sh for the other image this repository builds. agent.Dockerfile is
# the opt-in AI agent sandbox; .devcontainer/Dockerfile is the editor image VS Code opens, and
# until this script existed it was built by copying a `docker build` line out of a comment in
# devcontainer.json -- which is how its tag drifted away from the one devcontainer.json actually
# pulls, so a rebuild silently changed nothing.
#
# One difference from agent.sh is worth knowing before reading the rest. agent.sh keeps an
# unqualified local repository and only `sync` knows the registry; here the local and remote
# repository are the same name, because devcontainer.json's "image" field is the single name both
# VS Code and this script have to agree on, and on a fresh clone that name has to be pullable.
#
# Usage: hack/devcontainer.sh [-v] <command>
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root" || exit 1

prog=devcontainer.sh
# shellcheck source=lib/registry.sh
source "$repo_root/hack/lib/registry.sh"

# Must match the "image" field in .devcontainer/devcontainer.json; check_json_image enforces that
# rather than trusting it.
image_repo=${DEVCONTAINER_IMAGE_REPOSITORY:-chihayaanon0908/k8s-ai-devcontainer}
remote_image_repo=${DEVCONTAINER_IMAGE_REMOTE_REPOSITORY:-$image_repo}
devcontainer_json=.devcontainer/devcontainer.json
dockerfile=.devcontainer/Dockerfile

verbose=0
force_latest=0
pull_policy=${DEVCONTAINER_PULL:-missing}

# The whole point of this script: a build tagged with anything other than what devcontainer.json
# names produces an image VS Code will never open, and Rebuild Container then quietly reuses the
# old one. Comparing is cheap; debugging "I rebuilt and nothing changed" is not.
#
# The field is read with a grep rather than jq because devcontainer.json is JSONC -- it is mostly
# comments -- and jq rejects it.
check_json_image() {
  local declared
  declared=$(grep -oE '^[[:space:]]*"image"[[:space:]]*:[[:space:]]*"[^"]+"' "$devcontainer_json" |
    head -1 | sed -E 's/.*"image"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')
  if [[ -z $declared ]]; then
    die "no \"image\" field found in $devcontainer_json"
  fi
  if [[ $declared != "$image_repo:latest" ]]; then
    die "$devcontainer_json opens $declared, but this script builds $image_repo:latest -- align them, or set DEVCONTAINER_IMAGE_REPOSITORY"
  fi
}

build_image() {
  check_json_image

  local git_sha commit_image latest_image
  git_sha=$(current_git_sha)
  commit_image="$image_repo:$git_sha"
  latest_image="$image_repo:latest"

  local go_image go_digest
  go_image=${DEVCONTAINER_GO_IMAGE:-$(dockerfile_arg_default "$dockerfile" GO_IMAGE)}
  pull_base_image "$go_image"
  go_digest=$(base_image_digest "$go_image")

  # Host network by default, because a proxy bound to the host's loopback is unreachable from the
  # bridge network a build normally gets -- which would make the proxy variables below mean one
  # thing outside the build and nothing inside it. On a host that reaches github.com and
  # proxy.golang.org directly this buys nothing, so DEVCONTAINER_BUILD_NETWORK=bridge is a
  # supported choice rather than a broken one.
  local build_flags=(--network "${DEVCONTAINER_BUILD_NETWORK:-host}")
  local build_args=(
    --build-arg GO_IMAGE="$go_digest"
    --build-arg GIT_SHA="$git_sha"
  )
  # Mirrors keep their defaults in the Dockerfile and are passed only when the environment sets
  # them, so there is one source of truth and a slow network can still override it. Same rule as
  # agent.sh, including for the module proxy: the host's `go env` is never asked, because the image
  # is the toolchain and the host's Go may be older than go.mod (see agent.sh).
  if [[ -n ${GO_PROXY:-} ]]; then
    build_args+=(--build-arg "GOPROXY=$GO_PROXY")
  fi
  local mirror_var
  for mirror_var in APT_MIRROR DOCKER_MIRROR GITHUB_MIRROR; do
    if [[ -n ${!mirror_var:-} ]]; then
      build_args+=(--build-arg "$mirror_var=${!mirror_var}")
    fi
  done
  # Taken from the environment rather than written into the Dockerfile or a comment, so the repo
  # stays machine-independent: a host with no proxy passes nothing and the build goes direct.
  local proxy_var
  for proxy_var in HTTP_PROXY HTTPS_PROXY ALL_PROXY NO_PROXY http_proxy https_proxy all_proxy no_proxy; do
    if [[ -n ${!proxy_var:-} ]]; then
      build_args+=(--build-arg "$proxy_var=${!proxy_var}")
    fi
  done

  ((verbose)) || printf '%s: building %s and %s (-v to follow)\n' "$prog" "$commit_image" "$latest_image" >&2
  run_step docker build \
    "${build_flags[@]}" \
    -f "$dockerfile" \
    "${build_args[@]}" \
    -t "$commit_image" \
    -t "$latest_image" .

  printf '%s: built. Run "Dev Containers: Rebuild Container" in VS Code to pick it up.\n' "$prog" >&2
}

usage() {
  cat <<'EOF'
Usage: hack/devcontainer.sh [-v] <command>

Commands:
  image     build the devcontainer image locally, tagged with the commit sha and latest
  sync      align the local and published images for this commit: pull, build, or push as needed
  help      this text

A build only puts the image on disk. VS Code picks it up on the next
"Dev Containers: Rebuild Container"; reopening an existing container does not, and neither does
it rerun the lifecycle commands or install the extensions from customizations.vscode.

Options:
  -v, --verbose  print the docker commands, and the build output that is otherwise captured.
                 A step that fails prints its output regardless.
  --force-latest move the remote latest tag to this commit even when sync cannot prove this
                 checkout is newer. The guard normally treats an older checkout's sync as a
                 rollback and leaves latest alone.

Environment:
  DEVCONTAINER_IMAGE_REPOSITORY
                 image repository to build and publish, which must match the "image" field in
                 .devcontainer/devcontainer.json (default:
                 chihayaanon0908/k8s-ai-devcontainer)
  DEVCONTAINER_IMAGE_REMOTE_REPOSITORY
                 repository that `sync` publishes to, when it differs from the local one
                 (default: the same repository)
  DEVCONTAINER_GO_IMAGE
                 Go base image (default: the GO_IMAGE ARG default in .devcontainer/Dockerfile)
  DEVCONTAINER_PULL
                 base image refresh policy: missing (default) resolves the base from a local image
                 and pulls only when it is absent; always pulls the moving tag every build
  DEVCONTAINER_BUILD_NETWORK
                 docker build network mode (default: host, so localhost proxies work)
  APT_MIRROR     Debian package mirror for the image build (default: the Dockerfile's, which is
                 mirrors.aliyun.com)
  GITHUB_MIRROR  prefix for GitHub release downloads in the image build (default: the
                 Dockerfile's, https://github.com)
  GO_PROXY       module proxy for the build (default: the Dockerfile's,
                 https://proxy.golang.org,direct)
  http_proxy, https_proxy, all_proxy, no_proxy (and the uppercase spellings)
                 passed through to the build when set on the host
EOF
}

main() {
  # -v is accepted before the command as well as after it, because it is the flag most likely to
  # be added to a command someone has already typed.
  while [[ ${1:-} == -* ]]; do
    case $1 in
    -v | --verbose) verbose=1 ;;
    --force-latest) force_latest=1 ;;
    -h | --help)
      usage
      return 0
      ;;
    *) die "unknown option: $1" ;;
    esac
    shift
  done

  local command=${1:-help}
  [[ $# -gt 0 ]] && shift
  while [[ $# -gt 0 ]]; do
    case $1 in
    -v | --verbose) verbose=1 ;;
    --force-latest) force_latest=1 ;;
    *) die "unknown argument: $1" ;;
    esac
    shift
  done

  case $command in
  image) build_image ;;
  sync)
    check_json_image
    sync_image
    ;;
  help | -h | --help) usage ;;
  *)
    printf '%s: unknown command: %s\n\n' "$prog" "$command" >&2
    usage >&2
    exit 1
    ;;
  esac
}

main "$@"
