# Maintainer Training Tasks

You are building this platform, not just running it. These exercises take you from an empty
repository to the system in [architecture.md](architecture.md), which serves the requirements in
[draft.md](draft.md).

How to work a task:

1. Read the acceptance criteria and write down how you will prove it works, **before** coding.
2. Make the change, with tests.
3. Demonstrate it against the live kind cluster.
4. Record what you did and what surprised you in [progress.md](progress.md).

A task is done when a reviewer could reproduce your proof from your notes.

> The previous version of this platform — hand-written Kubernetes client, hand-written queue,
> hand-written traffic split, no CRDs — is in git history at `3cb8302`. It is worth reading when a
> task here says "Kueue does this for you", because that commit is what "this" looks like when you
> do it yourself.

---

## Level 0 — Ground truth (one day)

### 0.1 Spike the stack and write down what is actually true

Install Kueue and KServe on a kind cluster. Run each project's own sample end to end. Then write
down what you observed, because the rest of the plan depends on it.

Specifically, settle these before designing anything:

- Which KServe deployment mode works on kind without Knative and Istio, and **how traffic splitting
  works in that mode** — native `canaryTrafficPercent`, or a Gateway API `HTTPRoute` with weighted
  backends. This decides the shape of the `Release` controller. Do not take this document's word for
  it; versions move.
- What Kueue creates when you submit a suspended Job with a queue label, and which conditions appear
  on the `Workload` in which order.
- What each project's webhooks intercept, because yours will sit next to them.

**Acceptance criteria**

- `hack/` scripts create the cluster and install both, repeatably, from nothing.
- A note in `progress.md` answering the three questions above, with the `kubectl` output you used.
- Pinned versions recorded. "Latest" is not a version.

**Proof:** a fresh machine runs your scripts and gets a working cluster.

### 0.2 Scaffold and the first CRD

`kubebuilder init`, then `kubebuilder create api` for `Dataset`. Nothing else yet.

**Acceptance criteria**

- `make manifests generate` is wired and its output is committed.
- `Dataset` has a status subresource, printcolumns worth reading, and validation expressed as
  markers rather than as code in the reconciler.
- `kubectl get datasets` shows owner, rows, golden, and phase without `-o yaml`.

---

## Level 1 — The domain (one to three days each)

### 1.1 Dataset lifecycle

**Acceptance criteria**

- The controller validates the dataset exists at its URI and records rows and a checksum in status.
- A webhook rejects any change to a spec field after creation, with a message that says which field
  and why immutability matters here.
- Conditions carry `observedGeneration`, so a stale status is detectable.

### 1.2 TrainingJob on Kueue

**Acceptance criteria**

- `TrainingJob` renders a suspended `batch/v1` Job labelled into a `LocalQueue`; Kueue admits it.
- `TrainingJob.status` reports admission state in the platform's own words. A user who has never
  heard of Kueue can read why their job has not started.
- Quota exhaustion is visible: submit more than the `ClusterQueue` allows and the waiting jobs say
  so, with queue position.
- The job writes checkpoints and a final artifact to the artifact store.
- **The spec names no runtime.** Nothing in `TrainingJob.spec` mentions `batch/v1`.

**Proof:** fill the queue, show the wait, show the admission.

### 1.3 ModelVersion and the funnel

**Acceptance criteria**

- A succeeded `TrainingJob` registers a `ModelVersion` with `ownerReference` lineage back to the job
  and the dataset.
- The funnel is conditions, not a single string field, and illegal transitions are refused.
- `kubectl get modelversions` reads as a release dashboard.

### 1.4 Evaluation and gates

**Acceptance criteria**

- `Evaluation` is Kueue-admitted, like training, and scores against a golden `Dataset` only.
- Results land in the `ModelVersion` status; the version moves to `PromotionReady` or
  `PromotionBlocked` with a reason naming the gate that failed and by how much.
- Gates are declared on the `Evaluation`, with platform defaults when omitted.

### 1.5 Release on KServe

**Acceptance criteria**

- `Release` renders an `InferenceService`; traffic reaches the stable version.
- Setting `canaryVersion` and a percentage splits traffic, by whichever mechanism 0.1 established.
- Promote and rollback are edits to `Release` alone. Prove it: no other object is touched by hand.
- A lane with no ready replicas receives no traffic.

---

## Level 2 — Policy (the reason the platform exists)

### 2.1 The promotion gate as admission control

**Acceptance criteria**

- A `ValidatingAdmissionWebhook` rejects a `Release` whose `canaryVersion` is not `PromotionReady`.
- The rejection message tells the user the current status and what would make it promotable.
- **Prove it cannot be bypassed:** try `kubectl apply`, try patching the subresource, try editing
  the `InferenceService` directly, and write down which of those the gate does and does not catch.
  The last one is the interesting case.
- Failure policy is deliberate: decide whether a down webhook blocks all releases or allows them,
  and defend the choice in a comment.

