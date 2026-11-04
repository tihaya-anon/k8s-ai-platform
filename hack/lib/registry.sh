# Shared image plumbing for hack/agent.sh and hack/devcontainer.sh.
#
# Both scripts keep a local image and a Docker Hub copy aligned for the current commit, and the
# rules for that -- what identifies an image, when the remote latest tag may move, how to ask the
# registry a question without pulling gigabytes -- are the same for both. Only the Dockerfile, the
# build arguments and the repository name differ, so those stay in the scripts and this file holds
# everything else.
#
# Sourced, never executed. The caller must set, before sourcing:
#
#   prog                the script's name, used as the prefix on every message
#
# and before calling anything below:
#
#   verbose             1 to echo the docker commands and let setup steps print live
#   image_repo          local repository, without a tag
#   remote_image_repo   repository that sync publishes to and pulls from
#   force_latest        1 to move the remote latest tag even when sync cannot prove HEAD is newer
#   pull_policy         missing|always, the base image refresh policy pull_base_image applies
#
# and must define one function:
#
#   build_image         build "$image_repo:$(current_git_sha)" and "$image_repo:latest"

# This file is sourced, so it has no shebang to tell shellcheck which shell to assume; the
# directive does it instead. bash rather than sh: the code below uses arrays, `local` and
# arithmetic conditions.
#
# SC2154 is off for the whole file rather than at each use. Every variable it flags is one of the
# caller-set names listed above -- that is the contract, not an oversight, and six inline pragmas
# would say the same thing six times while hiding the one place it matters. The cost is that a
# genuine typo in a variable name goes unreported here, so a new caller-set variable belongs in
# the list above where a reader can check it.
# shellcheck shell=bash
# shellcheck disable=SC2154

die() {
  printf '%s: %s\n' "$prog" "$1" >&2
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
    printf '%s: this step failed:' "$prog" >&2
    printf ' %q' "$@" >&2
    printf '\n\n' >&2
    cat "$log" >&2
    rm -f "$log"
    exit 1
  fi
  rm -f "$log"
}

# The base images are moving tags, so the build has to resolve them to a digest either way -- and
# resolving is what pins the build, not pulling. On a network where Docker Hub is slow or
# unreachable, pulling on every build re-downloads whatever upstream changed for a base whose
# layers are already local, and can fail outright before the build starts. Reuse the local image by
# default; the caller's pull policy environment variable restores the refresh.
pull_base_image() {
  case ${pull_policy:-missing} in
  missing)
    if docker image inspect "$1" >/dev/null 2>&1; then
      return 0
    fi
    run_step docker pull "$1"
    ;;
  always) run_step docker pull "$1" ;;
  *) die "the pull policy must be missing or always, got: ${pull_policy}" ;;
  esac
}

# The digest of a base image that pull_base_image has already resolved. Both Dockerfiles take their
# bases as build arguments so the build is pinned to one immutable image rather than to a tag that
# moves under it.
base_image_digest() {
  docker image inspect --format '{{index .RepoDigests 0}}' "$1"
}

# The default a Dockerfile gives one of its ARGs. The base images are pinned in the Dockerfiles and
# the scripts read them from there instead of repeating them: a second copy is how both scripts
# kept building on golang:1.25 after go.mod and every Dockerfile had moved to 1.27.
dockerfile_arg_default() {
  local value
  value=$(sed -nE "s/^ARG $2=(.+)$/\1/p" "$1" | head -1)
  [[ -n $value ]] || die "no default for ARG $2 in $1"
  printf '%s\n' "$value"
}

current_git_sha() {
  git rev-parse --short=12 HEAD 2>/dev/null || printf 'nogit'
}

# Remote probes go through buildx's imagetools: it reads the manifest and image config without
# downloading the layers, and uses the same credential store as docker push. `docker manifest
# inspect` cannot read a label, and pulling three gigabytes to answer "is this commit published?"
# would make sync cost more than an unconditional build.
#
# A missing tag and a failed query must not look alike: treating "not logged in" as "not published"
# would build an image that cannot be pushed either, and hide the one error worth reading.
remote_ref_exists() {
  local ref=$1 err
  err=$(mktemp)
  if docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$ref" >/dev/null 2>"$err"; then
    rm -f "$err"
    return 0
  fi
  if grep -qiE 'not found|manifest unknown|name unknown' "$err"; then
    rm -f "$err"
    return 1
  fi
  printf '%s: cannot query %s (is docker logged in to its registry?):\n' "$prog" "$ref" >&2
  cat "$err" >&2
  rm -f "$err"
  exit 1
}

remote_manifest_digest() {
  docker buildx imagetools inspect --format '{{.Manifest.Digest}}' "$1" 2>/dev/null || true
}

