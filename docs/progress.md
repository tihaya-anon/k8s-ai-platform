# Maintainer Log

Your own log for [training-tasks.md](training-tasks.md). Keep it in the repository so the work is
portable: someone reading this file should be able to see what was built, what was learned, and what
is next.

One entry per task. Keep them short; the value is in the evidence and the surprises.

## Entries

### Archive before blank rebuild — 2026-10-07

**Goal:** preserve the generated scaffold before resetting the curriculum to start from blank.

**What I changed:** committed the previous state as `131697f` with message
`Archive generated kubebuilder scaffold before blank rebuild`, then removed the generated
Kubebuilder project, generated manifests, controller/test scaffolds, cluster scripts, local workflow
scripts, editor files, and CI files.

**How I proved it:** `git status --short` was clean after the archive commit; the follow-up deletion
leaves this repository as documentation plus project policy.

**What surprised me:** the generated project is not just `api/` and CRDs. `Makefile`, `PROJECT`,
`go.mod`, `cmd/main.go`, `config/`, RBAC, samples, tests, cluster scripts, and workflow helpers all
carry assumptions. Starting from blank makes those assumptions visible.

**Quiz answers:** none; this was a repository reset, not a training task.

### 0.0 Install the local toolchain — 2026-10-07

**Goal:** install tools

**What I changed:** install go, docker, kubectl, kind, kubebuilder

**How I proved it:**

```shell
go version
kubectl version
kind version
kubebuilder version
docker version
```

```shell
go version go1.25.14 linux/amd64
Client Version: v1.34.8
Kustomize Version: v5.7.1
Server Version: v1.36.1
Warning: version difference between client (1.34) and server (1.36) exceeds the supported minor version skew of +/-1
kind v0.32.0 go1.26.3 linux/amd64
Version: cmd.version{KubeBuilderVersion:"4.9.0", KubernetesVendor:"1.34.0", GitCommit:"5e331e74c7a25c8e8fc0d9d5c33c319b7268f395", BuildDate:"2025-09-22T10:53:21Z", GoOs:"linux", GoArch:"amd64"}
Client: Docker Engine - Community
 Version:           29.8.1
 API version:       1.56
 Go version:        go1.26.8
 Git commit:        4a63305
 Built:             Tue Sep 15 16:26:01 2026
 OS/Arch:           linux/amd64
 Context:           default

Server: Docker Engine - Community
 Engine:
  Version:          29.8.1
  API version:      1.56 (minimum version 1.40)
  Go version:       go1.26.8
  Git commit:       464cd50
  Built:            Tue Sep 15 16:26:01 2026
  OS/Arch:          linux/amd64
  Experimental:     false
 containerd:
  Version:          v2.3.6
  GitCommit:        ee2735368117d2eb259779949d5e75cdafec9761
 runc:
  Version:          1.5.1
  GitCommit:        v1.5.1-0-g8f2685a4
 docker-init:
  Version:          0.19.0
  GitCommit:        de40ad0
```

**What surprised me:** nothing

**Quiz answers:**

1. runtime: go, kubectl, kind, docker; code-generation: kubebuilder
2. to make it reproducible
3. kubebuilder works like spring initializer, controller-gen works like annotation parser

**Quiz review:**

1. The answer incorrectly puts Go in "runtime". Go is the language toolchain used to compile and
   install project binaries; it is not a cluster runtime tool. Docker/containerd is the container
   runtime layer, kind creates a Kubernetes cluster on that runtime, and kubectl talks to the API
   server. Kubebuilder is scaffolding; controller-gen is the generator that later reads markers.
2. "To make it reproducible" is correct but incomplete. Pinning generator versions matters because
   generated CRD schemas, RBAC, deepcopy code, and webhook manifests can change between tool
   releases even when the handwritten Go types did not. A teammate should not get a different diff
   just because their global controller-gen is newer.
3. The Spring Initializr / annotation parser analogy is useful. The missing detail is that
   Kubebuilder also writes and updates project metadata, manager wiring, config layout, and Makefile
   targets. controller-gen is narrower: it consumes Go packages plus markers and emits artifacts
   such as CRDs, RBAC, webhook config, and deepcopy methods.

The installed Kubebuilder is now `v4.9.0`, matching the training task. That is good: scaffolded
output can differ between Kubebuilder releases, so this keeps the exercise aligned with the archived
reference point.

### 0.1 Repository hygiene before Go code — 2026-10-07

**Goal:** maintain the repo

**What I changed:** almost nothing, just commit the repo and push to remote

**How I proved it:**

**What surprised me:**

**Quiz answers:**

1. because kubebuilder init requires an _empty_ dir
2. requirements.md and architecture.md, progress.md and training-tasks.md
3. editor files