### 2.2 What CEL can do instead

Re-express the rules that _are_ about the incoming object — percent bounds, replica floors, required
owner — as a `ValidatingAdmissionPolicy`.

**Acceptance criteria**

- Those rules move out of Go, and the tests still pass.
- A note on where the boundary fell, and why the gate in 2.1 could not cross it.

### 2.3 Quota and priority as configuration

The previous platform had a hand-written queue. This one has Kueue.

**Acceptance criteria**

- Two teams, two namespaces, two `LocalQueue`s, one `ClusterQueue` cohort with borrowing.
- A high-priority job preempts a low-priority one; both outcomes are visible in `TrainingJob`
  status.
- Online inference is protected from training: show an experiment being refused GPUs that serving
  needs, and explain the mechanism that did it.

---

## Level 3 — Operability

### 3.1 Conditions, events, and metrics

**Acceptance criteria**

- Every controller emits Events on transition; `kubectl describe` tells the whole story.
- Domain metrics on the manager's registry: versions by phase, queue depth, gate refusals, time from
  registration to stable.
- A `kubectl describe` of a stuck object explains the stall without a single log line being read.

### 3.2 Alerts

Define the conditions [draft.md](draft.md) asks for — high latency, high error rate, model load
failure, stuck queue, stale checkpoint — over the metrics you now have.

**Acceptance criteria**

- Each alert names the model, the owner, and a runbook section.
- Trigger at least two on purpose and show the signal firing.

### 3.3 Run an incident

Have someone break the platform without telling you how: delete an `InferenceService`, point a
`Release` at a version whose artifact is gone, exhaust the quota, scale the manager to two replicas,
delete the webhook's certificate. Diagnose using only the platform's own surfaces.

**Acceptance criteria**

- A written timeline: what you saw, what you checked, what the cause was, what you did.
- One improvement that would have made it faster.

### 3.4 Write the runbook

`docs/runbook.md`, from what you learned in 3.3. Deploy, observe, break safely, recover.

---

## Level 4 — Correctness

### 4.1 envtest for every reconciler

**Acceptance criteria**

- Each controller is tested against a real API server via envtest, including the failure paths.
- `go test ./...` stays under sixty seconds.

### 4.2 Idempotency and crash safety

**Acceptance criteria**

- A test proves reconciling twice equals reconciling once for every controller.
- Kill the manager mid-loop; no duplicated `ModelVersion`, no orphaned Job, no lost artifact.
- A test with two managers running proves leader election actually excludes one.

### 4.3 Contract tests for the release path

**Acceptance criteria**

- No sequence of API calls moves an unevaluated version into serving traffic. Enumerate the attempts
  you made; the value is in the list, not the assertion.

### 4.4 End-to-end

**Acceptance criteria**

- `make e2e` creates a cluster if needed, runs dataset → train → evaluate → canary → promote,
  asserts, cleans up, and exits non-zero on failure.

---

## Level 5 — GPUs and the cloud

### 5.1 Make the GPU request real on kind

The previous platform accepted a `gpus` field and silently ignored it. Do better without hardware.

**Acceptance criteria**

- GPUs are an extended resource advertised by a fake device plugin, or a dedicated `ResourceFlavor`;
  either way the request is accounted for by Kueue and a job is refused when the quota is gone.
- The refusal reason names the resource and the quota.

### 5.2 Swap the training runtime

Replace the child `batch/v1` Job with a `JobSet` or a Kubeflow `TrainJob` for multi-worker training.

**Acceptance criteria**

- **`TrainingJob.spec` does not change.** If it has to, say so in `progress.md` and explain what the
  original abstraction got wrong. That answer is worth more than the migration.
- Gang semantics hold: all workers start or none do.

### 5.3 Identity and ownership

**Acceptance criteria**

- Owner comes from the authenticated identity, not the spec; a mutating webhook stamps it.
- RBAC per team namespace; one team cannot read another's datasets.
- Only an evaluation's owner can promote its model version.

### 5.4 Run it on a cloud cluster

**Acceptance criteria**

- `config/overlays/cloud` deploys the same controllers against real GPU nodes with no Go changes.
- Real GPU metrics, real node topology, and a record of which node and which GPUs a job used.
- The delta between overlays is only flavors, storage classes, and resource names.

---

## Reviewing your own work

Before you call a task done, check it the way a platform reviewer would:

- Does every object have an owner, conditions, timestamps, and a failure reason?
- Can a user learn what went wrong from `kubectl describe` alone?
- Is the reconciler level-triggered? What happens if it is killed halfway through?
- Is every child object owned, so deleting the parent cleans up?
- **Does this controller carry policy, lineage, or a stable interface — or is it a YAML
  translator?** If the last one, delete it.
- Did you add the metric or event you will want during the incident this feature will cause?
- Would this survive a cloud cluster with no code change?
