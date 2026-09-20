# Maintainer Training Tasks

This platform is built for you to learn from. **Claude writes the main implementation; you read it,
answer for it, and patch it.** The code arriving finished is not the end of the exercise — being
able to defend it is.

The end state is the system in [architecture.md](architecture.md), serving the requirements in
[draft.md](draft.md).

## How a task runs

Each task below has four parts:

1. **Built for you** — what Claude implements, with tests, in one change.
2. **Read first** — the two or three files where the interesting decision lives. Read them before
   the quiz; skimming is visible in the answers.
3. **Quiz** — questions you answer out loud, in the chat, before the task is closed. Wrong answers
   are the point; they show which mechanism is still a black box. Claude marks them and re-explains
   what missed.
4. **Your patch** — a small, sharply bounded change you make yourself, in the code Claude just
   wrote. It is small so that the compiler and the tests give you fast feedback, and chosen so that
   doing it requires having understood the surrounding code.

A task is done when the acceptance criteria hold, your patch is merged and tested, and you have
answered the quiz. Record the quiz answers you got wrong in [progress.md](progress.md) — that list
is the most useful thing in this repository for the next person, and for you in six months.

Claude's side of the contract: do not hand over the patch answer, even when asked twice. Explain the
mechanism, point at the file, refuse the diff.

> The previous version of this platform — hand-written Kubernetes client, hand-written queue,
> hand-written traffic split, no CRDs — is in git history at `3cb8302`. It is worth reading when a
> task here says "Kueue does this for you", because that commit is what "this" looks like when you
> do it yourself.

---

## Level 0 — Ground truth

### 0.1 Spike the stack and write down what is actually true

**Status: done.** `hack/set-up-cluster.sh` builds the cluster and installs cert-manager, KServe
v0.20.0 and Kueue v0.19.5 from nothing.

Still outstanding, and worth finishing before 1.5 because the answer shapes the `Release`
controller:

- Which traffic-split mechanism Standard mode actually uses — native `canaryTrafficPercent`, or a
  Gateway API `HTTPRoute` with weighted backends.
- What Kueue creates when you submit a suspended Job with a queue label, and which conditions appear
  on the `Workload` in which order.
- What each project's webhooks intercept, because yours will sit next to them.

**Read first:** `deps/kserve/pkg/controller/v1beta1/inferenceservice/reconcilers/ingress/`.

**Quiz**

1. Why does the API server refuse to call a webhook over plain HTTP, and what are the two separate
   things that must be in place before the first call succeeds?
2. Kueue has no cert-manager `Certificate` in this cluster but its `caBundle` is populated. By what
   mechanism? What does Kueue pay for that choice that KServe does not?
3. A webhook's serving certificate expires at 3am. What breaks, and what does the user see?

**Your patch:** add a `kubectl wait` to `hack/set-up-cluster.sh` that fails the script when
`kserve-controller-manager` does not become available. Right now the script trusts KServe's
installer exit code and reports success for a cluster that is not ready.

### 0.2 Scaffold and the first CRD

**Built for you:** `kubebuilder init` and `kubebuilder create api` for `Dataset`; the `Makefile`,
`PROJECT`, `api/v1alpha1/dataset_types.go`, generated deepcopy and CRD manifests, and an envtest
harness that runs green.

**Read first:** `api/v1alpha1/dataset_types.go`, `PROJECT`, `Makefile` (the `manifests`, `generate`
and `envtest` targets), `config/crd/bases/`.

**Quiz**

1. `make manifests` and `make generate` run different tools and write different files. Which is
   which, and what breaks if you commit the types but not the output?
2. What does the status subresource actually change about how the API server handles a write? Why
   does a controller need it?
3. Where does the validation live, and what enforces it — the reconciler, the API server, or the
   webhook? How would you prove your answer with one `kubectl` command?
4. `spec` is user intent and `status` is observed reality. Name a field you might be tempted to put
   in `spec` that belongs in `status`, and say why.
5. The first short name for this kind was `ds`. `kubectl get ds` printed "No resources found" while
   two datasets existed, rather than reporting an ambiguity. Why? What does that tell you about how
   `kubectl` resolves a short name, and what is the rule for choosing one?

**Your patch:** add a printcolumn so `kubectl get datasets` shows the phase without `-o yaml`, and
regenerate. Then break it on purpose: change a validation marker, run `make manifests`, and find the
resulting diff in `config/crd/bases/`.

