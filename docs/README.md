# Documentation map

Five kinds of document, kept apart because they answer different questions and change at different
rates.

| Directory     | Answers                                 | Changes when                               |
| ------------- | --------------------------------------- | ------------------------------------------ |
| `product/`    | what the platform is for, and its shape | the product or the design changes          |
| `curriculum/` | what to build next, and how to learn it | a task is rewritten or a new one is taught |
| `log/`        | what was built, and what was learned    | after every task                           |
| `tooling/`    | how the development environment works   | the environment changes                    |
| `reference/`  | how Kubernetes itself works             | rarely; it is background, not a task       |

## Read in this order

1. [product/requirements.md](product/requirements.md) — the product source of truth: users, stories,
   and required platform behavior.
2. [product/architecture.md](product/architecture.md) — the design that follows from it. What this
   repository owns, what Kueue and KServe own, and the five custom resources.
3. [product/domain.md](product/domain.md) — the dictionary: every domain term and every field, what
   it means and which platform decision depends on it. Look a word up here rather than reading it
   end to end; it exists so that nothing else has to explain "golden dataset" twice.
4. [curriculum/training-tasks.md](curriculum/training-tasks.md) — the task list, built from a blank
   repository. Start with "How a task runs now".
5. [log/progress.md](log/progress.md) — one entry per task: what changed, how it was proved, the
   quiz, and the review.

If a task assumes a Kubernetes mechanism you have not met yet, look in `reference/` first.

## Kubernetes background

Platform-independent background, safe to read at any point and not tied to a task.

- [reference/cluster-components.md](reference/cluster-components.md) — the control plane, the node
  components, and the two rules that shape every convention here: only the API server talks to
  `etcd`, and controllers only meet in the API.
- [reference/api-machinery.md](reference/api-machinery.md) — groups, versions, kinds and resources;
  the `get`/`list`/`watch` verbs; `resourceVersion`, pagination and bookmarks; how list-watch
  becomes an informer cache and a workqueue.
- [reference/validation-and-immutability.md](reference/validation-and-immutability.md) — the four
  places a rule can live (schema markers, CEL, webhook, controller), which one to reach for, the
  `field.ErrorList` conventions, and why a drift check and an immutability check cannot be one
  function.
- [reference/objects-and-controllers.md](reference/objects-and-controllers.md) — `spec` versus
  `status`, owner references, finalizers and conditions; the admission path a write takes; the
  level-triggered reconcile loop.

## Deep-taught tasks

A task that needs more room than the list allows gets its own file in `curriculum/`, named for its
task number. Each one is in two halves: a **Learn** half you read and type, and a **You build** half
left unimplemented on purpose.

- [curriculum/0.5-tls-and-admission.md](curriculum/0.5-tls-and-admission.md) — tasks 0.5a and 0.5b:
  certificates from an openssl lab to the cluster's own PKI, then the admission webhooks already
  running in it.
- [curriculum/1.1-dataset-reconciler.md](curriculum/1.1-dataset-reconciler.md) — task 1.1: the
  `Dataset` reconciler typed by hand, then drift detection and the immutability webhook left to
  build.
- [curriculum/1.2-Kueue-and-jobs.md](curriculum/1.2-kueue-and-jobs.md) — task 1.2: how Kueue admits
  a suspended Job, read from the live cluster, then the `TrainingJob` controller left to build.

## Tooling

- [tooling/cluster-providers.md](tooling/cluster-providers.md) — the cluster scripts on kind or on
  the host's k3s: how the provider is chosen, and what differs between the two.
- [tooling/agent-container.md](tooling/agent-container.md) — the optional development container, how
  it is built, and what it mounts.
- [tooling/dashboards.md](tooling/dashboards.md) — k9s, Headlamp and the opt-in Grafana stack: what
  each one answers, and why the console authenticates as a ServiceAccount token.
