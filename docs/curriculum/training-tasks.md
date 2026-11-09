# Maintainer Training Tasks

This platform is built for you to learn Kubernetes operator work by constructing it from a blank
repository. The checked-in generated scaffold was archived in commit `131697f`; use that commit only
as a reference when you are stuck, not as something to copy first.

The end state is the system in [architecture.md](../product/architecture.md), serving the
requirements in [requirements.md](../product/requirements.md).

## How a task runs now

A task has five beats. They are kept apart on purpose, and only two of them are graded.

1. **Learn** — read the mechanism and watch it work before anything is built on top of it. No files
   change. Where the mechanism is upstream — Kueue, KServe, cert-manager, the API server — this
   means inspecting the live objects in your cluster. Where the task needs an upstream Go API, the
   constants, signatures and condition strings are written into the task, because locating an
   upstream API is not what this curriculum teaches.
2. **Spec** — Claude proposes the product decision in `docs/product/`, with the reasoning attached.
   You review it before any code exists. Status vocabulary, condition types, what a field promises:
   these are product decisions, and they belong in the product doc under review rather than being
   handed to you as an exercise with no written answer.
3. **Build** — Claude implements the feature against the accepted spec. `make lint fmt vet test`
   green is the entry condition for the next beat, not an achievement to report.
4. **Verify** — the tests, split. Claude writes some; the task names the ones left to you, chosen
   for what they teach. A test is review expressed as code, which is why this half stays yours.
5. **Review** — the graded beat. One or more exercises from
   [review-exercises.md](review-exercises.md): a variant comparison, a seeded defect review, a
   lifecycle narration, a blast-radius question, or a spec review. Answers go in
   [progress.md](../log/progress.md), and the reason is what is graded, never the verdict alone.

Then the **quiz**, which now comes after the review rather than after a build, because it grades
what the review surfaced. Wrong answers are the useful part; record them.

Each beat is a tagged commit on the `curriculum/review-format` branch, so the Spec is readable
before the code exists and the Build is readable as a diff. [stages.md](stages.md) has the git
commands for standing at a stage, reading a beat as a diff, and putting your own Verify work on a
branch off it.

What Claude will not do: hand over a review verdict, name the seeded defects, write the tests the
task assigned to you, or accept its own spec proposal. Asking "is this one of the bugs?" gets you a
question back. Asking "why does the informer cache make this stale?" gets you an answer — the
mechanism is never withheld, only the judgment.

Generated files are part of the lesson. When a task says a generator ran, inspect the diff and be
able to name which tool wrote each file: Kubebuilder, `controller-gen`, `kustomize`, or a
handwritten script.

Tasks already finished under the previous two-beat format keep what was built. Their **Review** beat
is written against the code as it stands, which is the one beat that works just as well in arrears.

> Reference point: commit `131697f` contains the previous generated Kubebuilder scaffold, `Dataset`
> CRD, envtest harness, and cluster scripts. `3cb8302` is the older handwritten platform.

---

## Level 0 — Bootstrap from nothing

Level 0 is environment work, and it is complete. There is nothing here for Claude to build: the
toolchain, the scaffold and the scripts were the owner's to set up, and a **Build** beat on "install
Go" would be theatre. What these entries gained in the rewrite is a **Review** beat, answerable now
against what is already in the tree.

### 0.0 Install the local toolchain

**Build:** install or verify the host tools before any repository code exists.

Required tools:

- Go 1.27 on `PATH` before any distro Go.
- Docker or another container runtime usable by kind.
- `kubectl`.
- `kind`.
- `kubebuilder` v4.16.x.
- Optional but useful now: `controller-gen`, `kustomize`, and `setup-envtest`; Kubebuilder's
  scaffolded Makefile will also install pinned local copies later.

**Look at:** the output of `go version`, `kubebuilder version`, `kubectl version --client`,
`kind version`, and `docker version`.

**Review:** exercise C — narrate what happens between `kubectl apply` and a pod running, naming
which of these tools is involved at each step and which are not involved at all. Most of them are
not.

**Quiz**

1. Which tools are runtime cluster tools, and which tools are code-generation/scaffolding tools?
2. Why do we pin generator versions instead of using whatever happens to be installed globally?
3. What is the difference between `kubebuilder` and `controller-gen`?

**Acceptance criteria**

- The commands above run successfully.
- You can explain which future task will first use each tool.

### 0.1 Repository hygiene before Go code

**Build:** keep the repository intentionally small before scaffolding. Add only documentation and
project hygiene that is not generated by Kubebuilder:

- `README.md` describing the platform goal and the blank-start training flow.
- `docs/product/requirements.md`, `docs/product/architecture.md`,
  `docs/curriculum/training-tasks.md`, and `docs/log/progress.md`.
- A minimal `.gitignore` for files already observed locally, such as editor state and agent/tool
  caches. Kubebuilder will generate its own `.gitignore` in 0.2; merge that generated file with this
  one rather than trying to predict every Go/Kubebuilder path now.
- Formatting/linting config for Markdown if you want it now; otherwise defer it to 0.6.

Do **not** add `Makefile`, `PROJECT`, `api/`, `cmd/`, `config/`, `internal/`, `go.mod`, or `go.sum`
by hand. Those arrive from Kubebuilder in 0.2.

**Look at:** the root file list. Every file should be explainable without saying "Kubebuilder made
it".

**Review:** exercise C — for each file in the root, say which tool owns it and what would break if
two tools both wrote it. `.prettierignore` and `kustomization.yaml` are the pair where the answer is
sharpest.

**Quiz**

1. Why is it useful to commit the repository before running `kubebuilder init`?
2. Which files are product documentation, and which files are developer workflow?
3. What belongs in `.gitignore` before generators run, and what should wait for Kubebuilder's
   generated `.gitignore`?

**Acceptance criteria**