**Acceptance criteria**

- `make manifests generate` is wired and its output is committed.
- `Dataset` has a status subresource, printcolumns worth reading, and validation expressed as
  markers rather than as code in the reconciler.
- `kubectl get datasets` shows owner, rows, golden, and phase without `-o yaml`.

---

## Level 1 — The domain

### 1.1 Dataset lifecycle

**Built for you:** the `Dataset` reconciler, the immutability webhook, and envtest coverage of both
including the failure paths.

**Read first:** `internal/controller/dataset_controller.go`, `internal/webhook/dataset_webhook.go`.

**Quiz**

1. The reconciler is level-triggered. Point at the line that would be wrong if it assumed it had
   seen the previous event, and say what would go wrong.
2. `observedGeneration` on a condition — what question does it let a user answer that the condition
   alone cannot?
3. The webhook rejects spec changes after creation. Name two ways a user could change the stored
   object anyway, and say which of them the webhook catches.
4. What happens if the reconciler returns an error? What happens if it returns `Requeue: true`? When
   is each correct?

**Your patch:** the immutability message names the field. Make it also say what the user should do
instead — create a new `Dataset` — and add the test case that asserts the message.

**Acceptance criteria**

- The controller validates the dataset exists at its URI and records rows and a checksum in status.
- A webhook rejects any change to a spec field after creation, with a message that says which field
  and why immutability matters here.
- Conditions carry `observedGeneration`, so a stale status is detectable.

### 1.2 TrainingJob on Kueue

**Built for you:** the `TrainingJob` type and reconciler, rendering a suspended `batch/v1` Job
labelled into a `LocalQueue`; status translation from Kueue's `Workload` conditions into the
platform's own vocabulary; artifact writing.

**Read first:** `internal/controller/trainingjob_controller.go`, and the Kueue `Workload` object in
the live cluster during a run.

**Quiz**

1. Why is the child Job created **suspended**? What would happen if it were not, and which component
   would you be fighting?
2. Trace one job from `kubectl apply` to a running pod, naming every controller that touches it in
   order.
3. `TrainingJob.spec` names no runtime. Name two things that would have to change if it did, when
   you swap `batch/v1` for `JobSet` in 5.2.
4. A user's job is not starting. Which condition tells them it is a quota problem rather than an
   image-pull problem, and where does the text come from?

**Your patch:** the status reports admission state. Add the queue position to the waiting message,
and the test that fills a `ClusterQueue` and asserts it.

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

**Built for you:** the `ModelVersion` type, registration from a succeeded `TrainingJob` with
`ownerReference` lineage, and the condition-based funnel with illegal transitions refused.

**Read first:** `api/v1alpha1/modelversion_types.go`, the transition table in
`internal/controller/modelversion_controller.go`.

**Quiz**

1. The funnel is conditions, not a phase string. What does that buy you the first time two things
   are true at once?
2. `ownerReference` from `ModelVersion` to `TrainingJob`: what does Kubernetes do with that, and
   what does it _not_ do that you might expect?
3. Deleting the `TrainingJob` garbage-collects the `ModelVersion`. Is that what you want? Defend
   either answer.

**Your patch:** add one printcolumn that makes `kubectl get modelversions` read as a release
dashboard rather than a list of names, and justify the choice in the PR description.

**Acceptance criteria**

- A succeeded `TrainingJob` registers a `ModelVersion` with `ownerReference` lineage back to the job
  and the dataset.
- The funnel is conditions, not a single string field, and illegal transitions are refused.
- `kubectl get modelversions` reads as a release dashboard.

### 1.4 Evaluation and gates

**Built for you:** the `Evaluation` type, Kueue admission reusing 1.2's machinery, gate evaluation
with platform defaults, and the `PromotionReady` / `PromotionBlocked` transition with a reason.

**Read first:** `internal/controller/evaluation_controller.go`, the gate defaulting.

**Quiz**

1. Why does `Evaluation` go through Kueue at all, rather than just running a pod?
2. A gate fails by 0.3%. Where does that number appear, and how many `kubectl` commands does a user
   need to find it?
3. Gates have platform defaults. Where are the defaults applied — the CRD markers, the webhook, or
   the reconciler? Why there and not the other two?

**Your patch:** add one gate of your own choosing to the defaults, with its test. Pick one that is
not a threshold on a metric, and explain what class of failure it catches.

