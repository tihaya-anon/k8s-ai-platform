# Domain dictionary

The words this platform uses, defined once.

Every other document is free to say "golden dataset" or "the base model" without explaining it, and
every task is free to name a field without re-deriving why it exists. If a term is used in
`requirements.md`, `architecture.md` or a curriculum task and is not obvious from Go and Kubernetes
alone, it belongs here.

This is a dictionary, not a design. [architecture.md](architecture.md) decides which objects exist
and how they relate; this file says what each word and each field _means_, and what platform
decision depends on it. When the two disagree, architecture.md wins and this file is stale.

**How to read a field table.** _What it is_ is the domain meaning, in one clause — enough that a
backend engineer who has never trained a model can carry on. _Why the platform needs it_ is the
decision that would be impossible without the field; a field that answers this with "the training
code reads it" belongs in the container image, not in an API. _Constraints_ is what the API
enforces, because a constraint written only in prose is a convention and conventions rot.

---

## Vocabulary

Terms that are not fields on any one object.

- **Artifact** — the bytes a stage consumes or produces: a dataset file or directory, a set of model
  weights, an evaluation report. Artifacts live in object storage or on a volume and are addressed
  by URI. They are never stored in `etcd`: an API object carries the _reference_ and the _claims
  about_ an artifact, never the artifact. This is why `Dataset` is an object with a `uri` rather
  than a blob.
- **Lineage** — the chain of recorded references from serving traffic back to the inputs that
  produced it: `Release` → `ModelVersion` → `TrainingJob` → `Dataset`. It is what makes "why does
  this endpoint behave this way?" and "what else did that bad dataset touch?" answerable. Lineage is
  recorded by controllers as they act, not claimed by users, because a claim can be wrong and a
  record of what actually happened cannot.
- **Training** — running code that adjusts a model's weights until it performs better on some data.
  For the platform it is a long, GPU-hungry, resumable batch workload whose output is an artifact.
  Nothing else about it needs to be modelled.
- **Fine-tuning** — training that starts from an already-trained model's weights instead of random
  ones. Most training here is fine-tuning, which is why a training request names a **base model**:
  the run has two input artifacts, not one, so it has two lineage edges.
- **Checkpoint** — weights written partway through a run so it can resume rather than restart. It is
  the reason a preempted training job is cheap to interrupt, which is the reason preemption is a
  usable policy at all.
- **Evaluation** — scoring a trained model against held-out data to get comparable numbers. It is a
  batch job that reads two artifacts and writes metrics, and it competes for the same GPUs as
  training.
- **Golden dataset** — a dataset approved as a fixed yardstick for evaluation. Two properties make
  it policy rather than a label: it must not move, because scores only compare across runs if the
  thing they are measured against is identical; and it must not be the data the model trained on,
  because a model scored on its own training data passes every gate and none of the passes mean
  anything.
- **Metric** — one number produced by an evaluation, such as accuracy or p95 latency. Metrics are
  raw; gates are the judgement applied to them.
- **Gate** — a policy check that must pass before a `ModelVersion` may be promoted. A gate consumes
  conditions, not raw metric names, so that the policy survives a change in what is measured.
- **Promotion** — moving a `ModelVersion` forward through the release funnel. The funnel's legal
  moves are in [architecture.md](architecture.md); the refusal of an illegal move is the platform's
  job, not the user's discipline.
- **Canary** — a release state in which a small share of serving traffic goes to a new version while
  the rest stays on the current one, so that a bad version is discovered by a few requests rather
  than all of them.
