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

**Left undone:** anything you knowingly skipped, and why.
```

## Entries

### 0.0 Platform handover — 2026-09-17

> Archived. This entry describes the handwritten first version, replaced by the rebuild described in
> [architecture.md](architecture.md). The code and its `mvp.md` are in git history at `3cb8302`.

**Goal:** stand up the handwritten MVP from the requirements in [requirements.md](requirements.md).

**What I changed:** initial implementation — control plane API and reconcile loop, gateway, model
server, training and evaluation workers, CLI, kind cluster, manifests, unit tests.

**How I proved it:** `make test` passes; `make up && make demo` runs the loop from dataset
registration to a promoted stable version.

**What surprised me:** the release path is not the hard part — the gate that refuses an unevaluated
model is what separates a platform from a deployment script, and it is four lines of code in one
handler.

**Left undone:** everything in [training-tasks.md](training-tasks.md). That is the point.

### 0.0b Rebuild decision — 2026-09-18

**Goal:** stop hand-writing what the ecosystem already does, and find out where the platform
engineer's actual work is.

**What I changed:** discarded the v1 implementation. New direction in
[architecture.md](architecture.md): Kueue for queue and quota, KServe for serving and traffic,
custom resources and controllers for the domain, and the promotion gate as an admission webhook
rather than an `if` in an HTTP handler.

**What surprised me:** v1 accepted a `gpus` field on every training job, stored it, and never read
it. No resource request, no node selector, nothing. The API described a cluster that did not exist.

**Left undone:** all of [training-tasks.md](training-tasks.md).

### 0.1 Spike the stack — 2026-09-20

**Goal:** get cert-manager, KServe and Kueue onto a kind cluster from nothing, repeatably, with
versions pinned.

**What I changed:** `hack/set-up-cluster.sh` — creates the kind cluster, clones the KServe reference
checkout at a tag, and runs KServe's own Standard-mode installer (which brings cert-manager), then
applies Kueue. Every step checks for its own result first, so it is safe to re-run.

**Pinned versions:** kind node v1.36.1 · cert-manager v1.17.0 · KServe v0.20.0 · Kueue v0.19.5.

**How I proved it:** `./hack/shutdown-cluster.sh stop` or `./hack/shutdown-cluster.sh delete` then
`./hack/set-up-cluster.sh` from nothing; all three controllers reach Available.

**What surprised me:**

- Per the
  [dependency matrix](https://kserve.github.io/website/docs/install/dependencies#dependency-matrix),
  Standard mode needs only cert-manager — no Knative, no Istio. "Standard" is the rename of the old
  `RawDeployment`; see `deps/kserve/pkg/constants/constants.go`.
- KServe's installer on `master` failed with
  `ghcr.io/kserve/charts/kserve-crd:v0.21.0-rc1: not found`. Master bumps `KSERVE_VERSION` to the
  _next_ release as soon as one ships, so for most of a release cycle its installer points at a
  chart nobody has published. Install from a release tag, not from master.
- Kueue populates its webhook `caBundle` with no cert-manager `Certificate` anywhere: it self-signs
  at startup and patches its own webhook configurations. KServe delegates the same job to
  cert-manager's cainjector. Two valid answers to the same problem, with different failure modes at
  renewal time.

**Left undone:** the three spike questions the task actually asks — the Standard-mode traffic-split
mechanism, the `Workload` condition order, and what each project's webhooks intercept. The
traffic-split one blocks task 1.5.

### 0.2 Scaffold and the first CRD — 2026-09-20

**Goal:** a kubebuilder project and a `Dataset` CRD whose validation lives in markers, not in Go.

**Course correction:** the first scaffold was produced by the coding agent. Before continuing to
1.1, redo 0.2 from a blank scratch project or scratch branch: run `kubebuilder init`, run
`kubebuilder create api`, inspect the tree after each step, then run `make manifests` and
`make generate` and identify exactly which generated files changed.

**What I changed:** kubebuilder v4.9.0 scaffold; API group `ml.aiplatform.io`, module
`github.com/tihaya-anon/k8s-ai-platform`. `api/v1alpha1/dataset_types.go`, generated deepcopy and
CRD manifests, `config/samples/`, and `internal/controller/dataset_api_test.go` — 19 envtest specs,
none of which run a reconciler.

**How I proved it:** `make fmt vet test` green; `make install` plus
`kubectl apply -f config/samples/` lists both datasets; an invalid object is refused by the API
server with both offending fields named.

**What surprised me:**

- `make manifests` appeared to hang for 25 minutes. controller-gen expands `...` by walking the
  filesystem, so — unlike `go list ./...` — it does **not** skip the nested module in `deps/`, and
  it tried to resolve KServe's entire dependency graph. Naming our own trees in the Makefile: 0.8s.
- `kubectl get ds` printed "No resources found" rather than an ambiguity error, because `ds` is
  already DaemonSet's short name. Renamed to `dset`.

**Quiz answers:** _to fill in — record the ones I got wrong, not the ones I got right._

**Left undone:** redo the scaffold from blank as a learning exercise, finish the phase printcolumn
patch job, and then start the reconciler in 1.1.
