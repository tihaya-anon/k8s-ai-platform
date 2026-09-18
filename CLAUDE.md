# Repository guide

## What this repository is

An internal AI model operations platform on Kubernetes — and a training environment. The owner of
this repository is learning to **maintain** a platform like this, so the code exists to be extended,
broken, and operated, not just to run.

- `docs/draft.md` is the product source of truth (user stories and required behaviour). Treat it as
  the requirements document; implementation follows it, not the other way around.
- `docs/mvp.md` states what is built and what is deliberately missing.
- `docs/training-tasks.md` holds the owner's exercises. **The gaps listed there are intentional.**
  Do not implement a task from that list unless explicitly asked — doing so removes the exercise.
- `docs/progress.md` is the owner's maintainer log.
- `docs/runbook.md` is how the platform is operated.

## How to help here

The owner is the maintainer; prefer answers that build their understanding over answers that just
produce code.

- When asked to explain something, explain the mechanism and where to see it in the running system,
  not only the code path.
- When asked to implement something, follow the conventions below and add tests in the same change.
- When reviewing their work, use the checklist at the end of `docs/training-tasks.md`.
- If a request would complete one of the training tasks, say so first and ask whether they want it
  done for them or want to be guided through it.

## Conventions

- **Standard library everywhere except the control-plane API surface.** Kubernetes access
  (`internal/k8s`), the gateway, the workers, and metrics stay dependency-free so the mechanics
  remain visible. The exception is `internal/api`, which uses Huma over `net/http` for declarative
  validation, spec generation, and the Swagger UI docs page it serves at `/docs`. Huma is the
  module's only requirement and pulls in nothing else. Ask before adding any further dependency.
- **The toolchain is Go 1.25**, installed under `~/.local/go` and ahead of the distro's Go on PATH.
  `go.mod` declares `go 1.25.0`; keep the tree buildable with the toolchain that is actually
  installed rather than relying on `GOTOOLCHAIN` to fetch another one mid-build.
- **The OpenAPI document is generated from the handler types, and the snapshot is part of the
  change.** An endpoint's shape, validation, and documentation all come from the input and output
  structs passed to `huma.Register`, so there is no second place to edit. `internal/api/openapi.yaml`
  is a committed snapshot of that document: run `make openapi` after changing an endpoint and review
  the diff. `TestSpecSnapshotIsCurrent` fails otherwise.
- One binary, `cmd/platform`, with a role as the first argument: `control-plane`, `gateway`,
  `serve`, `train`, `eval`, `ctl`.
- Every platform object has an owner, a status, timestamps, and a failure reason. Anything new must
  too.
- Every state transition emits an event. The event log is the incident timeline.
- Kubernetes writes are server-side applies; reconciling twice must equal reconciling once.
- Errors returned to callers name the layer at fault (`client`, `gateway`, `model-server`).
- Comments explain _why_ a constraint exists, not what the line does.
- Run `make fmt vet test` before saying a change is done.

## Layout

```text
cmd/platform          every process, selected by the first argument
internal/model        platform objects and the promotion gates
internal/store        JSON-file metadata database
internal/api          control-plane REST API
internal/controller   reconcile loop and the Kubernetes manifest builders
internal/gateway      online inference routing, canary split, per-version metrics
internal/serve        model server
internal/worker       training and evaluation job entrypoints
internal/ctl          CLI over the API
deploy/               kind cluster config and manifests
scripts/demo.sh       the full product loop
```
