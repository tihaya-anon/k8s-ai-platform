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
  *.go)
    # controller-gen rewrites its own output on every `make generate`, so formatting
    # it just produces a diff the next generator run reverts. golangci-lint skips
    # these too (`exclusions: generated` in .golangci.yml); staying in step with it
    # is the point. The marker is the Go convention, not a path pattern, so a
    # renamed generated file is still skipped.
    if grep -qE '^// Code generated .* DO NOT EDIT\.$' "$f"; then continue; fi
    go+=("$f")
    ;;
  esac
done

status=0

# markdownlint runs first: its --fix touches structure (list markers, fence
# spacing), then Prettier has the final say on wrapping and whitespace.
if [[ ${#md[@]} -gt 0 ]] && have markdownlint-cli2; then
  markdownlint-cli2 --fix "${md[@]}" >/dev/null 2>&1
fi

if [[ ${#prettier[@]} -gt 0 ]] && have prettier; then
  # .prettierrc loads prettier-plugin-sort-json, which Prettier resolves from the repository's
  # node_modules -- not from whichever global install provided the `prettier` on PATH. Without the
  # tree, Prettier fails on every file with a plugin-resolution error that says nothing about the
  # real fix, so name it here instead.
  if [[ ! -d $repo_root/node_modules/prettier-plugin-sort-json ]]; then
    echo "format.sh: the Prettier plugins are not installed; run 'npm ci'" >&2
    status=1
  else
    prettier --write --ignore-unknown --log-level warn "${prettier[@]}" || status=1
  fi
fi

# goimports is a superset of gofmt -- it applies the same formatting and also adds
# and regroups imports -- so it replaces gofmt rather than running alongside it.
# Grouping must stay in step with the goimports settings in .golangci.yml, or
# `make lint` will flag what this script just wrote.
if [[ ${#go[@]} -gt 0 ]]; then
  # LOCALBIN is what the Makefile installs goimports into; the container sets it to its own volume.
  localbin=${LOCALBIN:-$repo_root/bin}
  if [[ -x $localbin/goimports ]]; then
    "$localbin/goimports" -w "${go[@]}" || status=1
  elif have goimports; then
    goimports -w "${go[@]}" || status=1
  elif have gofmt; then
    # Loud, because gofmt leaves imports untouched: the file looks formatted while
    # still failing `make lint`. A silent skip here is what makes that confusing.
    echo "format.sh: goimports not found, falling back to gofmt; imports not regrouped (run 'make goimports')" >&2
    gofmt -w "${go[@]}" || status=1
  else
    echo "format.sh: no Go formatter found; install goimports with 'make goimports'" >&2
    status=1
  fi
fi

exit $status