**Acceptance criteria**

- `Evaluation` is Kueue-admitted, like training, and scores against a golden `Dataset` only.
- Results land in the `ModelVersion` status; the version moves to `PromotionReady` or
  `PromotionBlocked` with a reason naming the gate that failed and by how much.
- Gates are declared on the `Evaluation`, with platform defaults when omitted.

### 1.5 Release on KServe

**Built for you:** the `Release` type and the controller rendering an `InferenceService`, using
whichever traffic mechanism 0.1 establishes.

**Read first:** `internal/controller/release_controller.go`, and the generated `InferenceService` in
the cluster.

**Quiz**

1. This controller renders one CRD into another. Argue that it is _not_ a YAML translator, using the
   test in the review checklist. If you cannot, it should be deleted — say so.
2. KServe sets conditions on the `InferenceService`. Which of them does your controller read, and
   which does it copy into `Release.status`? What is the rule for when copying is acceptable?
3. A lane has no ready replicas. What stops traffic reaching it — your code, or KServe's?
4. Rollback is an edit to `Release` alone. What convergence property of the reconciler makes that
   true?

**Your patch:** add the guard that refuses a `canaryPercent` outside 0–100 — as a kubebuilder
marker, not as Go — and the test proving the API server rejects it before your code runs.

**Acceptance criteria**

- `Release` renders an `InferenceService`; traffic reaches the stable version.
- Setting `canaryVersion` and a percentage splits traffic, by whichever mechanism 0.1 established.
- Promote and rollback are edits to `Release` alone. Prove it: no other object is touched by hand.
- A lane with no ready replicas receives no traffic.

---

## Level 2 — Policy (the reason the platform exists)

### 2.1 The promotion gate as admission control

**Built for you:** the `ValidatingAdmissionWebhook` on `Release`, its certificate wiring, the
failure-policy decision with the reasoning in a comment, and the bypass-attempt test table.

**Read first:** `internal/webhook/release_webhook.go`, `config/webhook/`, `config/certmanager/`.

**Quiz**

1. Four bypass attempts are in the test table. One of them succeeds. Which, why, and what would it
   take to close it?
2. `failurePolicy: Fail` versus `Ignore` — which did the code choose, and what is the failure it
   accepts in exchange?
3. Your webhook and KServe's webhook both fire on a release. In what order, and can you rely on it?
4. The gate reads `ModelVersion` status during admission. Name the risk in doing I/O in an admission
   path, and what bounds it here.

**Your patch:** the rejection message states the current status. Make it also state what would make
the version promotable, and add the test that asserts the exact string.

**Acceptance criteria**

- A `ValidatingAdmissionWebhook` rejects a `Release` whose `canaryVersion` is not `PromotionReady`.
- The rejection message tells the user the current status and what would make it promotable.
- **Prove it cannot be bypassed:** try `kubectl apply`, try patching the subresource, try editing
  the `InferenceService` directly, and write down which of those the gate does and does not catch.
  The last one is the interesting case.
- Failure policy is deliberate: decide whether a down webhook blocks all releases or allows them,
  and defend the choice in a comment.

### 2.2 What CEL can do instead

**Built for you:** the object-scoped rules — percent bounds, replica floors, required owner — moved
out of Go into a `ValidatingAdmissionPolicy`, with the Go tests still passing.

**Read first:** `config/policy/`, and the Go code that was deleted (in the diff).

**Quiz**

1. Where exactly did the boundary fall, and what is the one property of the 2.1 gate that CEL cannot
   express?
2. A CEL policy and a webhook both reject the same bad object. Which is better, and on what axis?
3. What happens to a `ValidatingAdmissionPolicy` when the manager is down?

**Your patch:** express one more rule in CEL and delete its Go equivalent. Choose the rule yourself;
choosing correctly is the exercise.

**Acceptance criteria**

- Those rules move out of Go, and the tests still pass.
- A note on where the boundary fell, and why the gate in 2.1 could not cross it.

### 2.3 Quota and priority as configuration

**Built for you:** the two-team, two-namespace, one-cohort Kueue configuration under `config/`, the
preemption setup, and the demonstration scripts.

**Read first:** the `ClusterQueue` and `ResourceFlavor` manifests; no Go changes in this task.

**Quiz**

1. This whole task is YAML. What would it have cost to build in Go? Look at `3cb8302` before
   answering.
