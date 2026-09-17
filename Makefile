# Everything in this Makefile runs offline: the Go build uses only the standard library and the
# container image has no base layer to pull.
CLUSTER   ?= ai-platform
NAMESPACE ?= ai-platform
IMAGE     ?= ai-platform:dev
KUBECTL   ?= kubectl --context kind-$(CLUSTER) -n $(NAMESPACE)

AGENT_IMAGE    ?= k8s-ai-platform-agent
AGENT_NAME     ?= k8s-ai-agent
AGENT_AI_MOUNTS ?=
GO_PROXY       ?= $(shell go env GOPROXY 2>/dev/null || printf direct)

.PHONY: help
help: ## show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: setup
setup: ## prepare development deps and verify container builds
	$(MAKE) dev-deps
	$(MAKE) agent-deps
	$(MAKE) agent-build
	$(MAKE) image

.PHONY: fmt
fmt: ## format Go sources
	gofmt -w ./cmd ./internal

.PHONY: vet
vet: ## run go vet
	go vet ./...

.PHONY: test
test: ## run unit tests
	go test ./...

.PHONY: dev-deps
dev-deps: ## download development dependencies into the Go module cache
	go mod download

.PHONY: agent-image
agent-image: ## build the AI agent development container
	docker build \
		-f Dockerfile.agent \
		--build-arg USER_UID=$$(id -u) \
		--build-arg USER_GID=$$(id -g) \
		-t $(AGENT_IMAGE) .

.PHONY: agent-cache
agent-cache: agent-image ## ensure named Go cache volumes are writable by the agent user
	docker run --rm --user root \
		-v k8s-ai-go-pkg:/home/agent/go/pkg \
		-v k8s-ai-go-build:/home/agent/.cache/go-build \
		$(AGENT_IMAGE) \
		sh -c 'mkdir -p /home/agent/go/pkg/mod /home/agent/.cache/go-build && chown -R agent:agent /home/agent/go/pkg /home/agent/.cache/go-build'

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
		bash -c 'go test ./... && CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o /tmp/platform ./cmd/platform'

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
		$(AGENT_AI_MOUNTS) \
		-w /workspace/k8s-ai-platform \
		$(AGENT_IMAGE)

.PHONY: agent-shell-ai
agent-shell-ai: agent-shell ## alias for agent-shell with optional AI credential mounts

.PHONY: build
build: ## build the static linux binary the image uses
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o bin/platform-linux-amd64 ./cmd/platform

.PHONY: cli
cli: ## build the host CLI
	go build -o bin/platform ./cmd/platform

.PHONY: image
image: build ## build the container image
	docker build -t $(IMAGE) .

.PHONY: cluster
cluster: ## create the kind cluster and the shared artifact volume
	mkdir -p .local/artifacts
	kind create cluster --config deploy/kind-cluster.yaml

.PHONY: load
load: image ## push the image into the kind cluster
	kind load docker-image $(IMAGE) --name $(CLUSTER)

.PHONY: deploy
deploy: load ## apply the manifests and wait for the platform to be ready
	kubectl --context kind-$(CLUSTER) apply -f deploy/k8s/
	$(KUBECTL) rollout restart deployment/control-plane deployment/gateway
	$(KUBECTL) rollout status deployment/control-plane --timeout=120s
	$(KUBECTL) rollout status deployment/gateway --timeout=120s

.PHONY: up
up: cluster deploy ## create the cluster and deploy the platform

.PHONY: demo
demo: cli ## run the full product loop end to end
	./scripts/demo.sh

.PHONY: status
status: cli ## print the platform status page
	./bin/platform ctl status

.PHONY: logs
logs: ## follow control-plane logs
	$(KUBECTL) logs -f deployment/control-plane

.PHONY: clean-workloads
clean-workloads: ## delete every workload the platform created, keeping the platform itself
	$(KUBECTL) delete jobs,deployments,services -l app.kubernetes.io/managed-by=ai-platform

.PHONY: reset
reset: ## wipe platform state and artifacts, then restart the control plane
	# Artifacts are written by containers running as root, so they are cleaned from inside the node.
	docker exec $(CLUSTER)-control-plane sh -c 'rm -rf /artifacts/*'
	$(MAKE) clean-workloads || true
	$(KUBECTL) rollout restart deployment/control-plane
	$(KUBECTL) rollout status deployment/control-plane --timeout=120s

.PHONY: down
down: ## delete the kind cluster
	kind delete cluster --name $(CLUSTER)
