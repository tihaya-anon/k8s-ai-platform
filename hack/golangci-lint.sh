#!/usr/bin/env bash
# The Makefile's pinned, plugin-built golangci-lint, for the editor. See hack/lib/localbin.sh.
set -euo pipefail

# shellcheck source=lib/localbin.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/localbin.sh"

localbin_exec golangci-lint "$@"
