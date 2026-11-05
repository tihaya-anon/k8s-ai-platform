#!/usr/bin/env bash
# The Makefile's pinned controller-gen, for the editor. See hack/lib/localbin.sh.
#
# The Go Annotations extension reads its Kubebuilder marker definitions from controller-gen, so a
# wrong path here means marker completion and hover silently fall back to whatever the extension
# knows, which is not the version this project generates with.
set -euo pipefail

# shellcheck source=lib/localbin.sh
source "$(dirname "${BASH_SOURCE[0]}")/lib/localbin.sh"

localbin_exec controller-gen "$@"
