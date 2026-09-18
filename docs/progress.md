# Maintainer Log

Your own log for [training-tasks.md](training-tasks.md). Keep it in the repository so the work is
portable: someone reading this file should be able to see what was built, what was learned, and what
is next.

One entry per task. Keep them short; the value is in the evidence and the surprises.

## Template

```markdown
### <task id> <task name> — <date>

**Goal:** one sentence.

**What I changed:** files, endpoints, objects.

**How I proved it:** the commands and the output that show it works, including the failure case.

**What surprised me:** the thing you would tell the next maintainer.

**Left undone:** anything you knowingly skipped, and why.
```

## Entries

### 0.0 Platform handover — 2026-09-17

> Archived. This entry describes the hand-written first version, replaced by the rebuild described
> in [architecture.md](architecture.md). The code and its `mvp.md` are in git history at `3cb8302`.

**Goal:** stand up the hand-written MVP from the requirements in [draft.md](draft.md).

**What I changed:** initial implementation — control plane API and reconcile loop, gateway, model
server, training and evaluation workers, CLI, kind cluster, manifests, unit tests.

**How I proved it:** `make test` passes; `make up && make demo` runs the loop from dataset
registration to a promoted stable version.

**What surprised me:** the release path is not the hard part — the gate that refuses an unevaluated
model is what separates a platform from a deploy script, and it is four lines of code in one
handler.

**Left undone:** everything in [training-tasks.md](training-tasks.md). That is the point.

### 0.0b Rebuild decision — 2026-09-18

**Goal:** stop hand-writing what the ecosystem already does, and find out where the platform
engineer's actual work is.

**What I changed:** discarded the v1 implementation. New direction in
[architecture.md](architecture.md): Kueue for queue and quota, KServe for serving and traffic,
custom resources and controllers for the domain, and the promotion gate as an admission webhook
rather than an `if` in an HTTP handler.

**What surprised me:** v1 accepted a `gpus` field on every training job, stored it, and never read
it. No resource request, no node selector, nothing. The API described a cluster that did not exist.

**Left undone:** all of [training-tasks.md](training-tasks.md).
