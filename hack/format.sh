#!/usr/bin/env bash
# Format files in place, dispatching by extension.
#
# One script so the Claude Code PostToolUse hook, the git pre-commit hook, and a
# human running it by hand all apply exactly the same rules; a formatter that
# disagrees with itself depending on who invoked it is worse than none.
#
# Usage: hack/format.sh [file ...]   # no arguments = every git-tracked file
set -uo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$repo_root" || exit 1

have() { command -v "$1" >/dev/null 2>&1; }

files=("$@")
if [[ ${#files[@]} -eq 0 ]]; then
  mapfile -t files < <(git ls-files)
fi

md=() prettier=() go=()
for f in "${files[@]}"; do
  [[ -f $f ]] || continue
  case $f in
  *.md) md+=("$f") prettier+=("$f") ;;
  *.json | *.jsonc | *.yaml | *.yml) prettier+=("$f") ;;
  *.go) go+=("$f") ;;
  esac
done

status=0

# markdownlint runs first: its --fix touches structure (list markers, fence
# spacing), then Prettier has the final say on wrapping and whitespace.
if [[ ${#md[@]} -gt 0 ]] && have markdownlint-cli2; then
  markdownlint-cli2 --fix "${md[@]}" >/dev/null 2>&1
fi

if [[ ${#prettier[@]} -gt 0 ]] && have prettier; then
  prettier --write --ignore-unknown --log-level warn "${prettier[@]}" || status=1
fi

if [[ ${#go[@]} -gt 0 ]]; then
  if have gofmt; then
    gofmt -w "${go[@]}" || status=1
  elif have go; then
    go fmt "${go[@]}" >/dev/null || status=1
  fi
fi

exit $status
