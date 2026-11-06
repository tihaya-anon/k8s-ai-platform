#!/usr/bin/env bash
set -euo pipefail

# The tools this script used to download on every rebuild -- kind, kubebuilder, kubectl, starship,
# the zsh plugins, gopls and dlv -- are now baked into the image by .devcontainer/Dockerfile.
# What is left is only what has to run against the live container: fixing volume ownership,
# linking the baked zsh plugins into $HOME, and the bin/ tools that live on the named volume
# (so they persist across rebuilds and `make` skips them once present).

# Every external command this script runs, checked before any of them does work. A missing binary
# otherwise reaches the Dev Containers log as a bare "exit code 127" with no name attached: `set
# -e` stops the script at the `command not found`, and the CLI reports the status rather than the
# message -- which makes a one-word problem look like a lifecycle problem, and makes VS Code skip
# the rest of onCreate including the extension install. Checking up front also fails in a second
# instead of after the two minutes `npm ci` and `make` spend first.
missing=()
for cmd in sudo chsh zsh git curl make go node npm docker kind kubebuilder kubectl k9s starship; do
  command -v "$cmd" >/dev/null 2>&1 || missing+=("$cmd")
done
if [ ${#missing[@]} -gt 0 ]; then
  echo "post-install.sh: not on PATH: ${missing[*]}" >&2
  echo "  sudo and chsh come from the common-utils feature and docker from" >&2
  echo "  docker-outside-of-docker; everything else is baked by .devcontainer/Dockerfile." >&2
  echo "  Scroll up to that feature's or that layer's output for why it is absent." >&2
  exit 1
fi

# Docker creates a named volume's mount point, and every missing parent of a mount, owned by root
# -- and this script no longer runs as root, so those directories have to be handed to the user
# before anything writes under them. Same job as prepare_volumes() in hack/agent.sh, and the only
# place this script needs root for something that is not an install.
# /go/pkg is the Go module cache volume (GOPATH is /go in the golang image). It is listed here
# rather than left to Go, because Go creates what it needs *under* the mount point but cannot
# create the root-owned mount point's contents in the first place.
#
# The tools volume is mounted over the workspace's bin/, so that directory is a root-owned mount
# point too even though the bind mount around it belongs to the user. -R, because a volume that
# already carries tools from an earlier container has root-owned contents under it as well.
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
for dir in "$HOME/.cache" "$HOME/.config" "$HOME/.kube" /go/pkg; do
  if [ -d "$dir" ]; then sudo chown "$(id -u):$(id -g)" "$dir"; fi
done
sudo chown -R "$(id -u):$(id -g)" "$repo_root/bin"

# The plugins are baked under /opt/zsh-plugins because the agent user does not exist at image
# build time; link them to the path hack/agent-home/.zshrc reads. `${zsh_plugins:?}` guards the
# rm -rf against an empty variable, for the same reason agent.Dockerfile spells out at its apt
# rewrite.
zsh_plugins="$HOME/.local/share/zsh/plugins"
mkdir -p "$zsh_plugins"
for name in zsh-autosuggestions zsh-syntax-highlighting; do
  rm -rf "${zsh_plugins:?}/${name}"
  ln -sfn "/opt/zsh-plugins/${name}" "${zsh_plugins}/${name}"
done

# chsh on one's own account prompts for a password under PAM even with an empty one, so it goes
# through sudo like the chown above.
sudo chsh -s /bin/zsh "$(id -un)"

# The formatter toolchain, from package.json. `npm ci` rather than `npm install`: it installs the
# lockfile's exact tree and fails if package.json has drifted from it, which is what keeps this
# container, hack/agent.sh's container and the host on the same Prettier and the same
# prettier-plugin-sort-json. node_modules/ is gitignored, so this is also what makes a fresh clone
# able to format at all.
#
# It lands in the workspace bind mount, which both containers and the host share. Both packages are
# pure JavaScript, so one tree is valid everywhere; a dependency with a native binding would not be.
npm ci --no-audit --no-fund

# Into the workspace's bin/ (a volume, see devcontainer.json) at the Makefile's pins; the editor's
# linter, hack/format.sh and Go Annotations' marker definitions all read them from there, and both
# editor settings name that directory literally, so the tools have to exist before the editor
# starts rather than be built on first use.
make goimports golangci-lint controller-gen

go version
node --version
npm --version
kind version
kubebuilder version
kubectl version --client
k9s version --short
docker --version
zsh --version
starship --version | head -1
