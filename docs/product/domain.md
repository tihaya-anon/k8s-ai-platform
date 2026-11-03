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
| `uri`      | where the dataset's bytes actually live                     | the only way to reach the artifact; also the identity a drift check re-resolves                                                                  | required, 1–128 chars |
| `owner`    | the human or team accountable for it                        | incident routing and accountability — a failing dataset needs someone to wake, and "the platform" is not a someone                               | required, 1–128 chars |
| `rows`     | how many records the registrant says it has                 | a claim to check the artifact against, so a truncated upload is caught at registration rather than by a model that trains on a tenth of the data | optional, minimum 1   |
| `checksum` | the artifact digest the registrant says it has              | the other half of the same check, and the one that catches a silent rewrite rather than a truncation                                             | optional, 1–256 chars |
| `golden`   | whether this dataset is approved as an evaluation yardstick | makes the dataset a policy input: an `Evaluation` scores only against golden data, so flipping this bit changes what the promotion gate means    | defaults to `false`   |

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

| Field               | What it is                                                           | Why the platform needs it                                                                                                                                                                  | Constraints                         |
| ------------------- | -------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ----------------------------------- |
| the `Dataset`       | the registered dataset this run trains on, **by name**               | the first lineage edge, and an admission check: a job pointing at a missing or non-`Ready` dataset is refused at submission rather than discovered as a crash loop inside a GPU allocation | a reference, never an embedded copy |
| the base model      | the trained weights this run starts from                             | the second lineage edge and its own licence; also the dominant term in GPU memory, so it decides whether the resource request below can ever fit                                           | required for fine-tuning            |
| the image           | the container holding the training code, framework and GPU libraries | the code version, and the third lineage edge                                                                                                                                               | pin by digest, not tag              |
| GPU and CPU request | what the Pod asks the scheduler for                                  | the only number Kueue can do arithmetic on against `nominalQuota`, so it has to land on the Pod template where Kueue reads it                                                              | must reach the Pod template         |
| the queue           | the `LocalQueue` this run belongs to                                 | where the job meets the team boundary and the fairness policy — whose quota it spends and who preempts it                                                                                  | defaulted per namespace             |

Two rules the field list does not state. **A reference, not a copy**: embedding a `Dataset` object
into the spec snapshots it, so the copy cannot be checked for `Ready`, goes stale the moment the
real object changes, and drags the whole `DatasetSpec` schema into this CRD. **Pin the image by
digest**: a tag can be repointed, and two `TrainingJob`s with byte-identical specs producing
different models is the failure that makes an audit trail worthless.

What must _not_ be in the spec: replica counts, framework-specific knobs, gang-scheduling settings,
`suspend`. Those describe how a runtime executes the job, and the spec has to survive the swap from
a `batch/v1` Job to a `JobSet` or a Kubeflow `TrainJob` untouched. The moment it names a framework,
the swap breaks and the CRD was a YAML translator after all.

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