- `git status --short` is clean.
- The root has no operator scaffold yet. Optional agent-sandbox files, if present, are not the
  operator scaffold.

### 0.2 Initialize the Kubebuilder project

**Build:** Kubebuilder v4.9.0, the version this curriculum was first written against, refused to
initialize in a directory containing non-scaffold entries such as `docs/`; v4.16 no longer does.
Either way, keep this repository as the committed learning baseline, but run Kubebuilder in a
temporary empty directory and copy the generated scaffold back:

```bash
mkdir -p ../k8s-ai-platform-scaffold
cd ../k8s-ai-platform-scaffold
kubebuilder init \
  --domain aiplatform.io \
  --repo github.com/tihaya-anon/k8s-ai-platform \
  --project-name k8s-ai-platform
cd ../k8s-ai-platform
cp -a ../k8s-ai-platform-scaffold/. .
rm -rf ../k8s-ai-platform-scaffold
```

The `cp -a source/. .` form is intentional: it copies hidden files such as `.dockerignore` and the
Kubebuilder-generated `.gitignore`. Inspect the diff and merge any README or `.gitignore` changes
consciously.

Before committing, replace Kubebuilder's generated `.devcontainer/` files with the pinned versions
in [agent-container.md](../tooling/agent-container.md). This is part of the main scaffold task
because the generated `post-install.sh` downloads `latest` CLIs, and generator/runtime tool versions
are load-bearing in this repository.

Commit the scaffold and devcontainer adjustments together before creating any API kind.

**Look at:** `PROJECT`, `go.mod`, `Makefile`, `cmd/main.go`, `.devcontainer/`, and a structural pass
over the initial `config/` tree. For `config/`, list the generated directories, identify each
kustomize entry point, and explain which Kubernetes object family each directory owns; do not
line-review every generated YAML field yet.

**Review:** exercise C — trace `make run` from the shell to a reconcile: what the Makefile invokes,
what the manager builds, where the client's cache comes from, and which of those steps needs a
cluster.

**Quiz**

1. Which files did Kubebuilder create that are source code, and which are deployment configuration?
2. Why does the generated manager use `controller-runtime` instead of a handwritten Kubernetes REST
   client?
3. Which Makefile targets call generators, and which targets talk to a live cluster?
4. What is recorded in `PROJECT`, and why does Kubebuilder need it for later scaffolding commands?

**Acceptance criteria**

- `go test ./...` or the generated equivalent passes.
- The Kubebuilder-only scaffold is committed separately from the first API kind.

### 0.3 Create the `Dataset` API and learn the generators

**Build:** run Kubebuilder's API scaffolder:

```bash
kubebuilder create api \
  --group ml \
  --version v1alpha1 \
  --kind Dataset
```