2. Borrowing: team A is idle, team B exceeds its nominal quota, then team A submits. What happens to
   team B's running job, and which setting decided it?
3. What mechanism actually protects serving from training — priority, quota, flavors, or node
   taints? Name it precisely.

**Your patch:** add a third team that can borrow but never lend, and show the asymmetry in a run.

**Acceptance criteria**

- Two teams, two namespaces, two `LocalQueue`s, one `ClusterQueue` cohort with borrowing.
- A high-priority job preempts a low-priority one; both outcomes are visible in `TrainingJob`
  status.
- Online inference is protected from training: show an experiment being refused GPUs that serving
  needs, and explain the mechanism that did it.

---

## Level 3 — Operability

### 3.1 Conditions, events, and metrics

**Built for you:** Events on every transition in every controller, and the domain metrics registered
on the manager's registry.

**Read first:** the `Recorder` calls across `internal/controller/`, and the metric definitions.

**Quiz**

1. Events expire. What does that mean for using them as an incident timeline, and what compensates?
2. Your metric is a gauge of versions by phase. Who sets it, and what goes wrong if two manager
   replicas both do?
3. `kubectl describe` on a stuck object explains the stall. Which field carries that, and what
   writes it?

**Your patch:** add the one metric you would want during an incident that is not in the list, and
defend the choice of type — counter, gauge, or histogram.

**Acceptance criteria**

- Every controller emits Events on transition; `kubectl describe` tells the whole story.
- Domain metrics on the manager's registry: versions by phase, queue depth, gate refusals, time from
  registration to stable.
- A `kubectl describe` of a stuck object explains the stall without a single log line being read.

### 3.2 Alerts

**Built for you:** the alert rules over 3.1's metrics, for the conditions [draft.md](draft.md)
names.

**Quiz**

1. Each alert names a runbook section. Why is that a hard requirement and not a nicety?
2. Pick one alert and describe its false-positive mode. What would make an on-call engineer start
   ignoring it?

**Your patch:** trigger two alerts on purpose and record the signal. Then tune one threshold and say
what you traded.

**Acceptance criteria**

- Each alert names the model, the owner, and a runbook section.
- Trigger at least two on purpose and show the signal firing.

### 3.3 Run an incident

**This one is yours.** Claude breaks the platform without telling you how — delete an
`InferenceService`, point a `Release` at a version whose artifact is gone, exhaust the quota, scale
the manager to two replicas, delete the webhook's certificate. You diagnose using only the
platform's own surfaces.

No code is written for you here. The whole point is that the code is already written and you still
have to understand it under pressure.

**Acceptance criteria**

- A written timeline: what you saw, what you checked, what the cause was, what you did.
- One improvement that would have made it faster.

### 3.4 Write the runbook

**Also yours.** `docs/runbook.md`, from what you learned in 3.3. Deploy, observe, break safely,
recover. Claude reviews it and tries to follow it literally; anywhere it cannot, the runbook is
wrong.

---

## Level 4 — Correctness

### 4.1 envtest for every reconciler

**Built for you:** envtest coverage of every controller including failure paths, kept under sixty
seconds.

**Quiz**

1. envtest runs a real API server but no kubelet. Name three things that therefore cannot be tested
   this way, and where they are covered instead.
2. What makes a controller test flaky, and which helper in this suite exists to prevent it?

**Your patch:** find the one failure path with no test and write it.

**Acceptance criteria**

- Each controller is tested against a real API server via envtest, including the failure paths.
- `go test ./...` stays under sixty seconds.

### 4.2 Idempotency and crash safety

**Built for you:** the double-reconcile tests, the crash-mid-loop test, and the two-manager leader
election test.

**Quiz**

1. "Reconciling twice equals reconciling once." Name the operation in this codebase that would break
   that if written naively, and how it is written instead.
2. The manager is killed between creating the Job and updating status. What repairs it, and on what
   trigger?
3. Leader election excludes the second manager. What is the window in which both could act, and why
   is it acceptable?

**Your patch:** add a crash point of your own choosing to the crash test, and fix whatever it
breaks.

**Acceptance criteria**

- A test proves reconciling twice equals reconciling once for every controller.
- Kill the manager mid-loop; no duplicated `ModelVersion`, no orphaned Job, no lost artifact.
- A test with two managers running proves leader election actually excludes one.

### 4.3 Contract tests for the release path

**Built for you:** the enumerated attempt table — every sequence of API calls tried against the
release path.

