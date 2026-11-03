# Architecture

What this platform builds, and — more importantly — what it refuses to build.

The requirements are in [requirements.md](requirements.md). This document is how they are met. The
exercises that get you there are in [training-tasks.md](../curriculum/training-tasks.md).

---

## The one idea

An AI platform on Kubernetes is mostly **other people's controllers**. Queueing, quota, gang
scheduling, autoscaling, traffic splitting, GPU discovery — all of it exists, is CNCF-governed, and
is better than what you would write.

What does not exist, and what turns a collection of operators into a platform, is **policy**:

> A model version that has not passed its evaluation gates cannot receive production traffic.

Nobody ships that for you, because it encodes your organisation's judgement. That rule, the lineage
that makes it auditable, and the API your users actually touch — that is the code in this
repository. Most of the rest is configuration.

This is the same line you draw in a Spring service. You do not reimplement the IoC container; you
learn its annotations and spend your effort on the domain. Here `controller-runtime` is the
container, `kubebuilder` markers are the annotations, and the domain is model release.

---

## Build / adopt

| Concern                                | Owner                                | What you learn by adopting it                                                             |
| -------------------------------------- | ------------------------------------ | ----------------------------------------------------------------------------------------- |
| Queue, quota, preemption, borrowing    | **Kueue**                            | `ResourceFlavor` / `ClusterQueue` / `LocalQueue` topology, cohorts, `Workload` conditions |
| Serving, scaling, traffic split        | **KServe**                           | `InferenceService`, `ServingRuntime`, rollout mechanics                                   |
| Distributed training runtime           | deferred (JobSet / Kubeflow Trainer) | why the user-facing CRD must not name the runtime                                         |
| GPU discovery and health               | deferred (GPU Operator, DRA, DCGM)   | device advertisement, extended resources                                                  |
| **Domain objects and their lifecycle** | **this repo**                        | CRD design, reconcile contracts, conditions                                               |
| **The promotion gate**                 | **this repo**                        | admission control                                                                         |
| **Lineage and ownership**              | **this repo**                        | what makes an artifact auditable                                                          |

The failure mode of adopting a lot is writing a **YAML translator**: a controller whose entire job
is copying fields from your CRD into somebody else's CRD. If that is all a reconciler does, delete
the CRD and let users write the other one directly. A platform object earns its existence by
carrying policy, lineage, or a stable interface across a runtime swap — ideally all three.

---

## Custom resources

Five, all namespaced. The namespace is the team boundary, which is also Kueue's `LocalQueue`
boundary and the RBAC boundary. That alignment is deliberate and worth not breaking.

This section decides which objects exist and how they relate. What each field _means_, and which
platform decision would be impossible without it, is in [domain.md](domain.md) — including the
vocabulary the rest of these documents use without stopping to explain: artifact, lineage, base
model, golden dataset, gate, canary.

### `Dataset`

A versioned, immutable input. No Kubernetes analogue; a pure platform object.

```yaml
spec: { uri, owner, rows, checksum, golden }
status:
  { conditions, phase, observedRows, observedChecksum, lastResolvedTime }
```

`rows` and `checksum` appear on both sides on purpose. In `spec` they are the user's claim about
what they registered; in `status` they are what the controller actually found at the URI. A silently
rewritten artifact is only detectable because the two can disagree — which is the reason this is an
object at all rather than a URI string on a training job.

`status.phase` is only a short `kubectl get` summary; conditions remain the source of truth. The
initial API should allow these phases:

- `Pending` — accepted by the API server, but not resolved by the controller yet.
- `Ready` — the URI was resolved and the observed metadata matches any user-provided claims.
- `Failed` — resolution or validation failed; the condition reason names the failing layer.

Immutability is enforced in a webhook, not by convention. A golden dataset is the only kind an
`Evaluation` will score against — score a model on the data it trained on and every gate passes and
none of them mean anything.

### `TrainingJob`

A user's training request. The controller renders it into a `batch/v1` Job carrying
`kueue.x-k8s.io/queue-name`, creates it **suspended**, and lets Kueue admit it.

You do not implement queueing. You label into it, then read Kueue's `Workload` conditions
(`QuotaReserved`, `Admitted`, `Preempted`) back into your own status, so a user learns _why_ their
job is waiting without having to learn what Kueue is.

The spec must not name the runtime. Today the child is a Job; later it is a `JobSet` or a Kubeflow
`TrainJob`. If `TrainingJob.spec` survives that swap untouched, the abstraction was real.

### `ModelVersion`

A registered artifact and its position in the release funnel. The heart of the platform.

```mermaid
stateDiagram-v2
    [*] --> Registered
    Registered --> Evaluating
    Evaluating --> PromotionReady
    Evaluating --> PromotionBlocked
    PromotionReady --> Canary
    Canary --> Stable
    Canary --> Retired
    Stable --> Retired
    PromotionBlocked --> [*]
    Retired --> [*]
```

That graph is the set of legal transitions, not a `phase` field. The position is carried in
conditions, so that "evaluated but blocked" and "promotable but not yet canaried" are both
expressible; a single string would have to pick one. What the diagram defines is which moves the
controller refuses.

