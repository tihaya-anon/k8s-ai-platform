# Maintainer Training Tasks

You are the maintainer of this platform. These exercises turn the MVP into something closer to the
system described in [draft.md](draft.md). They are ordered so that each one leaves you able to do
the next; within a level, order does not matter.

How to work a task:

1. Read the acceptance criteria and write down how you will prove it works, **before** coding.
2. Make the change, with tests.
3. `make test && make deploy` and demonstrate it against the live cluster.
4. Record what you did and what surprised you in [progress.md](progress.md).

Every task below is done only when a reviewer could reproduce your proof from your notes.

---

## Level 0 — Learn the system (half a day)

### 0.1 Run the loop and narrate it

Run `make up && make demo`. Then answer, in `progress.md`, from evidence and not from memory:

- Which Kubernetes objects existed at each step, and who created them?
- Where did the model artifact come from, and who read it next?
- Which single API call moved traffic, and which object changed?

**Proof:** a timeline of the run with the `kubectl` and `platform ctl` output you used.

### 0.2 Break it on purpose

Fail a training job (`FAIL_AT_STEP` on the Job, or delete the pod mid-run) and watch the platform
report it. Then fail an evaluation gate by setting `-min-accuracy 0.99`.

**Proof:** the failure reason the platform recorded in both cases, and where it came from.

---

## Level 1 — Features (one to three days each)

### 1.1 Resume a failed training job

Today a failed job stays failed. Add `POST /v1/training-jobs/{name}/resume` that re-creates the
Kubernetes Job so the worker picks up from its latest checkpoint.

**Acceptance criteria**

- A resumed job keeps its name, increments `attempt`, and resumes from the last checkpoint step.
- The event log shows the original failure and the resume as separate entries.
- `platform ctl train resume -name ...` exists.
- Tests cover: resume of a failed job, refusal to resume a succeeded one.

### 1.2 Automatic canary analysis

The gateway already counts requests, errors, and latency per version. Make the platform judge its
own canary instead of waiting for a human.

**Acceptance criteria**

- The control plane reads gateway metrics on an interval.
- A canary that exceeds an error-rate or latency budget for N consecutive checks rolls back on its
  own and records why.
- A canary that stays healthy for a configured window becomes eligible for promotion, and the status
  page says so.
- Tests cover the decision logic without needing a live cluster.

### 1.3 Authentication middleware

The API is Huma over `net/http`, so identity belongs in middleware rather than in each handler.
Add one, either as a plain `http.Handler` wrapper or through `huma.Middlewares`.

**Acceptance criteria**

- Requests carry a token; the middleware rejects unauthenticated writes with 401 and leaves reads
  configurable.
- The owner recorded on an object comes from the identity, not from the request body.
- `/docs` can still drive the API: the spec declares the scheme and Swagger UI can authorise.
- Tests cover an authenticated write, an unauthenticated write, and an owner that cannot be spoofed.

### 1.4 Model version comparison

Add `GET /v1/models/{name}/compare?a=v1&b=v2` returning quality, latency, and cost side by side,
plus which gates each version passed.

**Acceptance criteria**

- `platform ctl model compare -model support-assistant -a v1 -b v2` prints a readable table.
- Missing or unevaluated versions produce a clear 4xx, not an empty table.

### 1.5 Batch inference plane

The largest gap against `draft.md`. Add a batch job that runs a model over a versioned input dataset
and registers a versioned output dataset.

**Acceptance criteria**

- A batch job declares input dataset, model version, shard size, retry policy, and idempotency key.
- Work is sharded; per-shard status, retries, and failure reasons are visible.
- Re-running a partially failed job completes only the missing shards and does not duplicate
  outputs.
- The output is registered as a new dataset version with lineage back to input dataset, model
  version, and job configuration.
- Throughput and estimated cost appear in the job report.

### 1.6 Quota and priority

Online inference must not lose GPUs to experiments.

**Acceptance criteria**

- A per-owner quota limits how many training jobs may run at once; the rest wait in `Queued` with a
  visible reason.
- Higher priority jobs schedule first.
- An operator can pause and resume a queue without editing user job specs.
- The status page shows queue depth and the oldest waiting job.

