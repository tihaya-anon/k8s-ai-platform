# Task 1.1 part one — write the Dataset reconciler by hand

This is the one task in the curriculum that hands you the code. Every other task asks you to build
first; a reconciler has too many conventions to rediscover by guessing, so you type this one with
the reasons attached, and then [part two](#part-two--the-part-that-is-yours) leaves a feature
unimplemented for you to build alone.

Type each step into the real file rather than pasting the whole thing at the end. The steps compile
in order, and the point of each one is the sentence that follows it.

Start from the Kubebuilder scaffold — the file as `kubebuilder create api` wrote it, with
`// TODO(user): your logic here` still in `Reconcile`. If yours has more than that in it, the
generated blob is `git show 382a03d`.

## Three patterns from the Kubebuilder book, and why this walkthrough differs

The book's operator tutorial is the obvious place to start, and following it produces a reconciler
shaped differently from the one below. The differences are the lesson, so they are worth naming
before you type anything. None of the three is wrong in the book's context.

**The book checks `apierrors.IsNotFound` explicitly and logs:**

```go
	if err := r.Get(ctx, req.NamespacedName, &dataset); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Dataset not found. Ignoring since the object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Fail to get Dataset")
		return ctrl.Result{}, err
	}
```

This is correct, and it is exactly what `client.IgnoreNotFound(err)` does in one line. Write it out
once so you know what the helper hides, then use the helper. The `log.Error` on the second branch is
redundant: controller-runtime already logs an error returned from `Reconcile`, with the object's
identity attached, so logging it here yields two lines for one failure.

**The book names its conditions `Available`, `Progressing`, and `Degraded`.** Those come from
Deployment, where they describe a workload's rollout. A `Dataset` has no rollout. Condition types
are a vocabulary you are defining for your users, and the useful question is always "what would a
human want to know?" — here that is _was the artifact resolved, and does it match what you claimed_.
This walkthrough uses one condition, `Resolved`, and part two adds the second one. Copying the
Deployment vocabulary onto a domain that does not have rollouts gives a user three conditions that
cannot be acted on.

**The book initializes status on the first reconcile and then re-reads the object:**

```go
	if len(dataset.Status.Conditions) == 0 {
		meta.SetStatusCondition(&dataset.Status.Conditions, metav1.Condition{
			Type: typeProgressingDataset, Status: metav1.ConditionUnknown,
			Reason: "Reconciling", Message: "Starting reconciliation",
		})
		if err := r.Status().Update(ctx, &dataset); err != nil { ... }
		if err := r.Get(ctx, req.NamespacedName, &dataset); err != nil { ... }
	}
```

This is the one to be careful with. Two separate things are happening:

- The `if len(...) == 0` guard is a **first-pass branch**, and any real work placed inside it runs
  once in the lifetime of the object and never again. That is the level-triggered mistake described
  below, and it is easy to grow into: the status seeding is harmless there, but the next line you
  add inside the same block is usually not.
- The `Get` after the `Status().Update` is a **re-fetch**, and it is needed for a real reason: your
  in-memory copy's `resourceVersion` is stale the moment the write lands, so any later write using
  that copy fails with a conflict. The walkthrough below avoids the problem instead of paying for it
  — it computes the whole status, writes once at the end, and returns. One write per reconcile needs
  no re-fetch.

An `Unknown` placeholder condition is worth writing when reconciliation is long and a user would
otherwise see an empty status for minutes. Resolving a URI is not that; by the time anyone looks,
the real condition is already there.

## What a reconcile is

A reconciler answers four questions, in order, every time it runs:

1. **What does the world look like?** Read the object. Read anything it points at.
2. **What should it look like?** Compute desired state from `spec` alone.
3. **Make it so.** Write the difference — child objects, then status.
4. **When should I come back?** Return an error, a delay, or nothing.

It is **level-triggered**: it reads the current state every time and never assumes it saw the last
event. The scaffold's `if len(status.Conditions) == 0 { ... }` shape — do the work once, on the
first pass — is the single most common way to get this wrong. A URI that stops resolving, a status
wiped by a bad `kubectl patch`, a controller restart that misses a watch event: all of them are
cases where the work has to happen again on an object that has already been reconciled.

## Step 1 — fetch the object, and handle the one error that is not an error

```go
func (r *DatasetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var dataset mlv1alpha1.Dataset
	if err := r.Get(ctx, req.NamespacedName, &dataset); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
```

`req` carries a name and namespace, never the object: by the time you are called the object may have
changed again, so the queue deduplicates to a key and you read the current version yourself. That is
what level-triggered means in code.

`client.IgnoreNotFound` turns `NotFound` into `nil` and leaves every other error alone. Returning an
error for a deleted object would requeue it with backoff, forever, for an object that will never
come back. Every other error — API server down, RBAC denied — _should_ retry, so it goes back
untouched.

`logf.FromContext(ctx)` gives you a logger controller-runtime has already tagged with the object's
name, namespace, and reconcile ID. Build your own and you lose the correlation.

## Step 2 — put the outside world behind an interface

The reconciler has to learn what is actually at `spec.uri`. It must not do that by dialing object
storage from inside `Reconcile`: envtest runs a real API server but has **no network**, so a
reconciler that reaches out directly cannot be tested at all — and the failure paths are most of
what is worth testing here.

Create `internal/artifact/resolver.go`:

```go
// Metadata is what can be learned about an artifact without downloading it into
// the controller. It mirrors the claims a user may put in DatasetSpec, so the
// reconciler can compare the two.
type Metadata struct {
	Rows     int
	Checksum string
}

// Resolver reads artifact metadata at a URI.
type Resolver interface {
	Resolve(ctx context.Context, uri string) (Metadata, error)
}
```

Two implementations go in the same package: `FileResolver`, which resolves `file://` URIs by hashing
the content and counting rows, and `Fake`, which tests inject. The full file is in
[Appendix A](#appendix-a--internalartifactresolvergo).

Note what `Resolve` does **not** do: it does not copy bytes into the cluster and it does not create
a PVC. A `Dataset` is metadata and lineage. The training pod in task 1.2 reads the artifact store
itself; a controller that staged data would inherit storage classes, sizing, and cleanup and carry
no policy in exchange.

## Step 3 — give the reconciler its dependencies

```go
type DatasetReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Resolver artifact.Resolver

	// ResyncInterval is injectable so tests do not wait ten minutes.
	ResyncInterval time.Duration
}
```

Everything the reconciler cannot reach through the API server arrives as a field. This is the
`controller-runtime`-as-IoC-container point: `SetupWithManager` is the composition root, the struct
is the constructor, and nothing inside `Reconcile` reaches for a global.

While you are here, add the RBAC marker for events next to the ones the scaffold generated:

```go
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
```

Then run `make manifests` and read `git diff config/rbac/role.yaml`. The rule you just granted
appears there. `controller-gen` wrote it; the marker is the source of truth, the YAML is output. A
controller that emits events without this marker works in envtest — which has no RBAC in the suite's
client path — and fails in the cluster. That asymmetry is worth remembering.

## Step 4 — resolve, then describe the result in conditions

```go
	previousPhase := dataset.Status.Phase

	found, resolveErr := r.Resolver.Resolve(ctx, *dataset.Spec.Uri)
	if resolveErr != nil {
		log.Info("artifact resolution failed", "uri", *dataset.Spec.Uri, "error", resolveErr)
		dataset.Status.Phase = mlv1alpha1.DatasetPhaseFailed
		apimeta.SetStatusCondition(&dataset.Status.Conditions, metav1.Condition{
			Type:               typeResolvedDataset,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: dataset.Generation,
			Reason:             "ArtifactUnreachable",
			Message:            fmt.Sprintf("artifact store: %v", resolveErr),
		})
	} else {
		dataset.Status.ObservedRows = &found.Rows
		dataset.Status.ObservedChecksum = &found.Checksum
		dataset.Status.LastResolvedTime = metav1.Now()
		dataset.Status.Phase = mlv1alpha1.DatasetPhaseReady
		apimeta.SetStatusCondition(&dataset.Status.Conditions, metav1.Condition{
			Type:               typeResolvedDataset,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: dataset.Generation,
			Reason:             "ArtifactResolved",
			Message:            fmt.Sprintf("resolved %d rows at %s", found.Rows, *dataset.Spec.Uri),
		})
	}
```

With the condition type declared once at the top of the file:

```go
const typeResolvedDataset = "Resolved"
```

Four things are deliberate here.

- **The resolve call is unconditional.** No `if first time`. Every reconcile asks the artifact store
  again, which is what makes drift detectable at all in part two.
- **`SetStatusCondition` is not a field assignment.** It looks up the condition by `type`, and it
  only touches `lastTransitionTime` when `status` actually flips. A hand-rolled append would restamp
  the timestamp on every pass and destroy the one piece of information the field carries: how long
  this has been true.
- **`ObservedGeneration` is the condition's answer to "is this stale?"** `metadata.generation`
  increments on every `spec` write and never on a status write. A condition carrying an older
  generation than the object means: the controller has not caught up with your edit yet. Without it,
  `Ready: True` is ambiguous — ready for the spec you just submitted, or for the previous one?
- **`phase` is a `kubectl get` summary, not state.** Conditions are the source of truth; the phase
  exists because `kubectl get datasets` should be readable at a glance.

## Step 5 — write status, and only status

```go
	if err := r.Status().Update(ctx, &dataset); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}
```

`r.Status().Update` writes to the `/status` subresource, which the `+kubebuilder:subresource:status`
marker on the type enabled. Two consequences: your write cannot touch `spec` even by accident, and a
user's `spec` edit cannot clobber your status. It is also why status changes do not bump
`generation`, which is what makes `observedGeneration` meaningful.

A conflict means someone wrote the object between your `Get` and your `Update` — your copy's
`resourceVersion` is stale. The object you are holding is now wrong, so there is nothing to salvage:
requeue and start over with a fresh read. Returning the conflict as an error would work too, but it
logs an error for something that is normal and expected under concurrent writes.

## Step 6 — emit an event, but only on a transition

```go
	if dataset.Status.Phase != previousPhase {
		eventType := corev1.EventTypeNormal
		if dataset.Status.Phase == mlv1alpha1.DatasetPhaseFailed {
			eventType = corev1.EventTypeWarning
		}
		r.Recorder.Eventf(&dataset, eventType, string(dataset.Status.Phase),
			"Dataset %s: %s", dataset.Status.Phase,
			apimeta.FindStatusCondition(dataset.Status.Conditions, typeResolvedDataset).Message)
	}
```

The repository convention is that every state transition emits an Event — _transition_, not pass. A
reconciler that emits unconditionally produces one event per resync forever, and `kubectl describe`
becomes unreadable at exactly the moment someone needs it.

Events are a separate object kind with their own retention (an hour by default). They are the
human-facing trace; conditions are the machine-facing state. Both, not either.

The event is emitted **after** the status write succeeds. Announcing a transition you failed to
persist is how a user ends up reading an event that the object contradicts.

## Step 7 — say when to come back

```go
	return ctrl.Result{RequeueAfter: r.resyncInterval()}, nil
}
```

Three ways to end a reconcile, and they mean different things:

| Return                              | Effect                                   | Use when                                           |
| ----------------------------------- | ---------------------------------------- | -------------------------------------------------- |
| `ctrl.Result{}, err`                | requeue with exponential backoff, logged | something failed and retrying may fix it           |
| `ctrl.Result{Requeue: true}, nil`   | requeue with backoff, no error logged    | expected contention, like a conflict               |
| `ctrl.Result{RequeueAfter: d}, nil` | requeue after exactly `d`                | nothing is wrong; poll something that has no watch |

The `RequeueAfter` here exists because an artifact store has no watch: a URI can start failing, or
its content can be replaced, without anyone touching the `Dataset`. Ten minutes is a guess you
should be able to defend — it is the window during which the platform will keep claiming `Ready` for
an artifact that has gone.

Never `time.Sleep` in a reconciler. The worker is one of a fixed pool, and sleeping blocks every
other object queued behind it.

## Step 8 — wire it up in `cmd/main.go`

```go
	if err := (&controller.DatasetReconciler{
		Client:   mgr.GetClient(),
		Scheme:   mgr.GetScheme(),
		Recorder: mgr.GetEventRecorderFor("dataset-controller"),
		Resolver: artifact.FileResolver{},
	}).SetupWithManager(mgr); err != nil {
```

`mgr.GetClient()` is the cached, watch-backed client — reads come from an informer, writes go
straight to the API server. `GetEventRecorderFor` names the component that will appear in the `FROM`
column of `kubectl describe`.

## Step 9 — prove it with envtest

envtest starts a real `kube-apiserver` and `etcd` against your CRDs. Everything above the API is
yours; everything below is real validation, real defaulting, real subresources. What it does not
have is a running manager, so you call `Reconcile` directly and assert on what landed in the API.

The two cases that matter are the happy path and the one failure you can actually produce — add both
to `internal/controller/dataset_controller_test.go`
([Appendix C](#appendix-c--the-two-envtest-cases)):

```go
			controllerReconciler := &DatasetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
				Resolver: &artifact.Fake{Entries: map[string]artifact.Metadata{
					"sample.uri": {Rows: 1, Checksum: "placeholder"},
				}},
			}
```

That is the payoff for step 2: `artifact.Fake` makes "the store is down" a one-line test instead of
an unreachable branch. `record.NewFakeRecorder(10)` is a buffered channel you can also assert on.

Run `make test`. Reconciler coverage should be around 80%, and both cases should pass.

## Part two — the part that is yours

Everything above resolves an artifact and reports what it found. It does **not** yet do the thing
this CRD exists for. From `docs/architecture.md`:

> `rows` and `checksum` appear on both sides on purpose. In `spec` they are the user's claim about
> what they registered; in `status` they are what the controller actually found at the URI. A
> silently rewritten artifact is only detectable because the two can disagree.

Nothing currently compares them. Build that.

**You build:**

1. **Drift detection in the reconciler.** When `spec.rows` or `spec.checksum` is set and the
   resolved metadata disagrees, the `Dataset` is `Failed`, not `Ready` — with a condition reason
   that names what disagreed and a `Warning` event. A `Dataset` that was `Ready` yesterday and whose
   artifact was rewritten overnight must come back `Failed` on a resync with nobody editing it.
   Decide deliberately whether `status.observedChecksum` still records the new value when it
   mismatches, and be able to defend the answer.
2. **The immutability webhook.** `ValidateUpdate` rejects changes to `spec.uri`, `spec.rows`, and
   `spec.checksum` on an existing object. Mind the optional pointer fields: `Rows` and `Checksum`
   are `*int` and `*string`, and comparing the pointers compares addresses, not values. A nil on one
   side and a value on the other is a change, not a panic.
3. **Tests for both.** envtest for the drift path, the webhook suite for the rejection, and an
   assertion on the error message, not just on the fact that it failed.

**Acceptance criteria**

- A resync with changed artifact content moves a `Ready` `Dataset` to `Failed` with no user edit.
- The condition reason distinguishes "could not reach the artifact" from "artifact does not match
  the claim". Errors name the layer at fault.
- `kubectl edit` on `spec.uri` of an existing `Dataset` is rejected, and the message tells the user
  to create a new `Dataset` instead.
- `make fmt vet test` passes.

Then answer the quiz in [training-tasks.md](training-tasks.md#11-dataset-lifecycle) and record it in
[progress.md](progress.md).

## Appendix A — `internal/artifact/resolver.go`

```go
// Package artifact reads metadata about a dataset artifact without copying it
// into the cluster. The platform records what an artifact looks like; moving its
// bytes is the training pod's job.
package artifact

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"os"
)

// Metadata is what can be learned about an artifact without downloading it into
// the controller. It mirrors the claims a user may put in DatasetSpec, so the
// reconciler can compare the two.
type Metadata struct {
	Rows     int
	Checksum string
}

// Resolver reads artifact metadata at a URI.
//
// This is an interface because envtest runs an API server but no network: the
// suite injects a fake, the manager injects a real store. A reconciler that
// dials object storage directly cannot be tested without one.
type Resolver interface {
	Resolve(ctx context.Context, uri string) (Metadata, error)
}

// FileResolver resolves file:// URIs by reading the artifact from disk.
type FileResolver struct{}

// Resolve counts newline-terminated rows and hashes the content.
func (FileResolver) Resolve(ctx context.Context, uri string) (Metadata, error) {
	parsed, err := url.Parse(uri)
	if err != nil {
		return Metadata{}, fmt.Errorf("parse uri: %w", err)
	}
	if parsed.Scheme != "file" {
		return Metadata{}, fmt.Errorf("unsupported scheme %q", parsed.Scheme)
	}

	file, err := os.Open(parsed.Path)
	if err != nil {
		return Metadata{}, fmt.Errorf("open artifact: %w", err)
	}
	defer func() { _ = file.Close() }()

	hash := sha256.New()
	rows := 0
	scanner := bufio.NewScanner(io.TeeReader(file, hash))
	for scanner.Scan() {
		// Cancellation matters here: a large artifact must not keep a worker busy
		// after the manager has started shutting down.
		if err := ctx.Err(); err != nil {
			return Metadata{}, err
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		return Metadata{}, fmt.Errorf("read artifact: %w", err)
	}

	return Metadata{Rows: rows, Checksum: hex.EncodeToString(hash.Sum(nil))}, nil
}

// Fake is an in-memory Resolver for tests.
type Fake struct {
	Entries map[string]Metadata
	Err     error
}

// Resolve returns the configured error, or the entry registered for uri.
func (f *Fake) Resolve(_ context.Context, uri string) (Metadata, error) {
	if f.Err != nil {
		return Metadata{}, f.Err
	}
	found, ok := f.Entries[uri]
	if !ok {
		return Metadata{}, fmt.Errorf("no artifact at %q", uri)
	}
	return found, nil
}
```

## Appendix B — the complete reconciler

Compare this against what you typed. If something differs, the interesting question is whether your
version is wrong or merely different.

```go
package controller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
	"github.com/tihaya-anon/k8s-ai-platform/internal/artifact"
)

// Condition types this controller owns. "Resolved" is about the artifact store;
// a second controller writing its own condition type never collides with it.
const (
	typeResolvedDataset = "Resolved"
)

// defaultResyncInterval re-resolves an artifact that nothing has touched. A URI
// can start failing without anyone editing the Dataset, and no watch fires for
// that.
const defaultResyncInterval = 10 * time.Minute

// DatasetReconciler reconciles a Dataset object
type DatasetReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder record.EventRecorder
	Resolver artifact.Resolver

	// ResyncInterval is injectable so tests do not wait ten minutes.
	ResyncInterval time.Duration
}

// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=datasets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=datasets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=datasets/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

func (r *DatasetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var dataset mlv1alpha1.Dataset
	if err := r.Get(ctx, req.NamespacedName, &dataset); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	previousPhase := dataset.Status.Phase

	found, resolveErr := r.Resolver.Resolve(ctx, *dataset.Spec.Uri)
	if resolveErr != nil {
		log.Info("artifact resolution failed", "uri", *dataset.Spec.Uri, "error", resolveErr)
		dataset.Status.Phase = mlv1alpha1.DatasetPhaseFailed
		apimeta.SetStatusCondition(&dataset.Status.Conditions, metav1.Condition{
			Type:               typeResolvedDataset,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: dataset.Generation,
			Reason:             "ArtifactUnreachable",
			Message:            fmt.Sprintf("artifact store: %v", resolveErr),
		})
	} else {
		dataset.Status.ObservedRows = &found.Rows
		dataset.Status.ObservedChecksum = &found.Checksum
		dataset.Status.LastResolvedTime = metav1.Now()
		dataset.Status.Phase = mlv1alpha1.DatasetPhaseReady
		apimeta.SetStatusCondition(&dataset.Status.Conditions, metav1.Condition{
			Type:               typeResolvedDataset,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: dataset.Generation,
			Reason:             "ArtifactResolved",
			Message:            fmt.Sprintf("resolved %d rows at %s", found.Rows, *dataset.Spec.Uri),
		})
	}

	if err := r.Status().Update(ctx, &dataset); err != nil {
		if apierrors.IsConflict(err) {
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	if dataset.Status.Phase != previousPhase {
		eventType := corev1.EventTypeNormal
		if dataset.Status.Phase == mlv1alpha1.DatasetPhaseFailed {
			eventType = corev1.EventTypeWarning
		}
		r.Recorder.Eventf(&dataset, eventType, string(dataset.Status.Phase),
			"Dataset %s: %s", dataset.Status.Phase, apimeta.FindStatusCondition(dataset.Status.Conditions, typeResolvedDataset).Message)
	}

	return ctrl.Result{RequeueAfter: r.resyncInterval()}, nil
}

func (r *DatasetReconciler) resyncInterval() time.Duration {
	if r.ResyncInterval > 0 {
		return r.ResyncInterval
	}
	return defaultResyncInterval
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatasetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mlv1alpha1.Dataset{}).
		Named("dataset").
		Complete(r)
}
```

## Appendix C — the two envtest cases

Both go inside the existing `Describe("Dataset Controller")` block, replacing the scaffold's
`It("should successfully reconcile the resource")`. The new imports are
`apimeta "k8s.io/apimachinery/pkg/api/meta"`, `"k8s.io/client-go/tools/record"`, and the
`internal/artifact` package.

```go
		It("should record resolved metadata and go Ready", func() {
			By("Reconciling the created resource")
			controllerReconciler := &DatasetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
				Resolver: &artifact.Fake{Entries: map[string]artifact.Metadata{
					"sample.uri": {Rows: 1, Checksum: "placeholder"},
				}},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			resolved := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resolved)).To(Succeed())
			Expect(resolved.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseReady))
			Expect(resolved.Status.ObservedRows).To(HaveValue(Equal(1)))
			Expect(resolved.Status.LastResolvedTime.IsZero()).To(BeFalse())
			Expect(apimeta.IsStatusConditionTrue(resolved.Status.Conditions, "Resolved")).To(BeTrue())
		})

		It("should go Failed when the artifact store cannot resolve the uri", func() {
			controllerReconciler := &DatasetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
				Resolver: &artifact.Fake{Err: errors.NewServiceUnavailable("store down")},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			failed := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, failed)).To(Succeed())
			Expect(failed.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseFailed))
			condition := apimeta.FindStatusCondition(failed.Status.Conditions, "Resolved")
			Expect(condition.Reason).To(Equal("ArtifactUnreachable"))
			Expect(condition.ObservedGeneration).To(Equal(failed.Generation))
		})
```