- **Extended resource** — a named counter a kubelet advertises for hardware the core API has no
  field for, such as `nvidia.com/gpu` or `amd.com/gpu`. The name is the vendor's, the count is whole
  devices, and unlike CPU it cannot be overcommitted or fractionally shared: a pod asks for two or
  three, never 2.5, and the request and the limit must be equal. See
  [5.1](../curriculum/training-tasks.md#51-make-the-gpu-request-real-on-kind).
- **Accelerator class** — this platform's name for a kind of hardware a user may ask for, such as
  `a100-80g`. It exists so that no user-facing object ever contains a vendor's extended-resource
  name. A class is a cluster-scoped object registered by an infrastructure engineer, and it is the
  only thing that has to change when the fleet gains a new brand of accelerator; the decision to
  make it data rather than a Go interface is in
  [architecture.md](architecture.md#accelerators-are-a-registry-not-a-vocabulary).
- **Quota, queue, admission** — Kueue's vocabulary, not this platform's. A `ClusterQueue` holds the
  quota, a `LocalQueue` is its namespaced front door, and _admission_ is the moment Kueue decides a
  workload may start. See [1.2](../curriculum/1.2-kueue-and-jobs.md).

---

## `Dataset`

A versioned, immutable input. Implemented — `api/v1alpha1/dataset_types.go` is the source of truth
for the markers.

### `spec`

| Field      | What it is                                                  | Why the platform needs it                                                                                                                        | Constraints           |
| ---------- | ----------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------- |
| `uri`      | where the dataset's bytes actually live                     | the only way to reach the artifact, and the address a drift check re-resolves                                                                    | required, 1–128 chars |
| `owner`    | the human or team accountable for it                        | incident routing and accountability — a failing dataset needs someone to wake, and "the platform" is not a someone                               | required, 1–128 chars |
| `rows`     | how many records the registrant says it has                 | a claim to check the artifact against, so a truncated upload is caught at registration rather than by a model that trains on a tenth of the data | optional, minimum 1   |
| `checksum` | the artifact digest the registrant says it has              | the other half of the same check, and the one that catches a silent rewrite rather than a truncation                                             | optional, 1–256 chars |
| `golden`   | whether this dataset is approved as an evaluation yardstick | makes the dataset a policy input: an `Evaluation` scores only against golden data, so flipping this bit changes what the promotion gate means    | defaults to `false`   |

`uri` is an address, not an identity. Two `Dataset`s may carry the same one, and nothing stops them:
uniqueness in Kubernetes is `metadata.name` and nothing else, because there is no unique index on a
spec field. A webhook that listed siblings to reject a duplicate would race a concurrent create and
read a possibly stale cache, so it can only ever warn — which is what
`internal/webhook/v1alpha1/dataset_webhook.go` does. Two registrations of one URI whose checksums
disagree is a signal a human should see, not an error. What identifies _content_ is `checksum`; what
identifies the _object_ is its name.

`rows` and `checksum` are claims, deliberately. They are what the registrant asserts, and the
controller writes what it actually found into `status`. The pair existing on both sides is the
entire reason `Dataset` is an object instead of a URI string on a training job: a silently rewritten
artifact is only detectable because the claim and the observation can disagree.

### `status`

| Field              | What it is                                     | Why the platform needs it                                                |
| ------------------ | ---------------------------------------------- | ------------------------------------------------------------------------ |
| `conditions`       | the standard Kubernetes condition list         | the source of truth for diagnosis, and what a gate consumes              |
| `phase`            | `Pending`, `Ready` or `Failed`                 | a one-word `kubectl get` summary; never the source of truth              |
| `observedRows`     | the row count the controller actually resolved | the observation half of the claim/observation pair                       |
| `observedChecksum` | the digest the controller actually resolved    | the same, for silent rewrites                                            |
| `lastResolvedTime` | when resolution last succeeded                 | tells a reader whether `Ready` is current or stale from before an outage |

---

## `TrainingJob`

A user's training request. In progress — task [1.2](../curriculum/1.2-kueue-and-jobs.md) builds it.

| Field          | What it is                                                           | Why the platform needs it                                                                                                                                                                  | Constraints                         |
| -------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------- |
| the `Dataset`  | the registered dataset this run trains on, by `metadata.name`        | the first lineage edge, and an admission check: a job pointing at a missing or non-`Ready` dataset is refused at submission rather than discovered as a crash loop inside a GPU allocation | a reference, never an embedded copy |
| the base model | the trained weights this run starts from                             | the second lineage edge and its own licence; also the dominant term in GPU memory, so it decides whether the resource request below can ever fit                                           | required for fine-tuning            |
| the image      | the container holding the training code, framework and GPU libraries | the code version, and the third lineage edge                                                                                                                                               | pin by digest, not tag              |
| the resources  | CPU, memory, and optionally an accelerator class and a device count  | the only numbers Kueue can do arithmetic on against `nominalQuota`, so they have to land on the Pod template where Kueue reads them. The field list is [below](#specresources)             | see the table below                 |
| the queue      | the `LocalQueue` this run belongs to                                 | where the job meets the team boundary and the fairness policy — whose quota it spends and who preempts it                                                                                  | defaulted per namespace             |

**By name** means the `metadata.name` of a `Dataset` in the _same namespace_ — not a UID, not a
`corev1.ObjectReference`, not the object itself. Same namespace because architecture.md aligns the
namespace with the team boundary, Kueue's `LocalQueue` boundary and the RBAC boundary, so a
reference that crossed it would let a job read data its submitter has no permission to read. A name
rather than a richer reference because the kind is already fixed — it is always a `Dataset` — so
`apiVersion` and `kind` would be noise, and a pinned `resourceVersion` would be worse than noise.
The exact Go shape, a bare string or a one-field reference struct, is 1.2's to choose.

A name is not the same question as what lineage records. This field is an _input_, resolved and
validated when the job is admitted; the lineage reference a `ModelVersion` carries is a _record_ of
what happened, and it has to survive the deletion of what it points at. That is 1.3's problem.

Two rules the field list does not state. **A reference, not a copy**: embedding a `Dataset` object
into the spec snapshots it, so the copy cannot be checked for `Ready`, goes stale the moment the
real object changes, and drags the whole `DatasetSpec` schema into this CRD. **Pin the image by
digest**: a tag can be repointed, and two `TrainingJob`s with byte-identical specs producing
different models is the failure that makes an audit trail worthless.

What must _not_ be in the spec: replica counts, framework-specific knobs, gang-scheduling settings,
`suspend`. Those describe how a runtime executes the job, and the spec has to survive the swap from
a `batch/v1` Job to a `JobSet` or a Kubeflow `TrainJob` untouched. The moment it names a framework,
the swap breaks and the CRD was a YAML translator after all.

### `spec.resources`

The one field in the list above that is a struct rather than a scalar, because "how much hardware"
is four answers, not one.

```yaml
spec:
  resources:
    cpu: "8"
    memory: 32Gi
    accelerator:
      class: a100-80g
      count: 2
```

| Field               | What it is                                           | Why the platform needs it                                                                                                                                                                  | Constraints                                         |
| ------------------- | ---------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | --------------------------------------------------- |
| `cpu`               | CPU cores the pod requests, as a Kubernetes quantity | one of the two resources every `ClusterQueue` is denominated in; a data-loading-bound run starved of CPU leaves its accelerators idle, which is the most expensive way to waste the fleet  | required, quantity, `> 0`                           |
| `memory`            | host RAM the pod requests, as a Kubernetes quantity  | the other; host memory is where the dataset shards and the data loader live, and an OOM-killed pod loses the whole run back to its last checkpoint                                         | required, quantity, `> 0`                           |
| `accelerator.class` | the name of an `AcceleratorClass` this run wants     | the platform word that resolves to a vendor resource name, a device memory size and a Kueue flavor — all of which are the cluster's business and none of which belong in a user's manifest | optional as a whole; if set, must name a real class |
| `accelerator.count` | how many whole devices of that class                 | what Kueue counts against `nominalQuota`, and, multiplied by the class's `deviceMemory`, what decides whether the base model can fit at all                                                | integer ≥ 1, ≤ the class's `maxPerJob`              |

**Quantities, not numbers.** `cpu` and `memory` are `resource.Quantity`, the same type the core API
uses, so `8`, `"8"`, `8000m` and `32Gi` all mean what a Kubernetes user expects them to mean. The
cost is that `+kubebuilder:validation:Minimum` does not apply to a quantity — `controller-gen`
renders it as an int-or-string — so a lower bound is a CEL rule
(`self.cpu == quantity(self.cpu) && quantity(self.cpu).compareTo(quantity('0')) > 0`), not a marker.
That is the shape of rule
[validation-and-immutability.md](../reference/validation-and-immutability.md) calls CEL's natural
home: it concerns only the incoming object.

**No accelerator at all is a legal request.** The whole of level 1 runs on kind, where no node
advertises a device. Making `accelerator` optional is what lets the same CRD serve a laptop cluster
and a GPU fleet without an overlay, and it is also honest: a small fine-tune on CPU is a real thing
to want.

**Whole devices only.** `count` is an integer because an extended resource is an integer counter —
the kubelet cannot hand out half a card. Fractional sharing exists (MIG, time-slicing) but it is
expressed by _a different class_ whose devices are smaller, not by a fractional count. That keeps
the arithmetic Kueue does on quota the same arithmetic in every cluster.

**Requests, not limits.** The user states one number per resource and the controller decides where
it lands: for `cpu` and `memory` it becomes the pod's request, and for the accelerator it becomes
both request and limit, because Kubernetes requires those to be equal for an extended resource and
rejects the pod otherwise. Exposing a `requests`/`limits` pair would hand the user a knob whose only
legal setting for the field that matters is "both the same".

**What is not here, and why.** No `gpuType` or `nvidia.com/gpu` string: a vendor name in a
user-facing field is the thing `AcceleratorClass` exists to prevent. No `ephemeral-storage`: nothing
in the platform makes a decision from it yet, and a field that no decision depends on is a web form.
No node selector, toleration or topology preference: those describe _where_, which is Kueue's
`ResourceFlavor` and the scheduler's business, and a user who can write them can route around the
quota boundary.

Like the other four inputs, `resources` is immutable after creation. A run whose resource request
changed halfway through is not the run that was admitted, and Kueue has already done arithmetic
against the original.

### `TrainingJob` status

_Proposed, pending review — task 1.2's Spec beat._

The requirement this section exists to meet: a user reads their own `TrainingJob` and learns why
their run is waiting **without learning what Kueue is**. Kueue's own vocabulary leaks its
implementation — `QuotaReserved`, `ClusterQueue`, `ResourceFlavor` are words about the queueing
system, not about the user's job — and it is also not stable across a runtime swap, which is the
whole reason this CRD exists.

Three condition types, and no more:

| Condition  | True means                                              | Who owns a `False` |
| ---------- | ------------------------------------------------------- | ------------------ |
| `Rendered` | the platform produced the runtime object for this run   | the platform       |
| `Accepted` | the cluster has agreed to run it and the pods may start | varies — see below |
| `Finished` | the run reached a terminal state, successfully or not   | the training code  |

`Rendered=False` is always a platform bug or an API-server refusal: the user's spec was already
validated at admission, so nothing they wrote can land here. `Finished=False` is not a failure, it
is "still going".

`Accepted` is the translation, and the whole design question is how much of Kueue's reason set
survives it. Three platform reasons, because three is the number of _different next actions_ a user
can take:

| Platform reason         | From Kueue's reason                                                                           | What the user does      |
| ----------------------- | --------------------------------------------------------------------------------------------- | ----------------------- |
| `WaitingForCapacity`    | `WaitingForQuota`, `WaitingForPreemptedWorkloads`, `PendingEvaluation`, `WaitingForPodsReady` | nothing — it waits      |
| `ExceedsTeamLimit`      | `ExceedsMaxQuota`                                                                             | shrink the request      |
| `PlatformMisconfigured` | `Misconfigured`, `NoMatchingFlavor`, `UnrecordableResourceUsage`, `DRAResourcesUnresolved`    | nothing — file a ticket |

The merges are deliberate. `WaitingForQuota` and `WaitingForPreemptedWorkloads` are the same
sentence to a user — "the team's capacity is full, you are in line" — and the difference between
them is a fact about Kueue's scheduler, not about their job. The splits are equally deliberate:
`ExceedsMaxQuota` is the one case where waiting is futile, so collapsing it into
`WaitingForCapacity` would leave a user watching a job that can never start, and
`PlatformMisconfigured` must be separate because the action is "tell an operator" and the fault is
not theirs. A platform that blames a user for its own missing `ResourceFlavor` has lied.

Two rules the table does not state.

**An unrecognised reason is passed through, never dropped.** Kueue adds reasons across releases, and
a `switch` with no default is how a new one becomes a job that is silently `Pending` forever. An
unmapped reason maps to `PlatformMisconfigured` with Kueue's own reason and message quoted in the
condition message, because an unmapped reason _is_ a platform defect — the platform has met a state
it does not understand, and that is an operator's problem rather than the user's.

**Eviction flips `Accepted` back to `False`.** A run that was admitted and then preempted is waiting
again, so the condition is level-triggered like everything else: it reports what is true now, not
the high-water mark. The reason carries `Preempted`, which is the one Kueue reason that reaches the
user unmerged, because it is the only one that explains a run that was running and now is not.

| Field              | What it is                                        | Why the platform needs it                                                  |
| ------------------ | ------------------------------------------------- | -------------------------------------------------------------------------- |
| `conditions`       | the three types above                             | the source of truth for diagnosis, and what the 1.4 gate consumes          |
| `phase`            | `Pending`, `Running`, `Success` or `Failed`       | a one-word `kubectl get` summary; never the source of truth                |
| `jobRef`           | the name of the child Job                         | the one pointer from platform vocabulary down to the runtime object        |
| `lastResolvedTime` | when the controller last read the admission state | tells a reader whether `Pending` is current or stale from before an outage |

`phase` is derived from the conditions on every pass and never written independently: `Pending`
until `Accepted`, `Running` once accepted and not finished, then `Success` or `Failed`. It exists
for `kubectl get` and for humans, and the rule that keeps it honest is that deleting it would lose
no information.

`jobRef` is a name, not an `ObjectReference`: the kind is fixed until task 5.2 swaps the runtime,
and when it does, a name is the field that survives — which is the same argument as `spec.dataset`.

---

## `ModelVersion`

A registered artifact and its position in the release funnel. Designed, not built — task 1.3.

The funnel's states and legal transitions are in [architecture.md](architecture.md), and they are
carried in conditions rather than one `phase` string so that "evaluated but blocked" and "promotable
but not yet canaried" are both expressible.

What is already decided: status carries the metrics, the gate results, the source `TrainingJob` and
the `Dataset` that produced it — the lineage chain from serving traffic back to the rows that made
the weights. Lineage lives in `status` rather than `spec` because it is a record of what a
controller observed, not a claim a user makes.

The field list is task 1.3's to design. The criteria it has to meet: the lineage must survive the
deletion of what it points at (a dangling name still names something; a dangling pointer does not),
and a promotion decision must be reconstructible from the object alone months later.

---

## `Evaluation`

Scores a `ModelVersion` against a golden `Dataset`. Designed, not built — task 1.4.

Kueue-admitted for a reason worth stating: evaluation competes for the same GPUs as training, so
running it as an in-process check inside a controller would let it bypass the quota every other
workload respects.

What is already decided: on completion it writes metrics into the `ModelVersion` status and moves it
to `PromotionReady` or `PromotionBlocked`, with a reason either way. The distinction the design
turns on is **infrastructure failure versus model-quality failure** — an evaluation whose Pod was
evicted has not told you anything about the model, and must not read as a failed gate.

---

## `Release`

The serving intent for a logical model: which version is stable, which is canary, at what split.
Designed, not built — task 1.5. The controller renders it into a KServe `InferenceService`.

The property that matters more than any field: application code addresses the `Release`, never a
version. A rollback is then a change to this one object — no application deploy, no image rebuild,
no coordination with the team that calls the endpoint.

---

## `AcceleratorClass`

A kind of hardware the platform supports, registered by an infrastructure engineer. Cluster-scoped,
and the only object in this dictionary no `TrainingJob` author writes. Designed, not built — the
field shape lands with [5.1](../curriculum/training-tasks.md#51-make-the-gpu-request-real-on-kind),
and the reasoning for it being an object at all is in
[architecture.md](architecture.md#accelerators-are-a-registry-not-a-vocabulary).

| Field          | What it is                                                      | Why the platform needs it                                                                                                                                               | Constraints                |
| -------------- | --------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------- |
| `resourceName` | the extended resource the kubelet advertises for this hardware  | the string the controller writes into the pod template, and the resource a `ClusterQueue` must cover before a job asking for this class can ever be admitted            | required, a qualified name |
| `deviceMemory` | one device's memory                                             | lets the platform refuse "this base model cannot fit in `count` × `deviceMemory`" at submission, instead of after an hour in the queue and an OOM inside the allocation | required, quantity         |
| `flavor`       | the Kueue `ResourceFlavor` this class's quota is denominated in | the cross-check that the class and the quota agree; a class whose `resourceName` the flavor's `ClusterQueue` does not cover describes jobs that queue forever           | required, must exist       |
| `maxPerJob`    | the largest `count` a single run may ask for                    | a guardrail that is a refusal at admission rather than a postmortem about the run that took the whole fleet                                                             | optional, integer ≥ 1      |
| `vendor`       | who makes the hardware                                          | metrics and dashboard grouping, nothing else — it is deliberately inert, so that no controller can be tempted to branch on it                                           | optional, label only       |

The fields that are **not** here matter as much: node labels, tolerations and topology live on
Kueue's `ResourceFlavor`, which already has them and already injects them on admission. Restating
them here would be two sources of truth for one fact, and would make this repository a translator
for fields it does not own.

`status` carries conditions and the observed allocatable count, so a class naming hardware that no
node in this cluster advertises reads as `Available: False` with a reason, rather than as jobs that
silently never start.
