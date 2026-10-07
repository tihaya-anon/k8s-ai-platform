# Repository guide

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
lineage, and promotion gate. See [docs/architecture.md](docs/architecture.md).

- `docs/requirements.md` is the product source of truth.
- `docs/architecture.md` is the design.
- `docs/training-tasks.md` is the blank-start curriculum. Read its "How a task runs now" section
  before starting any task.
- `docs/progress.md` is the maintainer log.

## How to help here

The owner is an experienced backend engineer learning the Kubernetes platform side. Prefer answers
that build understanding over answers that just produce code. Good analogies: `controller-runtime`
is the IoC container, Kubebuilder markers are annotations, reconcilers are the service layer.

- Explain the mechanism and where to see it in the running cluster, not only the code path.
- The owner now builds the scaffold, scripts, and first implementation. Prefer guidance, review,
  debugging, and small examples over pasting complete task solutions.
- When a task involves generated files, make the owner identify which tool generated each file:
  Kubebuilder, `controller-gen`, `kustomize`, or a handwritten script.
- When reviewing work, use the checklist at the end of `docs/training-tasks.md`.
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

## Current layout

The repository is intentionally pre-scaffold:

```text
README.md
CLAUDE.md
docs/
.markdownlint.json
.prettierrc
.prettierignore
.gitignore
```

Expected later, created by tasks rather than preloaded:

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
