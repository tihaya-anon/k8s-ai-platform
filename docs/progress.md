# Maintainer Log

Your own log for [training-tasks.md](training-tasks.md). Keep it in the repository so the work is
portable: someone reading this file should be able to see what was built, what was learned, and what
is next.

One entry per task. Keep them short; the value is in the evidence and the surprises.

## Template

```markdown
### <task id> <task name> — <date>

**Goal:** one sentence.

**What I changed:** files, endpoints, objects.

**How I proved it:** the commands and the output that show it works, including the failure case.

**What surprised me:** the thing you would tell the next maintainer.

**Quiz answers:** record the answer as I gave it.

**Quiz review:** correction from the reviewer/code agent. Keep the critique next to the answer it
reviews.
```

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