---

## Level 2 — Tests and correctness (half a day to two days each)

### 2.1 Raise controller coverage

`internal/controller` has tests only for the manifest builders. Introduce an interface for the
Kubernetes client, fake it, and test the reconcile loop.

**Acceptance criteria**

- Tests cover: job creation, success registering a model, failure recording a reason, evaluation
  applying gates, and serving converging after a route change.
- `go test ./...` stays under ten seconds.

### 2.2 End-to-end test

Add `make e2e` that runs the whole loop against a live kind cluster and asserts on the result, so
the demo script stops being the only proof the platform works.

**Acceptance criteria**

- One command creates a cluster if needed, runs the loop, asserts, and exits non-zero on failure.
- It cleans up what it created.

### 2.3 Concurrency and crash safety

Prove the claims the store makes.

**Acceptance criteria**

- A test runs concurrent writers under `go test -race` and passes.
- A test shows that state survives a control-plane restart mid-loop with no lost or duplicated model
  versions.

### 2.4 Contract tests for the API

Lock the release path so a refactor cannot loosen it.

**Acceptance criteria**

- Tests assert every status code in the promotion path, including that an unevaluated version can
  never be canaried by any sequence of calls.

---

## Level 3 — Operations and incidents (half a day each)

### 3.1 Write the alerts

Define the alert conditions `draft.md` asks for — high latency, high error rate, model load failure,
stuck queue, stale checkpoint — as concrete expressions over the metrics that exist today. Add any
metric that is missing.

**Acceptance criteria**

- Each alert names the affected model, owner, and a runbook section.
- You can trigger at least two of them on purpose and show the signal firing.

### 3.2 Run an incident

Have someone (or a script) break the platform without telling you how: delete a model server, point
a route at a version with no artifact, fill the artifact volume, or scale the control plane to two
replicas. Diagnose it using only the platform's own surfaces.

**Acceptance criteria**

- A written timeline: what you saw, what you checked, what the cause was, what you did.
- One improvement to the platform or the runbook that would have made it faster.

### 3.3 Backup and restore

The state file is the whole control plane.

**Acceptance criteria**

- A documented and tested procedure to back it up and restore it.
- An answer to: what does the cluster look like after a restore to an older state, and how does the
  controller reconcile the difference?

---

## Level 4 — Production readiness (two to five days each)

### 4.1 Move state to PostgreSQL

**Acceptance criteria**

- The store becomes an interface with a JSON implementation and a PostgreSQL implementation.
- Schema and migrations are in the repository.
- Tests run against both.

### 4.2 Split the control plane

Run the API and the controller as separate Deployments, with leader election on the controller.

**Acceptance criteria**

- Two controller replicas never both reconcile.
- The API stays available while the controller restarts.

### 4.3 Authentication and ownership

**Acceptance criteria**

- Requests carry an identity; the owner is taken from it rather than from the request body.
- Only an evaluation owner can promote; only a job's owner can cancel it.
- An unauthenticated request cannot change anything.

### 4.4 Real GPUs

Replace the simulation: real GPU resource requests, a real training workload, real GPU metrics, and
node topology in the scheduling decision.

**Acceptance criteria**

- A training job requests GPUs and is rejected with a clear reason when none are available.
- The platform records which node and which GPUs a job used.

### 4.5 CI

**Acceptance criteria**

- Every push runs format, vet, tests, and an image build.
- The pipeline fails on unformatted code or a failing test, and takes under five minutes.

---

## Reviewing your own work

Before you call a task done, check it the way a platform reviewer would:

- Does every new object have an owner, a status, a timestamp, and a failure reason?
- Can a user learn what went wrong without reading your code or the control-plane logs?
- Is the operation idempotent? What happens if the controller crashes halfway through it?
- Is there a recovery action, and is it one command?
- Did you add the metric or event you would want during the incident this feature will cause?
- Do the request and response types carry `doc:` tags and validation constraints, so the generated
  spec gives a stranger something they could fire from `/docs` and learn from? Is the snapshot in
  `internal/api/openapi.yaml` refreshed (`make openapi`) and reviewed?