Allow it to create the resource and the controller scaffold when prompted, but keep the controller
logic empty until Level 1. Then edit `api/v1alpha1/dataset_types.go` yourself. The list below is
what to build; what each field _means_ and why the platform needs it is in
[domain.md](../product/domain.md#dataset) — read that first if `golden` or the two `checksum` fields
look arbitrary.

- `spec.uri`: non-empty dataset artifact location.
- `spec.owner`: non-empty human or team owner used for accountability and incident routing.
- Optional `spec.rows`: claimed positive row count supplied by the registrant.
- Optional `spec.checksum`: claimed artifact checksum supplied by the registrant.
- `spec.golden`: whether the dataset is approved for evaluation gates; default `false`.
- `status.conditions`: Kubernetes conditions as the source of truth for diagnosis.
- `status.phase`: short display summary, one of `Pending`, `Ready`, or `Failed`.
- Optional `status.observedRows` and `status.observedChecksum`: metadata resolved by the controller.
- Optional `status.lastResolvedTime`: timestamp of the last successful metadata resolution.
- validation markers for the user-facing contract.
- a status subresource.
- printcolumns required by the product contract: owner, expected rows, golden flag, phase, and age.
  Age is not a custom field; it is printed from `.metadata.creationTimestamp`.

Run both generators and inspect the diff:

```bash
make manifests
make generate
```

**Look at:** `api/v1alpha1/dataset_types.go`, `api/v1alpha1/zz_generated.deepcopy.go`,
`config/crd/bases/`, `config/rbac/`, and `config/samples/`.

**Review:** exercise A — `Dataset.spec.uri` as a bare string, as a typed `URI` with a CEL rule, or
as a struct naming scheme and path. Three shapes, all valid; argue for one and name what would make
you switch.

**Quiz**

1. `make manifests` and `make generate` run the same binary for different jobs. Which output belongs
   to each target?
2. Where does validation live after `make manifests`, and what enforces it at runtime?
3. What does the status subresource change about writes to `.spec` and `.status`?
4. Why are `observedRows` and `observedChecksum` status fields instead of spec fields?
5. Why should a short name avoid collisions with built-in resources like DaemonSet's `ds`?

**Acceptance criteria**

- `make manifests generate` is wired and its output is committed.
- `Dataset` validation is expressed as markers, not as reconciler code.
- The generated CRD defines printer columns so `kubectl get datasets` will show owner, rows, golden,
  phase, and age after a later task installs the CRD into a cluster.

### 0.4 Add API-server tests with envtest

**Build:** add the envtest suite that starts a real API server without running your reconciler. Test
the `Dataset` contract you generated in 0.3:

- valid sample creates successfully.
- invalid URI, owner, rows, or checksum is rejected by the API server.
- status writes go through the status client.
- normal spec updates do not accidentally mutate status.

**Look at:** `internal/controller/suite_test.go`, the first API tests, and the `setup-envtest`
Makefile target.

**Review:** exercise D — envtest starts an API server with no controller-manager and no scheduler
behind it. Name three behaviours a test can therefore never observe, and for each, what would have
to run instead.

**Quiz**

1. What is envtest running that a plain unit test is not running?
2. Why does this task test API validation before a reconciler exists?
3. What does this prove about marker-generated validation?

**Acceptance criteria**

- `make test` runs envtest and passes.
- At least one test proves the API server, not Go business logic, rejects invalid input.

### 0.5 Write the cluster setup scripts

**Build:** add the cluster lifecycle scripts yourself under `hack/`:

- `hack/set-up-cluster.sh` creates or reuses a kind cluster.
- It installs cert-manager, KServe v0.20.0, and Kueue v0.19.5 with pinned versions.
- It waits for the relevant controller deployments to become Available.
- `hack/shutdown-cluster.sh` stops or deletes the cluster in a predictable way.

Do not copy the archived scripts first. Use upstream install docs, run each command manually once,
then encode only the commands you understand.

**Look at:** the scripts, the live namespaces, controller deployments, webhook configurations, and
CRDs they install.

**Review:** exercise D — the setup script succeeds but a webhook's certificate has not been issued
yet. What does the next `kubectl apply` do, what does the user see, and which readiness check would
have caught it?

**Quiz**

1. Why should the script wait for deployments instead of trusting installer exit codes?
2. `helm install` and `helm upgrade --install` are both "idempotent" in some sense. Which one keeps
   the cluster converged when you change a pinned version, and why does the other quietly stop being
   the source of truth?
3. Helm refuses to adopt an object annotated with a different `meta.helm.sh/release-name`. What
   would go wrong if it allowed it?
4. A Helm chart is not saved to disk by `helm install`. Where do the objects it describes actually
   live afterwards, and which command shows you them?

**Acceptance criteria**

- Starting from no kind cluster, `hack/set-up-cluster.sh` leaves cert-manager, KServe, and Kueue
  Available.
- Re-running the script is safe.
- The shutdown script can stop or delete the cluster.

### 0.5a Learn certificates before you read a webhook

**Moved:**
[tls-and-admission.md](0.5-tls-and-admission.md#05a-learn-certificates-before-you-read-a-webhook).
Certificates come before Kubernetes here, so the openssl walkthrough lives in its own document.

Build a throwaway CA outside the repository, make verification fail once for an untrusted chain and
once for a name mismatch, then find the same three parts — a CA, a signed certificate, a client that
needs the CA — in the running cluster. Tasks 0.5b and 2.1 fail in ways you cannot read without this.

**Acceptance criteria**

- You can say which x509 field becomes a username and which becomes groups.
- You can name the two independent TLS checks and the distinct error text each produces.
- You can point at where the cluster CA certificate is stored for your kubeconfig, a running pod,
  and a webhook configuration.

### 0.5b Read the admission webhooks you just installed

**Moved:**
[tls-and-admission.md](0.5-tls-and-admission.md#05b-read-the-admission-webhooks-you-just-installed).

Inspection only. Read the webhook configurations cert-manager, KServe, and Kueue registered in your
cluster — `rules`, `clientConfig.service`, `clientConfig.caBundle`, `failurePolicy`,
`timeoutSeconds` — and find out which mechanism filled each `caBundle`. Do 0.5a first. This is the
reading half of the webhooks you write in tasks 1.1 and 2.1.

**Acceptance criteria**

- You can name the Service each webhook configuration points at.
- You can explain where its `caBundle` came from.
- You have seen one real admission rejection and recorded its error text.

### 0.6 Add development workflow after it hurts

**Build:** add only the workflow helpers you now have evidence for:

- `hack/format.sh` once there are Markdown, YAML, and Go files to format.
- git hooks once you have repeated a mistake they would catch.
- editor tasks or CI only after the local commands are stable.
- a development container only if host setup becomes a real source of drift.

**Look at:** any new workflow file and the command it wraps. A wrapper is not allowed to hide a
command you cannot run directly.

**Review:** exercise A — a workflow helper as a `make` target, as a script in `hack/`, or as a git
hook. The repository now has all three; say which concern belongs to which, and why `shellcheck` is
a hook rather than a formatter.

**Quiz**

1. What is the single source of truth for formatting?
2. Which generated files should formatters avoid touching?
3. Why is workflow automation added after the manual command is understood?

**Acceptance criteria**

- `make fmt vet test` or the project equivalent is the normal local gate.
- Workflow files are documented in README or progress notes.

---

## Level 1 — The domain

### 1.1 Dataset lifecycle

The first reconciler is taught, not assigned. A reconciler carries too many conventions to
rediscover by guessing — level-triggered reads, the status subresource, `observedGeneration`,
conditions, events on transitions, the three meanings of a return value — so this one task hands you
the code with the reasons attached, and then leaves a feature unimplemented.

**You type:**
[reconciler-walkthrough.md](1.1-dataset-reconciler.md#task-11-part-one--write-the-dataset-reconciler-by-hand)
— nine steps from the Kubebuilder scaffold to a `Dataset` that resolves its artifact, reports what
it found in conditions, emits events on transitions, and is covered by envtest. Type each step into
the file; the point of each one is the sentence after it.

**Spec:** `Dataset.status` is already specified in [domain.md](../product/domain.md#status). Review
it in arrears: does it say what happens when the artifact store is unreachable but a previous
observation exists, and is `Resolved=False` a failure or a fact?

**Build:** [part two](1.1-dataset-reconciler.md#part-two--the-part-that-is-yours) — drift detection
and the immutability webhook. The walkthrough resolves the artifact but never compares what it found
against the user's claim, which is the entire reason this CRD exists. _Built by the owner under the
earlier two-beat format, where this half was the assignment._ It stays as written; the beats below
were added in arrears.

**Verify:** yours — the envtest case that proves a rewritten artifact is noticed on a later
reconcile, and the case that proves `ValidateUpdate` rejects a frozen-field edit. Both are
negatives, which is the hard kind to write well.

**Look at:** `internal/controller/dataset_controller.go`,
`internal/webhook/v1alpha1/dataset_webhook.go`, conditions, events, and status updates.

**Design notes** — two decisions the walkthrough does not make for you:

- **Marker or webhook?** A Kubebuilder marker becomes OpenAPI schema inside the CRD, so the _API
  server_ enforces it: on every client, with no certificate, no pod, and no availability risk. Reach
  for the webhook only when the rule needs something the schema cannot see — another object, the
  previous version of this one, or a computed value. Schema defaults also apply on read, so changing
  a `+kubebuilder:default` retroactively changes objects already in etcd; webhook defaulting only
  ever touches an object on its way in.
- **Uniqueness is `metadata.name`, and nothing else.** Kubernetes has no unique index on a spec
  field. A webhook that lists existing `Dataset`s to reject a duplicate `uri` races against a
  concurrent create and reads a cache that may be stale, so it can only ever be advisory — which is
  what the `admission.Warnings` return value is for. Two registrations of the same URI with
  different checksums is a signal worth keeping, not an error.

**Review:** exercise A — immutability as a CEL transition rule on the type, versus the same rule in
`ValidateUpdate`. Both are written; say which belongs where and what the webhook can see that CEL
cannot. Then exercise D — the `Dataset` an admitted `TrainingJob` referenced is deleted mid-run.

**Quiz**

1. The reconciler is level-triggered. Point at the line in your own file that would be wrong if it
   assumed it had seen the previous event, and say what would go wrong.
2. `observedGeneration` on a condition — what question does it let a user answer that the condition
   alone cannot?
3. The webhook rejects spec changes after creation. Name two ways a user could try to change the
   stored object anyway, and say which of them the webhook catches.
4. What happens if the reconciler returns an error, and what happens with `RequeueAfter`? When is
   each correct? `Result.Requeue` is deprecated as of controller-runtime v0.22 — read its
   deprecation note and say which two things it was conflating.
5. Your immutability rule compares the old object with the new one. Name the one thing the webhook
   can do here that a `+kubebuilder:validation` marker cannot, and say which part of your rule could
   have been a marker all along. Task 2.2 revisits this.
6. The walkthrough injects `artifact.Resolver` instead of calling the store directly. Name two
   things that become testable because of that seam, and one cost the indirection adds.

**Acceptance criteria**

- A resync moves a `Ready` `Dataset` to `Failed` when its artifact content changed, with no user
  edit involved.
- The condition reason distinguishes an unreachable artifact from one that contradicts the claim.
- The immutability webhook rejects a `spec.uri` edit, and a test asserts the message.
- `make fmt vet test` passes.

### 1.2 TrainingJob on Kueue

**Learn:** [1.2-Kueue-and-jobs.md](1.2-kueue-and-jobs.md) — what Kueue actually does to a suspended
Job, read from the live objects in your cluster: `ResourceFlavor`, `ClusterQueue`, `LocalQueue`, the
`Workload` Kueue creates for your Job, and the exact field it flips to let the Job start. You create
the queues and submit a plain Job by hand first. No controller code in this half.

**Spec:** `TrainingJob.status` — the condition types, the reasons, and the mapping from Kueue's
`QuotaReserved` reasons into platform vocabulary — is proposed in
[domain.md](../product/domain.md#trainingjob-status). Review it before reading the implementation.
The question it has to answer: which of `WaitingForQuota`, `ExceedsMaxQuota` and `Misconfigured`
stay distinct, and why.

**Build:** the `TrainingJob` API and reconciler. It renders a suspended `batch/v1` Job labelled into
a `LocalQueue`, owner-references it, and translates Kueue `Workload` conditions into platform
vocabulary so a user learns why their job is waiting without learning what Kueue is. The Kueue Go
surface you need — the label keys, how to find the `Workload` for a Job, and the condition and
reason strings to translate — is in
[1.2-Kueue-and-jobs.md](1.2-kueue-and-jobs.md#what-kueue-hands-your-controller); looking it up is
not the exercise, deciding what the platform says about it is.

**Verify:** yours — the translation tests. Kueue's controller does not run in envtest, so you write
the `Workload` by hand and assert what the `TrainingJob` says: one case per reason you decided to
keep distinct in the Spec beat. Claude writes the rendering tests (one Job, suspended,
owner-referenced, labelled).

**Look at:** `internal/controller/trainingjob_controller.go`, child Job owner references, and the
live Kueue `Workload` during a run.

**Review:** exercise A — three ways to find the `Workload` for a Job: the owner field index, a list
on `kueue.x-k8s.io/job-uid`, and `jobframework.FindMatchingWorkloads`. All three are implemented.
Then exercise D — the Kueue mutating webhook is unreachable when your controller creates the Job.

**Quiz**

1. What object does Kueue create from a suspended Job, and what labels connect the chain?
2. Why does this controller translate Kueue conditions instead of exposing them raw?
3. Which fields on the child Job does this controller own, and which fields must it not fight?
4. What happens when quota is exhausted, and where should the user see that reason?
5. Your controller creates the Job suspended and Kueue unsuspends it. What does your reconciler do
   on its next pass, when it reads a Job whose `spec.suspend` no longer matches what it wrote?

**Acceptance criteria**

- A `TrainingJob` produces exactly one child Job, owner-referenced, created suspended.
- Kueue admits it, and the `TrainingJob` status says so in platform vocabulary.
- With quota exhausted, the status names quota as the waiting reason.

### 1.3 ModelVersion and the funnel

**Learn:** no new upstream machinery here — the mechanism is state transitions, and the material is
already in this repository. Read the funnel in [architecture.md](../product/architecture.md) and
write the transition table out by hand before any code: every state, every legal move, and for each
illegal move the reason it is illegal. Then look at how an existing controller refuses a move:
`kubectl get modelversion -o yaml` has nothing to show you yet, so instead read the `Workload`
conditions from 1.2 and notice that Kueue never jumps a `Workload` from Pending to Finished.

**Spec:** what a `ModelVersion` records versus what it references, and whether lineage survives the
deletion of what it points at. Proposed in [domain.md](../product/domain.md#modelversion) — the
distinction between an input and a record is the one to check.

**Build:** `ModelVersion`, registration from a succeeded `TrainingJob` with lineage back to the
`Dataset`, and condition-based transitions that refuse illegal state jumps.

**Verify:** yours — the test that proves a `ModelVersion` outlives the `TrainingJob` that produced
it with its lineage intact. Claude writes the transition-table coverage.

**Look at:** `api/v1alpha1/modelversion_types.go` and the transition table in the controller.

**Review:** exercise B — six seeded defects in the registration controller, drawn from this
repository's own failure modes. Then exercise C — narrate a succeeded `TrainingJob` becoming a
registered `ModelVersion`, naming every object written and by whom.

**Quiz**

1. Why is a model artifact not enough without lineage to the training job and dataset?
2. Which state transitions are legal, and what makes an illegal transition dangerous?
3. What should be an owner reference, and what should be only a recorded reference?
4. Lineage points at a `Dataset` that may be deleted later. What does your reference store so the
   lineage survives that, and what is lost anyway?

**Acceptance criteria**

- A succeeded `TrainingJob` produces a `ModelVersion` carrying the dataset and job it came from.
- An illegal transition is refused with a reason naming the attempted move.

### 1.4 Evaluation and gates

**Learn:** re-read the `Evaluation` and gate sections of
[architecture.md](../product/architecture.md), then watch the thing you are about to imitate: submit
a second Kueue-admitted Job while the first still holds quota, and observe that evaluation and
training compete for the same `ClusterQueue`. That competition is the reason `Evaluation` is a
Kueue-admitted workload rather than an in-process check.

**Spec:** the default gate policy — which metrics gate a promotion, what a missing metric means, and
whether a gate can be waived and by whom. The waiver question is the one with a compliance answer
rather than a technical one.

**Build:** `Evaluation`, Kueue admission for evaluation jobs, gate defaults, and `PromotionReady` /
`PromotionBlocked` status on the `ModelVersion`.

**Verify:** yours — the test that an `Evaluation` with a missing metric blocks rather than passes.
Fail-closed is the behaviour, and a test that proves it is the one that matters.

**Look at:** `internal/controller/evaluation_controller.go` and the default gate policy.

**Review:** exercise E on the gate policy, then exercise D — an `Evaluation` job is evicted by Kueue
preemption halfway through. Is the gate result unknown, failed, or retried, and which does the
platform owe the user?

**Quiz**

1. Why is a golden dataset a policy input rather than a label anyone can edit?
2. Which failures are infrastructure failures, and which are model-quality failures?
3. Why does the promotion gate consume conditions instead of raw metric names?
4. An `Evaluation` scores a model against the dataset it trained on. Which object should have
   prevented that, and at which layer?

**Acceptance criteria**

- An `Evaluation` moves its `ModelVersion` to `PromotionReady` or `PromotionBlocked`, with a reason
  either way.
- An infrastructure failure during evaluation does not read as a failed gate.

### 1.5 Release on KServe

**Learn:** KServe first, your CRD second. Apply an `InferenceService` by hand from the KServe
samples, watch it come up, then split traffic between two revisions and watch what KServe writes
back into its own status. Read which fields you set and which fields KServe owns:

```bash
kubectl get inferenceservice -A                     # k9s: :inferenceservices, then 0
kubectl get inferenceservice <name> -o yaml         # status.components and traffic fields; k9s: y
kubectl get revision,service.serving.knative.dev -A
```

Deleting that hand-made `InferenceService` afterwards is part of the exercise: it is the last time
you will create one directly.

**Spec:** what `Release` owns and what KServe owns. The percentage split, the rollback trigger, and
which `InferenceService` fields the platform refuses to expose are the decisions; the swap test is
whether `Release` survives KServe changing its own API.

**Build:** `Release`, and the controller that renders the KServe `InferenceService` from it.

**Verify:** yours — the test that a `Release` pointing at an ungated `ModelVersion` is refused.
Claude writes the rendering coverage for the `InferenceService`.

**Look at:** `internal/controller/release_controller.go`, the generated `InferenceService`, and
KServe status conditions.

**Review:** exercise A — traffic split expressed as a canary percentage on `Release`, versus two
named `Release` objects with weights. Then exercise C — narrate a promotion from `ModelVersion` to
serving traffic.

**Quiz**

1. Which fields on `InferenceService` does this controller own?
2. How does the controller avoid fighting KServe's own controller?
3. What condition tells the user the failure is in KServe rather than in the platform gate?
4. Why is traffic percent validation an API concern, not reconcile-time business logic?

**Acceptance criteria**

- A `Release` renders an `InferenceService`, and a rollback changes only the `Release`.
- Repeated reconciles do not fight KServe over fields it owns.

---

## Level 2 — Policy

### 2.1 The promotion gate as admission control

**Learn:** you already read three admission webhooks in
[0.5b](0.5-tls-and-admission.md#05b-read-the-admission-webhooks-you-just-installed) and wrote one in
1.1. What is new here is the certificate path for a webhook of your own. Before writing the gate,
make the plumbing visible:

```bash
kubectl get certificate -A                     # cert-manager's output; k9s: :certificates, 0
kubectl get validatingwebhookconfiguration -o yaml | grep -A3 caBundle
kubectl get secret -n <manager-ns> -o name | grep cert
```

Then read `config/webhook/manifests.yaml` and `config/certmanager/` in this repository and say which
tool wrote each file and which field connects them.

**Spec:** what the gate refuses and what it merely warns about, and whether a denial can ever be
bypassed. Proposed in [requirements.md](../product/requirements.md) — the part to check is what the
platform promises when the webhook itself is down.

**Build:** the validating admission webhook that refuses a `Release` pointing at a `ModelVersion`
that has not passed gates — with certificate wiring and bypass-attempt tests.

**Verify:** yours — the test that the webhook refuses a `Release` pointing at an ungated
`ModelVersion`, and the test that it still refuses when the `Evaluation` is missing entirely rather
than failed.

**Look at:** `internal/webhook/v1alpha1/release_webhook.go`, webhook manifests, certificate
resources, and failure policy.

**Review:** exercise D — the `ValidatingWebhookConfiguration` is deleted. Say what the platform's
guarantee is worth at that moment, and whether `failurePolicy: Fail` is the whole answer.

**Quiz**

1. Why does this policy belong at admission time instead of only in the reconciler?
2. What does `failurePolicy` mean during a webhook outage?
3. Which bypass attempts does the webhook catch, and which are handled by RBAC or API validation?
4. Your serving certificate must carry one particular `subjectAltName`, and the API server must be
   handed one particular CA. Name both, say which tool produces each, and give the error text that
   appears when each one is wrong. This is
   [0.5a](0.5-tls-and-admission.md#05a-learn-certificates-before-you-read-a-webhook) applied to code
   you wrote.

**Acceptance criteria**

- A `Release` naming an ungated `ModelVersion` is rejected at admission, not at reconcile.
- The rejection message names the gate that failed and what to do next.
- Deleting the webhook's Secret produces the x509 error you can already recognize from 0.5a.

### 2.2 What CEL can do instead

**Learn:** write a `ValidatingAdmissionPolicy` by hand against an existing type before touching your
own. A three-line policy that rejects a `ConfigMap` with a forbidden key is enough:

```bash
kubectl explain validatingadmissionpolicy.spec.validations
kubectl apply -f <your-policy>.yaml            # plus its ValidatingAdmissionPolicyBinding
kubectl create configmap probe --from-literal=forbidden=1   # watch it fail
```

Notice what you did not need: no Service, no certificate, no pod. That absence is the whole point of
this task.

**Spec:** which rules are object-scoped and which need another object. That split decides what can
become CEL, and it is a property of each rule rather than a preference.

**Build:** move the simple object-scoped rules out of your webhook into `ValidatingAdmissionPolicy`,
and keep cross-object rules in Go.

**Verify:** yours — the test that proves the CEL policy and the deleted Go code refuse the same
inputs. A migration test is the only evidence that a move was a move and not a loss.

**Look at:** the CEL policy manifests and the Go code deleted by the change.

**Review:** exercise A — the same rule as a `ValidatingAdmissionPolicy`, as a CRD CEL marker, and as
webhook Go. All three exist in the tree by now; say what each costs at upgrade time.

**Quiz**

1. Which validations can CEL express without a webhook server?
2. Why can CEL not replace the promotion gate entirely?
3. What operational failure disappears when a rule moves from webhook code to CEL?
4. `oldSelf` is available in a CEL transition rule. Which part of your 1.1 immutability webhook
   could have been a CRD validation rule from the start, and what would you have lost?

### 2.3 Quota and priority as configuration

**Learn:** 1.2 created one queue. Here you read what a two-team setup looks like before building it:

```bash
kubectl get clusterqueue -o yaml      # nominalQuota, borrowingLimit, cohort; k9s: :clusterqueues, y
kubectl get resourceflavor -o yaml    # k9s: :resourceflavors, then y
kubectl describe clusterqueue <name>  # admitted vs pending; k9s: select, then d
```

Say out loud what happens to a workload in queue A when queue B is idle, and what happens to it when
queue B wakes up. Then build the configuration that makes it true.

**Spec:** the quota policy itself — which teams, which cohort, what borrows from whom, and who gets
preempted. This is the task where the product decision _is_ the deliverable, so the spec review
carries most of the weight.

**Build:** two teams, two namespaces, one cohort, resource flavors, borrowing, and preemption.

**Verify:** yours — the test that demonstrates borrowing, and the one that demonstrates preemption.
Both need two namespaces and a full `ClusterQueue`, which makes them the first tests that cannot be
unit tests.

**Look at:** `ClusterQueue`, `LocalQueue`, and `ResourceFlavor` manifests.

**Review:** exercise E on the quota policy, then exercise D — a team's `LocalQueue` is deleted while
its jobs are admitted and running.

**Quiz**

1. What is quota denominated in?
2. What does borrowing allow that separate fixed queues do not?
3. How would a user tell they are waiting on quota rather than a broken image?
4. A borrowed workload is preempted mid-run. Which of your platform conditions changes, and what
   does the user need to be told?

**Acceptance criteria**

- Two namespaces submit into separate `LocalQueue`s backed by one cohort.
- A demonstrated borrow, and a demonstrated preemption when the lender needs its quota back.

---

## Level 3 — Operability

### 3.1 Conditions, events, and metrics

**Learn:** the manager already exposes metrics you did not write. Look at them before adding your
own:

```bash
kubectl port-forward -n <manager-ns> deploy/<manager> 8080:8080    # k9s: :deploy, select, shift-f
curl -s localhost:8080/metrics | grep controller_runtime_reconcile
kubectl get events --sort-by=.lastTimestamp -A | tail -20          # k9s: :events, then 0
```

`controller_runtime_reconcile_errors_total` and `..._time_seconds` come free from
`controller-runtime`. Decide what a _domain_ metric adds that those do not — if you cannot say, the
metric does not exist yet.

**Spec:** which transitions are worth an Event and which are noise, and what each metric is for. A
metric nobody alerts on and no dashboard reads is a cost with no reader.

**Build:** every state transition emits an Event, every CRD reports conditions, and the domain
metrics you justified are registered on the manager.

**Verify:** yours — the test that an Event is emitted on a transition and _not_ emitted on a no-op
reconcile. The second half is the one that catches an event storm before production does.

**Look at:** recorder calls, condition helpers, and metric definitions.

**Review:** exercise A — condition-only status versus a `phase` field alongside conditions. Core
Kubernetes does both, in different objects; say which this platform should do and why `PodStatus` is
not a precedent you can simply copy.

**Quiz**

1. What belongs in a condition message versus a log line?
2. Why are Events useful during an incident even when logs exist?
3. What metric would catch a stuck controller before users complain?
4. Events expire. Which of your signals must therefore not depend on them?

### 3.2 Alerts

**Learn:** an alert is a promise that a human should wake up. Before writing any, list the four
failure modes you have actually produced in this repository so far — quota exhaustion, a bad
`caBundle`, an unreachable artifact, a failed gate — and for each one decide whether a human can do
anything at 3am. That list is the alert set; everything else is a dashboard.

**Spec:** the four alerts and what each one promises. An alert is a claim that a human should wake
up, so the spec review is about what happens when the alert fires at 03:00 and nobody can act on it.

**Build:** alert rules over the metrics and conditions from 3.1.

**Verify:** yours — a unit test per alert rule over synthetic series, proving it fires when it
should and stays quiet otherwise.

**Look at:** alert manifests and the runbook links they carry.

**Review:** exercise D — the manager loses leader election and a standby takes over. Which of your
alerts fire, which should, and which are now lying?

**Quiz**

1. Which alerts page a human, and which only create dashboard context?
2. Why should an alert name the failed layer?
3. How do you avoid alerting on normal queueing?

### 3.3 Run an incident

**Learn:** nothing to read. This task is the Learn half of every task cashed in at once.

**Build:** intentionally break one layer — quota, KServe image pull, webhook certificate, or a bad
model gate — and diagnose it from Kubernetes objects before reading controller logs. Work in this
order and note where you first saw the truth: `kubectl get` on your own CRD, then its conditions,
then events, then the upstream object, then logs.

**Deliverable:** a short incident note in [progress.md](../log/progress.md), including which layer
you blamed first and whether that was right.

**Review:** this task _is_ exercise D, run against the live cluster rather than on paper. Break one
layer, predict the symptom before looking, then compare. The gap between the prediction and the
symptom is the whole lesson, and it goes in the log.

### 3.4 Write the runbook

**Learn:** re-read your own incident note from 3.3 and the error strings you recorded in 0.5a and
0.5b. A runbook page is worth writing exactly when the symptom is unambiguous and the fix is not
obvious from it.

**Build:** the operator runbook for the incidents already demonstrated. Every page starts with the
symptom as a user sees it, then `kubectl` commands, then the fix.

**Review:** exercise C — for each runbook page, narrate the path from symptom to cause without
reading the page you wrote. A page you cannot reconstruct from the mechanism is a page that will not
help at 03:00.

**Acceptance criteria**

- Every page maps to an incident you actually caused.
- No page describes a failure nobody has seen.

---

## Level 4 — Correctness

### 4.1 envtest for every reconciler

**Learn:** read your own suite first. `internal/controller/suite_test.go` starts a real API server —
find the lines that install the CRDs, the line that creates the client, and the reason there is no
manager running. Then run `make test` with `-ginkgo.v` and watch which specs are slow and why.

**Build:** fast envtest coverage for every controller failure path, not only the happy ones.

**Verify:** yours — the failure-path cases. Claude writes the happy paths and the table-driven
validation coverage; you write the case per reconciler where a dependency fails, because deciding
what failure to simulate is the part that needs judgment.

**Review:** exercise B — eight seeded defects spread across the existing suites, every one of them a
test that passes for the wrong reason. This is the hardest defect review in the curriculum: a test
that passes looks like evidence.

**Quiz**

1. What does envtest not have, and which bugs therefore cannot be caught there?
2. Why does a reconciler test call `Reconcile` directly instead of waiting for a watch to fire?

### 4.2 Idempotency and crash safety

**Learn:** take any reconciler you have written and call `Reconcile` twice in a row in a test. The
second call must produce no new child object, no new event, and no condition timestamp change. Do
this before reading further — the failures it finds are the content of this task.

**Build:** double-reconcile tests, crash-mid-loop tests, and leader-election coverage where it earns
its place.

**Verify:** yours — the double-reconcile test for every controller, and one crash-mid-loop case.
Claude wires the harness that makes a mid-loop crash reproducible.

**Review:** exercise C — narrate what a second manager replica does to each of your reconcilers with
leader election off. Name the first object that gets written twice.

**Quiz**

1. Which of your writes are idempotent by construction, and which are only idempotent by luck?
2. What does a crash between a child-object create and a status write leave behind, and how does the
   next reconcile recover?

### 4.3 Contract tests for the release path

**Learn:** write the attack table before the tests. Every API sequence that could put an ungated
model in front of traffic: direct `InferenceService` create, `Release` edit, `ModelVersion` status
patch, webhook down, CRD deleted and recreated. For each, name the layer that should refuse it.

**Spec:** the attack table — every API sequence that could put an ungated model in front of traffic.
Written as a product document because it is a statement of what the platform promises, not a test
fixture.

**Build:** the tests that prove the platform refuses every row of that table.

**Verify:** yours — one test per row that you added to the table. Claude implements the rows the
table already had.

**Review:** exercise E on the attack table, then exercise B — three seeded defects, each of which
opens exactly one row back up.

**Quiz**

1. Which row is caught by RBAC rather than by your code?
2. Which row survives a webhook outage, and is that acceptable?

### 4.4 End-to-end

**Learn:** read `test/e2e/` as it stands and run it once against a live kind cluster before adding
anything. Note what makes an e2e test slow and what makes it flaky — they are not the same problem.

**Build:** `make e2e` covering dataset registration through promoted release.

---

## Level 5 — GPUs and cloud

**Verify:** yours — the failure-path e2e case. Claude writes the happy-path suite; the flaky-or-not
judgment on a failure case against a live cluster is the thing worth learning.

**Review:** exercise A — e2e against kind in CI, versus e2e against a long-lived cluster, versus
envtest plus contract tests and no e2e at all. Argue for one as this platform's gate.

### 5.1 Make the GPU request real on kind

**Learn:** kind nodes have no GPUs, so look at how the cluster describes the resources it does have:

```bash
kubectl get node -o jsonpath='{.items[0].status.allocatable}' | tr ',' '\n'
kubectl describe node | grep -A5 Allocatable  # k9s: :nodes, select, then d
```

Extended resources are just named counters the kubelet advertises. Once that lands, a fake device
plugin stops being a trick and becomes the obvious move.

**Spec:** `AcceleratorClass` — what a class names, how it resolves to an extended resource and a
`ResourceFlavor`, and what the platform says when the class is unknown. Proposed in
[domain.md](../product/domain.md#acceleratorclass); 1.2 deliberately left the field unlowered
waiting for this.

**Build:** two things. First, a fake device plugin or a dedicated `ResourceFlavor` so Kueue accounts
for GPU-like quota on kind. Second, the `AcceleratorClass` CRD and the resolution step in the
`TrainingJob` controller that 1.2 deferred: a cluster-scoped registry object whose fields are in
[domain.md](../product/domain.md#acceleratorclass), and whose reason for existing — hardware support
is data an infrastructure engineer applies, not a Go interface a developer implements — is in
[architecture.md](../product/architecture.md#accelerators-are-a-registry-not-a-vocabulary).

Register your fake device as a class, point a `TrainingJob` at it by name, and check the resolution
both ways: a class that exists renders its `resourceName` into the pod's limits, and a class that
names hardware no node advertises reports itself unavailable instead of leaving a job queued with no
explanation.

**Verify:** yours — the test that an unknown class is refused with a condition naming the class,
rather than rendered into a pod that can never schedule.

**Review:** exercise A — the class resolved by a CRD lookup, by a ConfigMap, or by a kustomize
overlay per cluster. Then exercise C — narrate a GPU request from the `TrainingJob` field to the
device the kubelet advertises.

**Quiz**

1. Who advertises an extended resource, and who enforces it?
2. What does your fake prove about the platform, and what does it deliberately not prove?
3. A user writes `accelerator.class: a100-80g` and a vendor's resource name never appears in their
   manifest. Name two things that would have had to change in every `TrainingJob` in the cluster if
   it had.
4. `AcceleratorClass` carries `resourceName` and `flavor` but not node labels or tolerations. Which
   object owns those, and what breaks if this one restates them?

### 5.2 Swap the training runtime

**Learn:** read the JobSet or Kubeflow `TrainJob` API and list, field by field, what your
`TrainingJob` spec would have to carry that it does not today. If the list is empty, the abstraction
was real; if it is long, find out why.

**Build:** replace the child `batch/v1` Job with JobSet or Kubeflow TrainJob, without changing the
platform API.

**Verify:** yours — the test that proves a `TrainingJob` spec written before the swap still renders
correctly after it. That is the only test that proves the abstraction was real.

**Review:** exercise A — the runtime chosen by a spec field, by a cluster-wide config, or by which
CRDs are installed. 1.2 had a `spec.underlying` discriminator and it was removed for a reason; say
whether this task brings it back and what changed.

**Quiz**

1. Which part of your controller changed, and which part did not? That boundary is the stable
   interface the CRD was supposed to provide.

### 5.3 Identity and ownership

**Learn:** 0.5a already showed you where identity comes from — `CN` became your username, `O` became
your groups. Now see it at admission:

```bash
kubectl auth whoami
kubectl auth can-i create datasets --as=someone-else
```

An admission webhook receives that same identity in `AdmissionRequest.userInfo`. That is how an
owner field gets stamped rather than typed.

**Spec:** whose identity is stamped, from where, and what the platform does when the request carries
a service account rather than a human. The answer decides whether the owner field is accountability
or decoration.

**Build:** owner stamping from authenticated identity and per-team RBAC.

**Verify:** yours — the test that a user cannot stamp someone else's name on a `TrainingJob`, using
a webhook test with a forged `UserInfo`.

**Review:** exercise D — a user is removed from the team's RBAC group while their job is running.

**Quiz**

1. Why is a user-supplied `spec.owner` not an identity?
2. Which field of `AdmissionRequest` carries the authenticated user, and what must the webhook do
   about updates?

### 5.4 Run it on a cloud cluster

**Learn:** list every assumption in your manifests that is true only on kind — storage class,
ingress, node labels, the fake GPU resource, image pull paths. That list is the overlay.

**Spec:** the overlay boundary — which differences between kind and cloud are configuration and
which are policy. A difference that turns out to be policy does not belong in an overlay at all.

**Build:** cloud overlays for real storage, ingress, and GPU node flavors. No `if kind` in Go.

**Verify:** yours — the test that renders both overlays and asserts the Go code is identical. No
`if kind` is a claim, and that test is what makes it checkable.

**Review:** exercise C — narrate a `Release` serving traffic on the cloud cluster, naming every
component that differs from kind and every one that does not.

**Quiz**

1. Which differences belong in a kustomize overlay, and which belong in a value a controller reads
   at runtime?

---

## Reviewing the work

For every review, use this checklist:

- Is this adopting existing Kubernetes ecosystem behavior where possible?
- Is the API carrying policy, lineage, or a stable abstraction rather than translating YAML?
- Are reconcilers level-triggered and idempotent?
- Are child objects owner-referenced?
- Are status conditions useful from `kubectl describe`?
- Does each error name the failed layer?
- Are generated files regenerated and committed with the source change that caused them?
- Can the owner explain which CLI or controller produced every generated artifact?
