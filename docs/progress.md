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

**Quiz answers to revisit:** only the answers I missed or guessed.

**Left undone:** anything I knowingly skipped, and why.
```

## Entries

### Archive before blank rebuild — 2026-10-07

**Goal:** preserve the generated scaffold before resetting the curriculum to start from blank.

**What I changed:** committed the previous state as `131697f` with message
`Archive generated kubebuilder scaffold before blank rebuild`, then removed the generated
Kubebuilder project, generated manifests, controller/test scaffolds, cluster scripts, local workflow
scripts, editor files, and CI files.

**How I proved it:** `git status --short` was clean after the archive commit; the follow-up deletion
leaves this repository as documentation plus project policy.

**What surprised me:** the generated project is not just `api/` and CRDs. `Makefile`, `PROJECT`,
`go.mod`, `cmd/main.go`, `config/`, RBAC, samples, tests, cluster scripts, and workflow helpers all
carry assumptions. Starting from blank makes those assumptions visible.

**Quiz answers to revisit:** none yet.

**Left undone:** begin task 0.0 by installing/verifying the CLI tools.