Status carries the metrics, the gate results, the source `TrainingJob`, and the `Dataset` that
produced it. That chain is the lineage: from serving traffic back to the rows that made the weights.

### `Evaluation`

Scores a `ModelVersion` against a golden `Dataset`. Also Kueue-admitted — evaluation competes for
the same GPUs as training, so it belongs in the same queue.

On completion it writes metrics into the `ModelVersion` status and moves it to `PromotionReady` or
`PromotionBlocked`, with a reason either way.

### `Release`

The serving intent for a logical model: which version is stable, which is canary, at what split. The
controller renders it into a KServe `InferenceService`.

Application code addresses the `Release`, never a version. A rollback changes this object and
nothing else — no application deploy, no image rebuild.

---

## The gate is an admission webhook

In the previous version of this repository the gate was an `if` statement in an HTTP handler. It
worked, and it protected exactly one path: callers who went through that API.

Here it is a `ValidatingAdmissionWebhook` on `Release`. A `Release` whose `canaryVersion` names a
`ModelVersion` that is not `PromotionReady` is **rejected by the API server**. That holds against
`kubectl apply`, against an Argo CD sync, against a Helm upgrade, against a well-meaning engineer at
2am, and against you.

That is the difference between a platform and a deployment script, expressed in the one place where
it cannot be routed around.

> **Why not `ValidatingAdmissionPolicy`?** CEL policies are the right tool when the rule concerns
> the incoming object, and they need no webhook server to run. They cannot reach into a _different_
> object's status, which is exactly what this rule does. Knowing where CEL stops is the lesson: use
> a policy for the rules that fit it, and a webhook for this one.

---

## From kind to a cloud cluster

The seam is **`ResourceFlavor`** — Kueue's model of "a kind of node", expressed as node labels plus
tolerations. Quota is denominated in flavors.

- **kind, no GPU:** a `cpu-sim` flavor over the kind nodes. Jobs request CPU and memory, and the
  quota arithmetic — borrowing, preemption, queue depth, preemption ordering — is entirely real.
  Only the hardware is not.
- **cloud, GPU:** a `gpu-a100` flavor selecting real nodes, quota denominated in `nvidia.com/gpu`.

The `ClusterQueue` shape, the controllers, and the CRDs do not change. Nothing in the repository may
hardcode a node label, a storage class, or a resource name; those live in kustomize overlays under
`config/overlays/{kind,cloud}`. Artifact storage sits behind an interface with a PVC implementation
now and an object-store implementation later.

**If a task cannot be done without an `if kind` branch in Go code, the design is wrong.**

---

## Layout

The repository is intentionally blank at the start of the curriculum. The previous generated
Kubebuilder scaffold was archived in commit `131697f`; the training path recreates it instead of
starting with it checked in.

Current files are documentation and project policy only:

```text
README.md
AGENTS.md
docs/
.markdownlint.json
.prettierrc
.prettierignore
.gitignore
```

As the levels progress, the repository should grow into standard Kubebuilder layout. Deviating from
that layout costs you every tool that assumes it.

```text
api/v1alpha1/          CRD types and kubebuilder markers
internal/controller/   one reconciler per kind, plus the envtest suite
internal/webhook/      promotion gate and immutability checks
internal/artifact/     artifact store interface: PVC now, object store later
cmd/main.go            the manager entrypoint
config/                kustomize — crd, rbac, manager, samples, prometheus
config/overlays/       kind and cloud differences
hack/                  scripts you wrote after running the commands manually
deps/                  gitignored pinned upstream checkouts, read as reference
test/e2e/              the loop against a live cluster
docs/
```

### Expected generator deviation

When you recreate the Makefile, keep Kubebuilder's generated targets first. If `deps/` later lives
under the repository root, adjust `make manifests` and `make generate` to name your own package
trees instead of a broad `./...`. `controller-gen` expands `...` by walking the filesystem rather
than by asking the Go tool, so it does **not** skip a nested module the way `go list ./...` does.
That difference is why the archived scaffold eventually used explicit paths.

---

## Conventions

- **Level-triggered, not edge-triggered.** A reconciler reads the world and makes it match the spec.
  It never assumes it saw the previous event. Reconciling twice equals reconciling once.
- **Status is conditions.** `type`, `status`, `reason`, `message`, `observedGeneration`,
  `lastTransitionTime`. A user diagnoses from `kubectl describe` without reading controller logs.
- **Every child object carries an `ownerReference`.** Garbage collection is Kubernetes' job. A
  finalizer exists only for cleanup Kubernetes cannot do, and every finalizer needs a documented
  removal path for when its controller is down.
- **Every state transition emits an Event.** The event log is the incident timeline.
- **Never mutate another operator's CRs beyond the fields you own.** Set what is yours, read their
  conditions, do not fight their controller.
- **Errors name the layer at fault.** A user must be able to tell "your quota is exhausted" from
  "KServe could not pull the image" from "your model failed its gates".
