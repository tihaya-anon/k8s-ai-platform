# Walking a task through its stages

The review-format curriculum keeps each beat of a task as its own commit on the `main` branch,
tagged so you can stand at any one of them. The point is that **Spec** is reviewable before the code
exists, and **Build** is reviewable as a diff rather than as a finished tree.

`backlog` is the tip of the history from before the review format. It is kept as a marker, not as a
line of work. Every commit on it is also on `main`, so there is nothing on `backlog` to go back for.

## The tags

Every tag is `task/<number>/<beat>`:

| Tag                        | The tree at that point                                               |
| -------------------------- | -------------------------------------------------------------------- |
| `task/1.1/variant-webhook` | `Dataset` immutability enforced in `ValidateUpdate`                  |
| `task/1.1/variant-cel`     | the same rule as CEL transition rules, webhook block removed         |
| `task/1.2/spec`            | the status vocabulary written into `docs/product/domain.md`, no code |
| `task/1.2/build`           | the implementation, Claude's tests, and the three lookup variants    |

`git tag --list 'task/*'` is the current list; it grows as tasks are built.

## Standing at a stage

```bash
git switch main        # the whole branch, latest state
git switch --detach task/1.2/spec          # read the proposal before any code exists
git switch --detach task/1.2/build         # the implementation
git switch -                               # back where you were
```

`--detach` is deliberate: a tag is a place to read from, not a branch to commit on. Git will say
"You are in 'detached HEAD' state", which is the correct state for reading.

## Reading a beat as a diff

The useful view is usually not the tree but what the beat added:

```bash
git diff task/1.2/spec task/1.2/build                       # everything the build added
git diff task/1.2/spec task/1.2/build -- internal/          # just the Go
git diff task/1.2/spec task/1.2/build --stat                # what moved, before reading any of it
git show task/1.2/build                                     # the commit message argues the design
git log --oneline task/1.2/spec..task/1.2/build
```

For the Spec beat, the diff against what came before is the whole exercise:

```bash
git show task/1.2/spec -- docs/product/domain.md
```

## Doing your own half

The **Verify** beat is yours, and it belongs on your own branch off the stage it tests:

```bash
git switch -c mine/1.2-tests task/1.2/build
# write the cases listed at the foot of internal/controller/trainingjob_status_test.go
make fmt vet test
```

Keeping it on `mine/...` means the stage tags stay stable as references, and your work is diffable
against them later. When you want it in the trunk:

```bash
git switch main
git merge --no-ff mine/1.2-tests
```

## Comparing two variants that are whole branches

Task 1.1's two are mutually exclusive -- you would ship one, not both -- so they are branches rather
than a seam in one tree:

```bash
git diff task/1.1/variant-webhook task/1.1/variant-cel -- api/ internal/
git diff task/1.1/variant-webhook task/1.1/variant-cel -- config/crd/   # what the generator did
git show task/1.1/variant-cel                                          # the trade, argued
```

Both pass the gate. Check that rather than taking it on trust:

```bash
git switch --detach task/1.1/variant-cel && make fmt vet test lint
git switch --detach task/1.1/variant-webhook && make fmt vet test lint
git switch -
```

## Checking a variant comparison in one tree

The three `WorkloadFinder` implementations live side by side in one file rather than on three
branches, because the contract test has to run all three against the same fixtures -- that is what
makes the comparison a design question instead of a correctness one.

```bash
go test ./internal/controller/ -run TestFinders -v           # all three agree
go test ./internal/controller/ -run TestFramework -v          # where they do not
```

To try your verdict, change the one line in `cmd/main.go` that names the finder and run the gate:

```bash
make fmt vet test lint
```

If your verdict is the lightest strategy, confirm the claim rather than trusting it:

```bash
git switch -c mine/1.2-drop-jobframework task/1.2/build
# delete workload_finder.go's OwnerIndexFinder and FrameworkFinder, then:
go mod tidy && git diff --stat go.mod
```

That diff is the argument, in a form nobody can talk you out of.

## Proving a test fails for the right reason

A test that passes is not evidence until you have watched it fail. Break the implementation on a
scratch branch, confirm your case goes red, then throw the branch away:

```bash
git switch -c scratch/break-it task/1.2/build
# invert one arm of kueueReasonToPlatform in internal/controller/trainingjob_status.go
go test ./internal/controller/ -run TestAccepted
git switch - && git branch -D scratch/break-it
```

## If you get lost

```bash
git switch main                       # back to the branch tip
git status                            # what you have in flight
git stash list                        # what you put down earlier
```
