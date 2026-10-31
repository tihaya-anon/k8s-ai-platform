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
for dir in "$HOME/.cache" "$HOME/.config" "$HOME/.kube"; do
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

# Into $LOCALBIN (a volume, see devcontainer.json) at the Makefile's pins; the editor's linter,
# hack/format.sh and Go Annotations' marker definitions all read them from there. Go Annotations
# only watches the workspace's bin/, so controller-gen has to be there before the editor starts.
make goimports golangci-lint controller-gen

go version
kind version
kubebuilder version
kubectl version --client
docker --version
zsh --version
starship --version | head -1
