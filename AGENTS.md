# Agent guidance

`AGENTS.md` is the source of truth for agent guidance. `CLAUDE.md` is an ignored, generated copy for
Claude; always edit `AGENTS.md`, never `CLAUDE.md`, even when reading this guidance from
`CLAUDE.md`. The pre-commit hook refreshes `CLAUDE.md` after formatting, including on commits with
no staged guidance changes. Enable hooks in a fresh clone with
`git config core.hooksPath hack/git-hooks`; to refresh the copy before committing (or after a
checkout), run `cp AGENTS.md CLAUDE.md`.

## Training-task reviews

When reviewing an entry in `docs/log/progress.md`, treat the `Quiz answers` section as the learner's
answer being graded. The assistant is the critic: assess each answer against the questions in
`docs/curriculum/training-tasks.md`, explain corrections briefly, and identify any unanswered or
misunderstood question.

Do not ask the learner to refine or add to their answer merely because the implementation is
incomplete. Only request an answer revision when the learner misunderstood a question or omitted one
of its required parts. Keep implementation gaps separate from quiz-answer feedback, and mention them
as review observations rather than assigning an unnecessary refinement exercise.

## What this repository is

An internal AI model operations platform on Kubernetes — and a training environment. The owner is
learning to build and maintain the platform, so the curriculum now starts from a deliberately blank
repository.

Two reference points exist in git history:

- `3cb8302` — the old handwritten platform: custom Kubernetes REST client, custom queue, custom
  traffic split, no CRDs.
- `131697f` — the generated Kubebuilder scaffold and first `Dataset` CRD that existed before the
  blank-start reset.

Use those commits as references only after the owner has tried the current task from scratch.

The direction remains **compose the ecosystem, own the policy**. Kueue does queueing and quota,
KServe does serving and traffic, and this repository contributes the custom resources, controllers,
lineage, and promotion gate. See [docs/product/architecture.md](docs/product/architecture.md).

`docs/` is split by purpose, and [docs/README.md](docs/README.md) is its index:

- `docs/product/` — `requirements.md` is the product source of truth, `architecture.md` is the
  design.
- `docs/curriculum/` — `training-tasks.md` is the blank-start curriculum; read its "How a task runs
  now" section before starting any task. Alongside it, one file per task that needs more room than
  the list allows, named for its task number: `0.5-tls-and-admission.md`,
  `1.1-dataset-reconciler.md`, `1.2-kueue-and-jobs.md`. A task with such a file keeps only a summary
  and a link in `training-tasks.md`.
- `docs/log/progress.md` — the maintainer log, one entry per task.
- `docs/tooling/` — `agent-container.md` and anything else about the development environment rather
  than the platform.

Every task has two halves: **Learn** (read the mechanism, inspect it running, no files change) and
**You build** (the feature). Keep them apart when writing or reviewing a task — an unfinished
implementation is not a reason to send the owner back to the Learn half.

## How to help here

The owner is an experienced backend engineer learning the Kubernetes platform side. Prefer answers
that build understanding over answers that just produce code. Good analogies: `controller-runtime`
is the IoC container, Kubebuilder markers are annotations, reconcilers are the service layer.

- Explain the mechanism and where to see it in the running cluster, not only the code path.
- The owner now builds the scaffold, scripts, and first implementation. Prefer guidance, review,
  debugging, and small examples over pasting complete task solutions.
- One exception, by the owner's decision: task 1.1 part one in
  `docs/curriculum/1.1-dataset-reconciler.md` is a taught walkthrough. Showing and explaining that
  code is the point. The exception stops at part two — drift detection and the immutability webhook
  are the owner's to write, and the usual refusal applies there.
- When a task involves generated files, make the owner identify which tool generated each file:
  Kubebuilder, `controller-gen`, `kustomize`, or a handwritten script.
- When reviewing work, use the checklist at the end of `docs/curriculum/training-tasks.md`.
- Do not move to the next task until the quiz is answered and missed answers are recorded.
- Never hand over the answer to a patch/refine step when the task is meant to teach the mechanism.
  Explain the mechanism, name the file, and refuse the diff.

## Conventions

- **Adopt before you build.** A platform object earns its existence by carrying policy, lineage, or
  a stable interface across a runtime swap.
- **Kubebuilder conventions, unaltered.** Standard layout, standard `make` targets, markers for
  validation and RBAC rather than code.
- **Level-triggered reconcilers.** Read the world, make it match the spec, never assume you saw the
  previous event.
- **Status is conditions** — `type`, `status`, `reason`, `message`, `observedGeneration`,
  `lastTransitionTime`.
- **Every child object carries an `ownerReference`.**
- **Every state transition emits an Event.**
- **Never mutate another operator's resources beyond the fields you own.**
- **Errors name the layer at fault.** Distinguish quota exhaustion, KServe failures, and failed
  model gates.
- **No `if kind` in Go.** Cluster differences live in kustomize overlays under
  `config/overlays/{kind,cloud}` once those overlays exist.
- **The toolchain is Go 1.25**, installed under `~/.local/go` and ahead of the distro Go on `PATH`.
- Comments explain _why_ a constraint exists, not what the line does.
- Diagrams are Mermaid, not ASCII art.
- Once workflow helpers exist, keep one owner per formatting concern: Prettier for wrapping,
  markdownlint for Markdown structure, `gofmt` for Go.
- Once the Kubebuilder project exists, run the local gate before calling a change done:
  `make fmt vet test`.

## Repository layout

Repository guidance and documentation:

```text
README.md
AGENTS.md             canonical agent guidance
CLAUDE.md             generated, gitignored copy
docs/README.md        index: what each document is for
docs/product/         requirements.md, architecture.md
docs/curriculum/      training-tasks.md and one file per deep-taught task
docs/log/             progress.md
docs/tooling/         agent-container.md
.markdownlint.json
.prettierrc
.prettierignore
.gitignore
```

The training tasks build out the standard Kubebuilder layout:

```text
Makefile              Kubebuilder-generated, then adjusted only with understanding
PROJECT               Kubebuilder project registry
api/v1alpha1          CRD types and Kubebuilder markers
cmd/main.go           manager entrypoint
config/               kustomize, CRDs, RBAC, manager, samples
internal/controller   reconcilers and envtest suite
internal/webhook      admission webhooks
internal/artifact     artifact store interface
hack/                 scripts after the manual commands are understood
deps/                 gitignored upstream reference checkouts
test/e2e/             live-cluster end-to-end tests
```
