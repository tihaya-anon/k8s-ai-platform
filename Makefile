# Everything in this Makefile runs offline: the Go build uses only the standard library and the
# container image has no base layer to pull.
CLUSTER   ?= ai-platform
NAMESPACE ?= ai-platform
IMAGE     ?= ai-platform:dev
KUBECTL   ?= kubectl --context kind-$(CLUSTER) -n $(NAMESPACE)

.PHONY: help
help: ## show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## ' $(MAKEFILE_LIST) | awk -F':.*?## ' '{printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2}'

.PHONY: fmt
fmt: ## format Go sources
	gofmt -w ./cmd ./internal

.PHONY: vet
vet: ## run go vet
	go vet ./...

.PHONY: test
test: ## run unit tests
	go test ./...

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