**Quiz**

1. The value is in the list, not the assertion. Which attempt in the table taught you something, and
   what?
2. Add one attempt to the list that is not there. Does it pass?

**Your patch:** that attempt, as a test.

**Acceptance criteria**

- No sequence of API calls moves an unevaluated version into serving traffic. Enumerate the attempts
  you made; the value is in the list, not the assertion.

### 4.4 End-to-end

**Built for you:** `make e2e`.

**Quiz**

1. The e2e run creates a cluster if needed. What makes that safe to run against a cluster you
   already have, and what would make it unsafe?
2. It exits non-zero on failure. Name the failure mode where it would exit zero and be wrong.

**Your patch:** make the cleanup step survive a failure in the middle of the run.

**Acceptance criteria**

- `make e2e` creates a cluster if needed, runs dataset → train → evaluate → canary → promote,
  asserts, cleans up, and exits non-zero on failure.

---

## Level 5 — GPUs and the cloud

### 5.1 Make the GPU request real on kind

**Built for you:** the fake device plugin or dedicated `ResourceFlavor`, wired so Kueue accounts for
the request and refuses a job when the quota is gone.

**Quiz**

1. An extended resource versus a `ResourceFlavor` — which did the code use, and what does the other
   one do better?
2. The previous platform accepted `gpus` and ignored it. What single check in this one makes that
   impossible now?

**Your patch:** make the refusal reason name the resource and the quota, and test the exact text.

**Acceptance criteria**

- GPUs are an extended resource advertised by a fake device plugin, or a dedicated `ResourceFlavor`;
  either way the request is accounted for by Kueue and a job is refused when the quota is gone.
- The refusal reason names the resource and the quota.

### 5.2 Swap the training runtime

**Built for you:** the child `batch/v1` Job replaced by a `JobSet` or Kubeflow `TrainJob`, with gang
semantics.

**Quiz**

1. Did `TrainingJob.spec` change? If it did, what did the original abstraction get wrong? That
   answer is worth more than the migration — write it in `progress.md` either way.
2. Gang semantics: all workers start or none do. Which component enforces that, and what does your
   controller have to avoid doing to let it?

**Your patch:** the runtime is now selectable. Add the validation that refuses an unknown runtime at
admission rather than at reconcile, and say why that is better.

**Acceptance criteria**

- **`TrainingJob.spec` does not change.** If it has to, say so in `progress.md` and explain what the
  original abstraction got wrong.
- Gang semantics hold: all workers start or none do.

### 5.3 Identity and ownership

**Built for you:** the mutating webhook stamping owner from the authenticated identity, per-team
RBAC, and the promotion authorization check.

**Quiz**

1. Owner comes from the authenticated identity, not the spec. Where does the webhook get it, and why
   can the user not forge it?
2. RBAC stops team A reading team B's datasets. Does it stop team A reading team B's _model
   versions_? Should it?
3. "Only an evaluation's owner can promote its model version" — is that RBAC, the webhook, or both?

**Your patch:** add the test that proves a forged owner field is overwritten, not rejected, and say
why overwriting is the right choice.

**Acceptance criteria**

- Owner comes from the authenticated identity, not the spec; a mutating webhook stamps it.
- RBAC per team namespace; one team cannot read another's datasets.
- Only an evaluation's owner can promote its model version.

### 5.4 Run it on a cloud cluster

**Yours to run**, with Claude on hand. The overlay is written; the cluster is real and costs money,
so the decision to run it is yours.

**Acceptance criteria**

- `config/overlays/cloud` deploys the same controllers against real GPU nodes with no Go changes.
- Real GPU metrics, real node topology, and a record of which node and which GPUs a job used.
- The delta between overlays is only flavors, storage classes, and resource names.

---

## Reviewing the work

Whoever wrote it, check it the way a platform reviewer would. Use this on Claude's code as hard as
on your own — finding a violation here is worth more than a passing quiz.

- Does every object have an owner, conditions, timestamps, and a failure reason?
- Can a user learn what went wrong from `kubectl describe` alone?
- Is the reconciler level-triggered? What happens if it is killed halfway through?
- Is every child object owned, so deleting the parent cleans up?
- **Does this controller carry policy, lineage, or a stable interface — or is it a YAML
  translator?** If the last one, delete it.
- Did you add the metric or event you will want during the incident this feature will cause?
- Would this survive a cloud cluster with no code change?
