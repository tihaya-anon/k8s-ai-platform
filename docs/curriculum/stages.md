# Walking a task through its stages

The review-format curriculum keeps each beat of a task as its own commit on the `main` branch. A few
of those commits are tagged, so you can check one out and read the tree in your editor. The point is
that **Spec** is reviewable before the code exists.

**You should not need this file to run a task.** Every beat in
[training-tasks.md](training-tasks.md) carries the commands it needs, inline, ready to paste. This
is the reference for when you want to go off that path: to see how the tags are laid out, to put
your own work somewhere, or to get back.

`backlog` is the tip of the history from before the review format. It is kept as a marker, not as a
line of work. Every commit on it is also on `main`, so there is nothing on `backlog` to go back for.

## The tags

Every tag is `task/<number>/<beat>`:

| Tag                    | Check it out to see                                        |
| ---------------------- | ---------------------------------------------------------- |
| `task/1.2/spec`        | the `TrainingJob` status proposal, before any code existed |
| `task/1.1/variant-cel` | `Dataset` immutability as CEL rules instead of the webhook |

`main` carries everything built so far, so most beats need no checkout at all. These two tags exist
because they are the states `main` is **not** in.

One more tag marks a commit rather than a place to stand: `task/1.2/build` is where the 1.2
implementation landed. `main` has it plus later documentation, so read the code on `main`.

`git tag --list 'task/*'` is the current list; it grows as tasks are built.

## Standing at a stage

```bash
git checkout task/1.2/spec    # read this tree in your editor
git checkout main             # back to everything
```

Git will say "You are in 'detached HEAD' state". That is the right state for reading. Nothing is
wrong. A tag is a place to read from, not a branch to commit on.

If the checkout is refused, you have uncommitted work. Put it down first:

```bash
git stash            # ... read, then: git checkout main && git stash pop
```

The commit message on each tag argues the design. If you want it without leaving your editor, it is
also in the GitHub commit view. `git log -1 task/1.2/spec` prints it in the terminal.

## Doing your own half

The **Verify** beat is yours. Put it on your own branch, cut from `main`:

```bash
git checkout -b mine/1.2-tests main
# write the cases listed at the foot of internal/controller/trainingjob_status_test.go
make fmt vet test
```

Keeping it on `mine/...` leaves the tags alone as references. When you want it in the trunk:

```bash
git checkout main
git merge --no-ff mine/1.2-tests
```

## Comparing two variants

Task 1.1's two are mutually exclusive. You would ship one, not both. So `main` holds one and a tag
holds the other. Read each tree in turn:

```bash
git checkout task/1.1/variant-cel
make fmt vet test lint         # it passes too; that is the point
# read api/v1alpha1/dataset_types.go and internal/webhook/v1alpha1/
git checkout main
# read the same files here
```

Task 1.2's three are not mutually exclusive, so they sit side by side in one file. The contract test
runs all three against the same fixtures, which is what makes that comparison a design question and
not a correctness one:

```bash
go test ./internal/controller/ -run TestFinders -v      # all three agree
go test ./internal/controller/ -run TestFramework -v    # the two places they do not
```

To try a verdict, change the one line in `cmd/main.go` that names the finder, then
`make fmt vet test lint`.

If your verdict is the lightest one, confirm the claim rather than trusting it:

```bash
git checkout -b mine/1.2-drop-jobframework main
# delete OwnerIndexFinder and FrameworkFinder from internal/controller/workload_finder.go
go mod tidy && git diff --stat go.mod
```

That diff is the argument, in a form nobody can talk you out of.

## Proving a test fails for the right reason

A test that passes is not evidence until you have watched it fail. Break the code on a scratch
branch, confirm your case goes red, then throw the branch away:

```bash
git checkout -b scratch/break-it main
# invert one arm of kueueReasonToPlatform in internal/controller/trainingjob_status.go
go test ./internal/controller/ -run TestAccepted
git restore . && git checkout main && git branch -D scratch/break-it
```

## If you get lost

```bash
git checkout main    # back to everything
git status           # what you have in flight
git stash list       # what you put down earlier
```
