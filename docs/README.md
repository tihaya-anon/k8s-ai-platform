# Documentation map

Four kinds of document, kept apart because they answer different questions and change at different
rates.

| Directory     | Answers                                 | Changes when                               |
| ------------- | --------------------------------------- | ------------------------------------------ |
| `product/`    | what the platform is for, and its shape | the product or the design changes          |
| `curriculum/` | what to build next, and how to learn it | a task is rewritten or a new one is taught |
| `log/`        | what was built, and what was learned    | after every task                           |
| `tooling/`    | how the development environment works   | the environment changes                    |

## Read in this order

1. [product/requirements.md](product/requirements.md) — the product source of truth: users, stories,
   and required platform behavior.
2. [product/architecture.md](product/architecture.md) — the design that follows from it. What this
   repository owns, what Kueue and KServe own, and the five custom resources.
3. [curriculum/training-tasks.md](curriculum/training-tasks.md) — the task list, built from a blank
   repository. Start with "How a task runs now".
4. [log/progress.md](log/progress.md) — one entry per task: what changed, how it was proved, the
   quiz, and the review.

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

- [tooling/agent-container.md](tooling/agent-container.md) — the optional development container, how
  it is built, and what it mounts.