# The OCI revision label both Dockerfiles stamp on every build. Empty for an image built before
# the label existed, or for a multi-platform index (where .Image is keyed by platform); callers
# treat that as "cannot tell" rather than as "not this commit".
remote_image_revision() {
  docker buildx imagetools inspect \
    --format '{{index .Image.Config.Labels "org.opencontainers.image.revision"}}' "$1" 2>/dev/null || true
}

push_ref() {
  local local_ref=$1 remote_ref=$2
  run_step docker tag "$local_ref" "$remote_ref"
  run_step docker push "$remote_ref"
}

# How the remote latest relates to this checkout, decided by the revision label and git history:
#   advance -- this checkout is the same as, or newer than, what latest points at
#   same    -- latest already is this commit
#   behind  -- latest is a descendant of HEAD, so pushing would roll it back
#   diverged -- the histories do not share a descendant relationship
#   unknown -- no label and no digest match, so there is nothing to compare
latest_state() {
  local git_sha=$1 rev head_full latest_digest sha_digest
  if ! remote_ref_exists "$remote_image_repo:latest"; then
    printf 'advance\n'
    return 0
  fi

  rev=$(remote_image_revision "$remote_image_repo:latest")
  head_full=$(git rev-parse HEAD 2>/dev/null || true)

  if [[ -n $rev && -n $head_full && $head_full == "$rev"* ]]; then
    printf 'same\n'
    return 0
  fi
  if [[ -n $rev ]] && git cat-file -e "${rev}^{commit}" 2>/dev/null; then
    if git merge-base --is-ancestor "$rev" HEAD; then
      printf 'advance\n'
    elif git merge-base --is-ancestor HEAD "$rev"; then
      printf 'behind\n'
    else
      printf 'diverged\n'
    fi
    return 0
  fi

  # A latest pushed before the label existed can still identify itself through its digest: the
  # commit tag is pushed before this runs, so equal digests mean latest is this commit.
  latest_digest=$(remote_manifest_digest "$remote_image_repo:latest")
  sha_digest=$(remote_manifest_digest "$remote_image_repo:$git_sha")
  if [[ -n $latest_digest && $latest_digest == "$sha_digest" ]]; then
    printf 'same\n'
  else
    printf 'unknown\n'
  fi
}

# Align the local and published images for this checkout. The commit tag is the identity: if the
# registry already has it, a machine without the image pulls instead of paying for a rebuild; if
# neither side has it, this checkout is the only source and the build is followed by a push. The
# remote latest then follows the same rule, advancing only when git history says HEAD is at or
# ahead of it.
sync_image() {
  local git_sha local_ref local_latest remote_ref remote_latest state
  git_sha=$(current_git_sha)
  local_ref="$image_repo:$git_sha"
  local_latest="$image_repo:latest"
  remote_ref="$remote_image_repo:$git_sha"
  remote_latest="$remote_image_repo:latest"

  local has_local=0 has_remote=0
  if docker image inspect "$local_ref" >/dev/null 2>&1; then
    has_local=1
  fi
  if remote_ref_exists "$remote_ref"; then
    has_remote=1
  fi

  if ((has_remote)) && ! ((has_local)); then
    printf '%s: pulling %s, the image for this commit\n' "$prog" "$remote_ref" >&2
    run_step docker pull "$remote_ref"
    run_step docker tag "$remote_ref" "$local_ref"
    has_local=1
  fi

  if ! ((has_local)); then
    build_image
    has_local=1
  fi

  if ! ((has_remote)); then
    printf '%s: pushing %s\n' "$prog" "$remote_ref" >&2
    push_ref "$local_ref" "$remote_ref"
  fi

  state=$(latest_state "$git_sha")
  case $state in
  advance)
    printf '%s: moving %s to %s\n' "$prog" "$remote_latest" "$git_sha" >&2
    push_ref "$local_ref" "$remote_latest"
    ;;
  same) ;;
  behind)
    # latest is ahead of this checkout. Pulling it would leave the local image older than the
    # registry's, so the local pointer stays on this commit even though the registry's is ahead.
    printf '%s: %s is newer than this checkout; leaving it alone and keeping local latest on %s\n' \
      "$prog" "$remote_latest" "$git_sha" >&2
    ;;
  diverged | unknown)
    if ((force_latest)); then
      printf '%s: --force-latest: moving %s to %s\n' "$prog" "$remote_latest" "$git_sha" >&2
      push_ref "$local_ref" "$remote_latest"
    else
      printf '%s: not moving %s (%s); rerun with --force-latest to override\n' \
        "$prog" "$remote_latest" "$state" >&2
    fi
    ;;
  esac

  # The local latest is the image the callers run, so it always follows this checkout rather than
  # the registry. The commit tag is what sync keeps identical on both sides.
  run_step docker tag "$local_ref" "$local_latest"
  printf '%s: %s is now present locally and remotely\n' "$prog" "$remote_ref" >&2
}
