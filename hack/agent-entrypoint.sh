#!/usr/bin/env bash
# Wire the repo-local git config that cannot be baked into an image, then exec the command.
#
# `core.hooksPath` lives in .git/config, which is not tracked, so a fresh clone — or a
# container that mounts one — starts with the pre-commit hook disarmed. Setting it here
# means the hook is armed by the act of entering the container rather than by remembering
# a command. Everything is best-effort: the read-only targets (agent-deps, agent-build,
# agent-review) mount the workspace with :ro and cannot write .git/config, and failing to
# arm a hook must never stop the build.
set -uo pipefail

repo=/workspace/k8s-ai-platform

# hack/agent.sh mounts the host Pi credential read-only at this neutral path, not directly over
# ~/.pi/agent/auth.json. Pi is allowed to rewrite its own credential store during refresh, so copy
# the mounted secret into the container's writable layer before starting the requested command.
if [[ -r /run/agent-creds/pi-auth.json ]]; then
  mkdir -p /home/agent/.pi/agent
  cp /run/agent-creds/pi-auth.json /home/agent/.pi/agent/auth.json
  chmod 0600 /home/agent/.pi/agent/auth.json 2>/dev/null || true
fi

# The host's selected model and variant live in state, not in cli.json. Copy that read-only mount
# into the container's writable state directory before the server starts.
if [[ -r /run/agent-host/opencode-model.json ]]; then
  mkdir -p /home/agent/.local/state/opencode
  cp /run/agent-host/opencode-model.json /home/agent/.local/state/opencode/model.json
fi

# The container shares the host network, and OpenCode's service defaults to the host's port. Use the
# port hack/agent.sh allocated for this container instead. 49376 is the fallback for a container
# started without the script, so it still avoids the host's 49374.
opencode_port=${AGENT_OPENCODE_PORT:-49376}
if [[ $opencode_port =~ ^[1-9][0-9]*$ ]] && ((opencode_port <= 65535)); then
  mkdir -p /home/agent/.config/opencode
  printf '{\n  "port": %s\n}\n' "$opencode_port" >/home/agent/.config/opencode/service.json
fi

# hack/agent.sh exports the host's saved OpenCode accounts and mounts the JSON here. Import it into
# the container's own database; the read-only mount is not a place OpenCode can store accounts.
if [[ -r /run/agent-creds/opencode-auth.json ]]; then
  if ! opencode auth import /run/agent-creds/opencode-auth.json >/dev/null 2>&1; then
    printf 'agent-entrypoint: could not import OpenCode credentials\n' >&2
  fi
fi

if [[ -d $repo/.git ]]; then
  # The mount is owned by the host UID. It matches ${USER_UID} for the person who built the
  # image, but not for anyone else, and git then refuses the repo outright.
  if ! git config --global --get-all safe.directory 2>/dev/null | grep -qxF "$repo"; then
    git config --global --add safe.directory "$repo" 2>/dev/null || true
  fi

  # Write a repository config key only when it does not already carry the value we want.
  #
  # The test is on the outcome, not on write permission. .git/config is routinely not writable
  # by this container -- the :ro targets mount the workspace read-only, and a devcontainer or
  # any other root process that touched the checkout leaves .git/config owned by root while
  # this process is the agent user -- yet in that case the value is usually already correct,
  # because it was written from the host. Testing `-w` first reported a disarmed hook whenever
  # the file was merely unwritable, including when the hook was in fact armed.
  ensure_git_config() {
    local key=$1 want=$2
    [[ $(git -C "$repo" config --get "$key" 2>/dev/null) == "$want" ]] && return 0
    git -C "$repo" config "$key" "$want" 2>/dev/null
  }

  ensure_git_config user.name "${AGENT_GIT_USER_NAME:-AI Platform Agent}" || true
  ensure_git_config user.email "${AGENT_GIT_USER_EMAIL:-agent@k8s-ai-platform.local}" || true

  # Failing to arm the hook must never stop the command, so this only warns -- and only when the
  # hook really is not armed.
  if [[ -d $repo/hack/git-hooks ]] && ! ensure_git_config core.hooksPath hack/git-hooks; then
    printf 'agent-entrypoint: core.hooksPath is not hack/git-hooks and .git/config could not be\n' >&2
    printf '  written (read-only workspace, or a .git owned by another UID); the pre-commit hook\n' >&2
    printf '  is disarmed in this container.\n' >&2
  fi
fi

exec "$@"
