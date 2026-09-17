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

**Goal:** stand up the MVP described in [mvp.md](mvp.md) from the requirements in
[draft.md](draft.md).

**What I changed:** initial implementation — control plane API and reconcile loop, gateway, model
server, training and evaluation workers, CLI, kind cluster, manifests, unit tests.

**How I proved it:** `make test` passes; `make up && make demo` runs the loop from dataset
registration to a promoted stable version.

**What surprised me:** the release path is not the hard part — the gate that refuses an unevaluated
model is what separates a platform from a deploy script, and it is four lines of code in one
handler.

**Left undone:** everything in [training-tasks.md](training-tasks.md). That is the point.
