# Repository guide

## What this repository is

An internal AI model operations platform on Kubernetes — and a training environment. The owner of
this repository is learning to **build and maintain** a platform like this, so the code exists to be
extended, broken, and operated, not just to run.

It is being rebuilt. The first version implemented everything by hand: its own Kubernetes REST
client, its own queue, its own traffic split, no CRDs. That version is archived in git history at
`3cb8302` and is worth reading as a reference; it is not the direction.

The direction is **compose the ecosystem, own the policy**. Kueue does queueing and quota, KServe
does serving and traffic, and this repository contributes the custom resources, the controllers, the
lineage, and the promotion gate. See [docs/architecture.md](docs/architecture.md) for where that
line falls and why.

- `docs/requirements.md` is the product source of truth (user stories and required behaviour). Treat
  it as the requirements document; implementation follows it, not the other way around.
- `docs/architecture.md` is the design: what is built here, what is adopted, and the reasoning.
- `docs/training-tasks.md` holds the owner's curriculum. **You write the main implementation; the
  owner reads it, answers the quiz, and does the patch job.** Read that file's "How a task runs"
  before starting any task in it.
- `docs/progress.md` is the owner's maintainer log.

## How to help here

The owner is an experienced backend engineer (Java, Spring Boot) learning the Kubernetes platform
side. Prefer answers that build their understanding over answers that just produce code, and lean on
the analogy where it is honest: `controller-runtime` is the IoC container, kubebuilder markers are
the annotations, reconcilers are the service layer.

- When asked to explain something, explain the mechanism and where to see it in the running cluster,
  not only the code path.
- When asked to implement something, follow the conventions below and add tests in the same change.
- When reviewing their work, use the checklist at the end of `docs/training-tasks.md`.
- When you finish a task from `docs/training-tasks.md`, hand over three things together: the code,
  the quiz, and the patch job. Do not move to the next task until the quiz is answered.
- **Never hand over the answer to a patch job, even when asked twice.** Explain the mechanism, name
  the file, refuse the diff. That refusal is the only part of the curriculum that is load-bearing.

## Conventions

- **Adopt before you build.** Before writing a controller, establish that no CNCF project already
  does it. If a reconciler only copies fields from one CRD into another, it is a YAML translator —
  delete it and let users write the other resource directly. A platform object earns its existence
  by carrying policy, lineage, or a stable interface across a runtime swap.
- **Kubebuilder conventions, unaltered.** Standard layout, standard `make` targets, markers for
  validation and RBAC rather than code. Deviating costs every tool that assumes the standard.
- **Level-triggered reconcilers.** Read the world, make it match the spec, never assume you saw the
  previous event. Reconciling twice equals reconciling once.
- **Status is conditions** — `type`, `status`, `reason`, `message`, `observedGeneration`,
  `lastTransitionTime` — and a user diagnoses from `kubectl describe` without reading logs.
- **Every child object carries an `ownerReference`.** Finalizers only for cleanup Kubernetes cannot
  do, and each one needs a documented removal path for when its controller is down.
- **Every state transition emits an Event.** The event log is the incident timeline.
- **Never mutate another operator's resources beyond the fields you own.** Set yours, read their
  conditions, do not fight their controller.
- **Errors name the layer at fault.** "Your quota is exhausted" must be distinguishable from "KServe
  could not pull the image" and from "your model failed its gates".
- **No `if kind` in Go.** Cluster differences live in kustomize overlays under
  `config/overlays/{kind,cloud}`. Ask before adding a dependency, a CRD, or a webhook.
- **The toolchain is Go 1.25**, installed under `~/.local/go` and ahead of the distro's Go on PATH.
  Keep the tree buildable with the toolchain that is actually installed rather than relying on
  `GOTOOLCHAIN` to fetch another one mid-build.
- Comments explain _why_ a constraint exists, not what the line does.
- **Diagrams are Mermaid, not ASCII art.** A mermaid fence renders on GitHub, survives reflow by
  Prettier, and can be diffed line by line; box drawing cannot. Directory listings stay in a text
  fence — a file tree is a listing, not a diagram.
- **A diagram reads in the column it sits in.** Prose here is 100 columns; a diagram wider than it
  is tall shrinks to an illegible strip in that space. Prefer `flowchart TB` over `LR`, break long
  node labels with `<br/>`, and keep the rendered aspect ratio under roughly 1:1. Render with
  `mmdc -i x.mmd -o x.png` and check the pixel dimensions before committing — a six-to-one strip
  looks fine in the source and is unreadable on the page. `mmdc` is also the only parse check there
  is: a malformed fence renders as a blank box, and neither Prettier nor markdownlint looks inside
  it.
- **Formatting is automatic, linting is a gate.** `hack/format.sh` is the only entry point; it
  formats Markdown, JSON, YAML and Go in place. A Claude Code `PostToolUse` hook runs it per edit,
  and `hack/git-hooks/pre-commit` runs it on the staged set and then blocks the commit on
  `markdownlint-cli2` and `gofmt -l`. A formatter can fix whitespace but cannot decide whether a
  heading skips a level, so structure blocks and whitespace does not.
- **One owner per formatting concern.** Prettier owns line length and wrapping (`MD013` is therefore
  off), markdownlint owns structure, and the rules they both have an opinion on — list markers,
  emphasis, fence style — are pinned in `.markdownlint.json` to what Prettier emits. Change one side
  without the other and the two tools undo each other on every save.
- Run `make fmt vet test` before saying a change is done. `go vet` is slow on a cold cache here
  (WSL2); that is not a hang.

## Layout

Scaffolded by kubebuilder at task 0.2; API group `ml.aiplatform.io`, module
`github.com/tihaya-anon/k8s-ai-platform`.

```text
api/v1alpha1          CRD types and kubebuilder markers
internal/controller   one reconciler per kind, plus the envtest suite
cmd/main.go           the manager entrypoint
config/               kustomize: crd, rbac, manager, samples, prometheus
Makefile              kubebuilder's; `make manifests generate fmt vet test`
PROJECT               kubebuilder's registry of scaffolded kinds
hack/set-up-cluster.sh  kind + cert-manager + KServe + Kueue, pinned and idempotent
hack/format.sh        the one formatter entry point, shared by both hooks
hack/git-hooks/       pre-commit; enable with `git config core.hooksPath hack/git-hooks`
hack/boilerplate.go.txt  licence header controller-gen stamps on generated files
hack/agent-home/      shell config for host and container; the nvim config is host-only
docs/                 requirements, architecture, training-tasks, progress, agent-container, editors
deps/                 gitignored; pinned upstream checkouts read as reference (KServe v0.20.0)
.claude/settings.json the format-on-edit hooks
.zed/                 Zed project settings and tasks; see docs/editors.md
.markdownlint.json    markdown rules, relaxed where they fought Prettier or the docs
.prettierrc           width 100, prose wrapped, code inside fences left alone
.prettierignore       generated manifests are owned by controller-gen, not by Prettier
agent.Dockerfile      development container image
agent.Makefile        development container; kept out of Makefile so kubebuilder owns that
```

Not yet created, and expected as the levels land:

```text
internal/webhook      the promotion gate and the immutability checks
internal/artifact     artifact store interface: PVC now, object store later
config/overlays/      kind and cloud
test/e2e/             the loop, against a live cluster
```
