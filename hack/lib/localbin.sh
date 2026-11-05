#!/usr/bin/env bash
# Resolve a tool the Makefile installs into LOCALBIN, building it if it is missing.
#
# Sourced by the one-line wrappers the editor points at. The editor needs a single path that is
# correct in every checkout, and `go.alternateTools` (and the Go Annotations equivalent) take a
# literal path in which VS Code expands only `${workspaceFolder}` -- so neither can reach LOCALBIN,
# which is the repo's bin/ on a host and /home/agent/.cache/k8s-ai-platform/bin inside the
# devcontainer, because the two run different checkout paths and the Makefile links tools by
# absolute path. A hardcoded path is therefore right in exactly one of the two and silently wrong
# in the other: the extension finds nothing, reports nothing, and the editor looks clean while
# `make lint` is not.
#
# Reading LOCALBIN the way the Makefile reads it keeps one owner for that decision. Nothing here
# knows which environment it is in.

# Usage: localbin_exec <tool> [args...]
localbin_exec() {
  local tool="$1"
  shift

  local repo_root bin
  repo_root="$(cd "$(dirname "${BASH_SOURCE[1]}")/.." && pwd)"
  bin="${LOCALBIN:-$repo_root/bin}/$tool"

  # Build rather than fail. A fresh clone's first save would otherwise report nothing, which is the
  # failure these wrappers exist to prevent. The Makefile target installs without running the tool.
  if [[ ! -x "$bin" ]]; then
    echo "$tool not found at $bin; building it with 'make $tool'" >&2
    make -C "$repo_root" "$tool" >&2
  fi

  exec "$bin" "$@"
}