**Quiz review:** Better, but still too compressed.

1. Corrected again after trying the real CLI: Kubebuilder v4.9.0 requires a stricter _empty scaffold
   directory_ than expected. It refused existing `cmd/`, and it also refused `docs/`. The safe
   workflow is to commit this repository as the learning baseline, run `kubebuilder init` in a
   temporary empty directory with `--project-name k8s-ai-platform`, then copy the generated scaffold
   back here. The reason to commit first is still diff hygiene: the next commit should show exactly
   what Kubebuilder generated.
2. `requirements.md` and `architecture.md` are product/design documentation. `training-tasks.md` and
   `progress.md` are curriculum/learning records, not product requirements. Workflow files are
   things like `.gitignore`, `.prettierrc`, `.prettierignore`, and `.markdownlint.json`.
3. "editor files" is correct for the pre-scaffold baseline, and the important refinement is timing:
   before generators run, ignore only artifacts already observed locally, such as `.idea` and tool
   caches. Kubebuilder generates its own `.gitignore` during 0.2, so Go/Kubebuilder-specific ignores
   should be merged from that generated file instead of guessed too early.

### 0.2 Initialize the Kubebuilder project — 2026-10-07

**Goal:** build the scaffold using kubebuilder cli

**What I changed:** just move the generated files here

**How I proved it:** `go test ./...` passes and `make help` shows the help messages

**What surprised me:**

**Quiz answers:**

1. only `./cmd/main.go` is the source code
2. to get type definitions so the schema are guaranteed during compiling
3. manifests and generate, Deployment
4. versions and metadata, maybe for compatibility?

**three most-used Makefile targets:**

1. manifests
2. generate
3. build

**Quiz review:** Good scaffold evidence: `go test ./...` passing proves the generated Go module
compiles and the empty test packages are valid, and `make help` proves the generated Makefile
targets are wired.

1. `cmd/main.go` is source code, but it is not the only source-like scaffold. `test/`, `go.mod`,
   `go.sum`, and `hack/boilerplate.go.txt` are also code/build inputs. Deployment configuration is
   mainly under `config/`, plus `Dockerfile`/`.dockerignore` for the manager image and `.github/`
   for generated workflow examples. `PROJECT` is Kubebuilder metadata, not runtime deployment YAML.
2. `controller-runtime` is not mainly about compile-time schemas. It is the controller framework:
   manager lifecycle, shared cache/informers, Kubernetes client, scheme registration, leader
   election, probes, metrics, and webhook integration. A handwritten REST client would make the
   project rebuild those operator mechanics by hand.
3. Generator targets are `manifests` and `generate`; targets such as `build`, `run`, `test`, and
   `deploy` call them as dependencies. Live-cluster targets include `install`, `uninstall`,
   `deploy`, `undeploy`, and the e2e kind targets. The target name is `deploy`, while `Deployment`
   is the Kubernetes object rendered under `config/manager/`.
4. `PROJECT` records the Kubebuilder CLI version, plugin/layout, project name, repo/module path, API
   domain, and config version. Kubebuilder uses it so later scaffold commands know how to extend
   this project consistently.

The three most-used targets are reasonable for this point. After the first API exists, expect
`make manifests generate` while changing markers/types, and `make fmt vet test` as the local gate.

`config/` review should stay structural for 0.2. It is enough to know that `config/default/`
composes the deployment, `config/manager/` owns the manager `Deployment`, `config/rbac/` owns
service account and permissions, `config/prometheus/` owns monitoring integration, and
`config/network-policy/` owns metrics access policy. Deep YAML line review belongs later when a task
changes one of those areas.

### 0.3 Create the `Dataset` API and learn the generators — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 0.4 Add API-server tests with envtest — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 0.5 Write the cluster setup scripts — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 0.6 Add development workflow after it hurts — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.1 Dataset lifecycle — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.2 TrainingJob on Kueue — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.3 ModelVersion and the funnel — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.4 Evaluation and gates — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 1.5 Release on KServe — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 2.1 The promotion gate as admission control — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 2.2 What CEL can do instead — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 2.3 Quota and priority as configuration — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 3.1 Conditions, events, and metrics — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 3.2 Alerts — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 3.3 Run an incident — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 3.4 Write the runbook — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.1 envtest for every reconciler — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.2 Idempotency and crash safety — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.3 Contract tests for the release path — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 4.4 End-to-end — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.1 Make the GPU request real on kind — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.2 Swap the training runtime — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.3 Identity and ownership — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**

### 5.4 Run it on a cloud cluster — YYYY-MM-DD

**Goal:**

**What I changed:**

**How I proved it:**

**What surprised me:**

**Quiz answers:**

**Quiz review:**
