#!/usr/bin/env bash
set -euo pipefail

KIND_VERSION=${KIND_VERSION:-v0.32.0}
KUBEBUILDER_VERSION=${KUBEBUILDER_VERSION:-4.9.0}
KUBECTL_VERSION=${KUBECTL_VERSION:-v1.34.8}
TARGET_ARCH=${TARGET_ARCH:-amd64}
# Same pins as agent.Dockerfile, so the shared hack/agent-home/.zshrc behaves the same in both
# containers and on the host.
STARSHIP_VERSION=${STARSHIP_VERSION:-1.25.0}
ZSH_AUTOSUGGESTIONS_COMMIT=${ZSH_AUTOSUGGESTIONS_COMMIT:-85919cd1ffa7d2d5412f6d3fe437ebdbeeec4fc5}
ZSH_SYNTAX_HIGHLIGHTING_COMMIT=${ZSH_SYNTAX_HIGHLIGHTING_COMMIT:-e6683f20e5c38d3e5447f45845d04ddda75d0f23}

# Docker creates a named volume's mount point, and every missing parent of a mount, owned by root
# -- and this script no longer runs as root, so those directories have to be handed to the user
# before anything writes under them. Same job as prepare_volumes() in hack/agent.sh, and the only
# place this script needs root for something that is not an install.
#
# $HOME/.cache matters beyond the tools volume: it is the parent Docker had to create for that
# mount, and it is also where GOCACHE lands, so left root-owned it fails the `go install`s below
# rather than anything that looks mount-related.
#
# The tools volume is the one chown that recurses, and only because a volume filled by an earlier
# root-running container still holds root-owned binaries; `make` has to be able to replace them.
# It holds a handful of tool binaries, so the walk is cheap.
#
# `if` rather than `[ -d ] && chown`, for the reason agent.Dockerfile spells out at its apt-source
# rewrite: a trailing failed test leaves the loop's exit status non-zero, and under `set -e` that
# ends the script on a directory that simply was not there.
for dir in "$HOME/.cache" "$HOME/.config" "$HOME/.kube"; do
  if [ -d "$dir" ]; then sudo chown "$(id -u):$(id -g)" "$dir"; fi
done
sudo chown -R "$(id -u):$(id -g)" "$HOME/.cache/k8s-ai-platform"

sudo curl -fsSLo /usr/local/bin/kind \
  "https://kind.sigs.k8s.io/dl/${KIND_VERSION}/kind-linux-${TARGET_ARCH}"
sudo chmod +x /usr/local/bin/kind

# The scaffold's go.kubebuilder.io/dl/<version> redirect 404s for pinned versions; the GitHub
# release asset is the stable location.
curl -fsSLo /tmp/kubebuilder \
  "https://github.com/kubernetes-sigs/kubebuilder/releases/download/v${KUBEBUILDER_VERSION}/kubebuilder_linux_${TARGET_ARCH}"
chmod +x /tmp/kubebuilder
sudo mv /tmp/kubebuilder /usr/local/bin/kubebuilder

sudo curl -fsSLo /usr/local/bin/kubectl \
  "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${TARGET_ARCH}/kubectl"
sudo chmod +x /usr/local/bin/kubectl

# The golang image ships bash only. The dotfiles themselves are bind-mounted from hack/agent-home/
# by devcontainer.json; this installs what they expect to find.
sudo apt-get update
sudo apt-get install -y --no-install-recommends zsh
sudo rm -rf /var/lib/apt/lists/*
# chsh on one's own account prompts for a password under PAM even with an empty one, so it goes
# through sudo like the installs above.
sudo chsh -s /bin/zsh "$(id -un)"

case "$TARGET_ARCH" in
  amd64) star_arch=x86_64-unknown-linux-gnu ;;
  arm64) star_arch=aarch64-unknown-linux-musl ;;
  *) echo "unsupported TARGET_ARCH: $TARGET_ARCH" >&2; exit 1 ;;
esac
curl -fsSL \
  "https://github.com/starship/starship/releases/download/v${STARSHIP_VERSION}/starship-${star_arch}.tar.gz" |
  sudo tar -C /usr/local/bin -xz starship

# The path .zshrc reads on the host too, so the shared file needs no container branch.
zsh_plugins="$HOME/.local/share/zsh/plugins"
for plugin in "zsh-autosuggestions:${ZSH_AUTOSUGGESTIONS_COMMIT}" \
              "zsh-syntax-highlighting:${ZSH_SYNTAX_HIGHLIGHTING_COMMIT}"; do
  name="${plugin%%:*}"
  commit="${plugin#*:}"
  rm -rf "${zsh_plugins:?}/${name}"
  mkdir -p "${zsh_plugins}/${name}"
  curl -fsSL "https://github.com/zsh-users/${name}/archive/${commit}.tar.gz" |
    tar -C "${zsh_plugins}/${name}" --strip-components=1 -xz
  test -r "${zsh_plugins}/${name}/${name}.zsh"
done

# The Go extension would otherwise prompt to install these unpinned on first open. gopls v0.22+
# needs Go 1.26, so v0.21.x is the newest that builds on this toolchain.
GOPLS_VERSION=${GOPLS_VERSION:-v0.21.1}
DELVE_VERSION=${DELVE_VERSION:-v1.27.2}
go install "golang.org/x/tools/gopls@${GOPLS_VERSION}"
go install "github.com/go-delve/delve/cmd/dlv@${DELVE_VERSION}"
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
