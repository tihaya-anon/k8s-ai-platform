#!/usr/bin/env bash
set -euo pipefail

# The tools this script used to download on every rebuild -- kind, kubebuilder, kubectl, starship,
# the zsh plugins, gopls and dlv -- are now baked into the image by .devcontainer/Dockerfile.
# What is left is only what has to run against the live container: fixing volume ownership,
# linking the baked zsh plugins into $HOME, and the LOCALBIN tools that live on the named volume
# (so they persist across rebuilds and `make` skips them once present).

# Docker creates a named volume's mount point, and every missing parent of a mount, owned by root
# -- and this script no longer runs as root, so those directories have to be handed to the user
# before anything writes under them. Same job as prepare_volumes() in hack/agent.sh, and the only
# place this script needs root for something that is not an install.
# /go/pkg is the Go module cache volume (GOPATH is /go in the golang image). It is listed here
# rather than left to Go, because Go creates what it needs *under* the mount point but cannot
# create the root-owned mount point's contents in the first place.
for dir in "$HOME/.cache" "$HOME/.config" "$HOME/.kube" /go/pkg; do
  if [ -d "$dir" ]; then sudo chown "$(id -u):$(id -g)" "$dir"; fi
done
sudo chown -R "$(id -u):$(id -g)" "$HOME/.cache/k8s-ai-platform"

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

# Into $LOCALBIN (a volume, see devcontainer.json) at the Makefile's pins; the editor's linter,
# hack/format.sh and Go Annotations' marker definitions all read them from there. Go Annotations
# only watches the workspace's bin/, so controller-gen has to be there before the editor starts.
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
