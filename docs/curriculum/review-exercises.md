# Review exercises

Each task ends in a graded **Review** beat. This file defines the exercise types so that a task
entry can name one in a line instead of restating the rules.

The premise: an agent writes most of the code now, so the scarce skill is not producing a reconciler
but judging one — spotting the plausible-looking defect, choosing between two designs that both
compile, and knowing what the system does when a dependency is down. These exercises train the
judgment, and the answers are recorded in [progress.md](../log/progress.md) the same way quiz
answers always were.

A review answer is graded on the **reason**, never on matching a key. "Option B, because the index
is maintained by the informer and the label is maintained by a controller that may not have run yet"
is a pass. "Option B" is not an answer.

---

## A — Variant comparison

Claude implements the same behaviour two or three ways, each complete and each passing the gate, and
names the axis they differ on. You pick one for the repository and defend it.

What makes a variant set worth building: the options have to be genuinely defensible, so that the
answer turns on a property of the system rather than on one version being obviously broken. A set
where one option is wrong is a defect review wearing a costume.

Deliverable from you: a verdict, the reason, and the condition under which you would switch. That
last part is the one that matters — a design choice you cannot reverse the reasoning on was a
coin-flip.

## B — Seeded defect review

Claude hands you a diff containing a stated number of deliberate defects, drawn from the failure
modes this repository actually hits: a swallowed error, a level-triggered assumption, a write to a
field another operator owns, a condition that reports the wrong layer, a test that passes for the
wrong reason.

The count is stated so the exercise terminates. The defects are realistic, not typos — if a `gofmt`,
`go vet` or `golangci-lint` run would catch it, it does not count as one.

Deliverable from you: file and line for each, what breaks, and under what input. Score is _n_ of
_m_, recorded.

## C — Lifecycle narration

Describe a path through the system end to end, with no code. For example: a `TrainingJob` from
`kubectl apply` to a running pod — which admission plugins run, in what order, which webhook sees
what, what the API server stores, which watch fires which controller, and where the pod's resource
requests came from.

This is the exercise that makes the other four legible, and the one most likely to expose a gap you
did not know you had. A narration that cannot say _which process_ performed a step is incomplete.

## D — Blast radius

A single "what happens if" about a dependency failing or an object disappearing underneath the
platform. The `ClusterQueue` is deleted under a running job. The Kueue webhook is unreachable. The
`Dataset` an admitted `TrainingJob` referenced is deleted mid-run. A controller is scaled to two
replicas without leader election.

Deliverable from you: what the user sees, what the platform does, and whether that is the behaviour
the platform should have. The third part is the graded one.

## E — Spec review

Claude writes a product decision into `docs/product/` as a proposal, with the reasoning attached,
and you review it before any code exists.

Deliverable from you: accept, amend, or reject with a reason — and specifically, what the proposal
leaves underspecified. A spec that does not say what happens in some reachable state is the defect
this exercise exists to catch, and it is cheaper to catch here than in a seeded-defect review later.

## F — Test design

Not an exercise in isolation but the standing split in the **Verify** beat: Claude writes some of
the tests, and the task names the ones left to you. Writing a good test is its own skill, and it is
the one form of implementation that keeps its value when an agent writes the feature — a test is
review expressed as code.

What the task names for you is chosen for what it teaches: the case that needs a fake rather than a
cluster, the case that proves a negative, the case that fails for the right reason. Claude's half
covers the mechanical coverage around it.

A test you wrote that passes against a deliberately broken implementation is worth more attention
than a test that passes — see B.
