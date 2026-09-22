# Development container targets.
#
# These live outside the main Makefile on purpose: `kubebuilder init` writes its own Makefile into
# the project root, and it should be free to do so without clobbering this. Use it as:
#
#     make -f Makefile.agent agent-shell
#
# See docs/agent-container.md.

AGENT_IMAGE    ?= k8s-ai-platform-agent
AGENT_NAME     ?= k8s-ai-agent
AGENT_AI_MOUNTS ?=
GO_PROXY       ?= $(shell go env GOPROXY 2>/dev/null || printf direct)
NPM_REGISTRY   ?= $(shell npm config get registry 2>/dev/null || printf https://registry.npmjs.org)

# Neovim's plugins, Treesitter parsers and Mason downloads are large, fetched from the network, and
# rebuilt from scratch by every `docker run --rm`. They live in named volumes for the same reason
# the Go caches do. The configuration itself is baked into the image from hack/agent-home/, so
# these hold only derived state and can be deleted at any time.
NVIM_VOLUMES = \
	-v k8s-ai-nvim-share:/home/agent/.local/share/nvim \
	-v k8s-ai-nvim-state:/home/agent/.local/state/nvim \
	-v k8s-ai-nvim-cache:/home/agent/.cache/nvim

# The shell and editor configuration, mounted from the repository rather than read from the copy
# baked into the image. The image keeps that copy so it still works standalone, but a mount means
# editing hack/agent-home/ on the host takes effect in the next shell without a rebuild, and there
# is one source of truth for both sides.
#
# Read-only, except the Neovim directory: lazy.nvim rewrites lazy-lock.json when plugins are
# updated, and that belongs back in the repository as a diff to review. ~/.bash_env is absent on
# purpose -- it holds real tokens on the host and the .bashrc guard skips it here.
HOME_MOUNTS = \
	-v "$(CURDIR)/hack/agent-home/.bashrc:/home/agent/.bashrc:ro" \
	-v "$(CURDIR)/hack/agent-home/.bash_aliases:/home/agent/.bash_aliases:ro" \
	-v "$(CURDIR)/hack/agent-home/.blerc:/home/agent/.blerc:ro" \
	-v "$(CURDIR)/hack/agent-home/.config/starship.toml:/home/agent/.config/starship.toml:ro" \
	-v "$(CURDIR)/hack/agent-home/.config/nvim:/home/agent/.config/nvim"

# The review shell takes the same set with Neovim's configuration read-only too, to match the
# read-only workspace: a session that cannot change the code should not change its own editor.
HOME_MOUNTS_RO = \
	-v "$(CURDIR)/hack/agent-home/.bashrc:/home/agent/.bashrc:ro" \
	-v "$(CURDIR)/hack/agent-home/.bash_aliases:/home/agent/.bash_aliases:ro" \
	-v "$(CURDIR)/hack/agent-home/.blerc:/home/agent/.blerc:ro" \
	-v "$(CURDIR)/hack/agent-home/.config/starship.toml:/home/agent/.config/starship.toml:ro" \
	-v "$(CURDIR)/hack/agent-home/.config/nvim:/home/agent/.config/nvim:ro"

# Claude Code keeps its credentials, session history and installed skills under ~/.claude. A named
# volume means `claude /login` inside the container survives `docker run --rm`, so the host's
# credentials never have to be mounted in at all.
CLAUDE_VOLUME = -v k8s-ai-claude:/home/agent/.claude

.PHONY: help
help: ## show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: agent-image
agent-image: ## build the AI agent development container
	docker build \
		-f agent.Dockerfile \
		--build-arg USER_UID=$$(id -u) \
		--build-arg USER_GID=$$(id -g) \
		--build-arg GO_PROXY="$(GO_PROXY)" \
		--build-arg NPM_REGISTRY="$(NPM_REGISTRY)" \
		-t $(AGENT_IMAGE) .

.PHONY: agent-cache
agent-cache: agent-image ## ensure the named cache volumes are writable by the agent user
	docker run --rm --user root \
		-v k8s-ai-go-pkg:/home/agent/go/pkg \
		-v k8s-ai-go-build:/home/agent/.cache/go-build \
		$(NVIM_VOLUMES) \
		$(CLAUDE_VOLUME) \
		$(AGENT_IMAGE) \
		sh -c 'mkdir -p /home/agent/go/pkg/mod /home/agent/.cache/go-build /home/agent/.local/share/nvim /home/agent/.local/state/nvim /home/agent/.cache/nvim /home/agent/.claude && chown -R agent:agent /home/agent/go/pkg /home/agent/.cache /home/agent/.local /home/agent/.claude'

.PHONY: agent-deps
agent-deps: agent-cache ## download Go dependencies through the agent container
	docker run --rm \
		--cap-drop ALL \
		--security-opt no-new-privileges \
		-e GOPROXY="$(GO_PROXY)" \
		-v "$(CURDIR):/workspace/k8s-ai-platform:ro" \
		-v k8s-ai-go-pkg:/home/agent/go/pkg \
		-v k8s-ai-go-build:/home/agent/.cache/go-build \
		-w /workspace/k8s-ai-platform \
		$(AGENT_IMAGE) go mod download

.PHONY: agent-build
agent-build: agent-cache ## verify the source builds and tests inside the agent container
	docker run --rm \
		--cap-drop ALL \
		--security-opt no-new-privileges \
		-e GOPROXY="$(GO_PROXY)" \
		-v "$(CURDIR):/workspace/k8s-ai-platform:ro" \
		-v k8s-ai-go-pkg:/home/agent/go/pkg \
		-v k8s-ai-go-build:/home/agent/.cache/go-build \
		-w /workspace/k8s-ai-platform \
		$(AGENT_IMAGE) \
		bash -c 'go build ./... && go test ./...'

.PHONY: agent-fmt
agent-fmt: agent-cache ## format the tree with the container's pinned formatters
	docker run --rm \
		--cap-drop ALL \
		--security-opt no-new-privileges \
		-v "$(CURDIR):/workspace/k8s-ai-platform" \
		-w /workspace/k8s-ai-platform \
		$(AGENT_IMAGE) ./hack/format.sh

.PHONY: agent-lint
agent-lint: agent-cache ## run the markdown gate with the container's pinned markdownlint
	docker run --rm \
		--cap-drop ALL \
		--security-opt no-new-privileges \
		-v "$(CURDIR):/workspace/k8s-ai-platform:ro" \
		-w /workspace/k8s-ai-platform \
		$(AGENT_IMAGE) markdownlint-cli2 "**/*.md"

.PHONY: agent-shell
agent-shell: agent-cache ## open a repo-scoped development shell in the agent container
	docker run --rm -it \
		--name $(AGENT_NAME) \
		--cap-drop ALL \
		--security-opt no-new-privileges \
		--pids-limit 512 \
		-e GOPROXY="$(GO_PROXY)" \
		-v "$(CURDIR):/workspace/k8s-ai-platform" \
		-v k8s-ai-go-pkg:/home/agent/go/pkg \
		-v k8s-ai-go-build:/home/agent/.cache/go-build \
		$(NVIM_VOLUMES) \
		$(CLAUDE_VOLUME) \
		$(HOME_MOUNTS) \
		$(AGENT_AI_MOUNTS) \
		-w /workspace/k8s-ai-platform \
		$(AGENT_IMAGE)

.PHONY: agent-review
agent-review: agent-cache ## open a readonly repo-scoped shell in the agent container
	docker run --rm -it \
		--name $(AGENT_NAME) \
		--cap-drop ALL \
		--security-opt no-new-privileges \
		--pids-limit 512 \
		-e GOPROXY="$(GO_PROXY)" \
		-v "$(CURDIR):/workspace/k8s-ai-platform:ro" \
		-v k8s-ai-go-pkg:/home/agent/go/pkg \
		-v k8s-ai-go-build:/home/agent/.cache/go-build \
		$(NVIM_VOLUMES) \
		$(CLAUDE_VOLUME) \
		$(HOME_MOUNTS_RO) \
		$(AGENT_AI_MOUNTS) \
		-w /workspace/k8s-ai-platform \
		$(AGENT_IMAGE)

.PHONY: agent-nvim
agent-nvim: agent-cache ## populate the Neovim plugin volume from lazy-lock.json, then exit
	docker run --rm -it \
		--cap-drop ALL \
		--security-opt no-new-privileges \
		-e GOPROXY="$(GO_PROXY)" \
		-v "$(CURDIR):/workspace/k8s-ai-platform" \
		-v k8s-ai-go-pkg:/home/agent/go/pkg \
		-v k8s-ai-go-build:/home/agent/.cache/go-build \
		$(NVIM_VOLUMES) \
		$(HOME_MOUNTS) \
		-w /workspace/k8s-ai-platform \
		$(AGENT_IMAGE) \
		nvim --headless "+Lazy! restore" "+TSUpdateSync" +qa

.PHONY: agent-nvim-reset
agent-nvim-reset: ## delete the Neovim volumes; the next agent-nvim rebuilds them
	-docker volume rm k8s-ai-nvim-share k8s-ai-nvim-state k8s-ai-nvim-cache

.PHONY: agent-shell-ai
agent-shell-ai: agent-shell ## alias for agent-shell with optional AI credential mounts
