# Where a rule lives: schema, CEL, webhook, controller

Four places can reject or report a bad `Dataset`, and they are not interchangeable. Picking the
wrong one is how a project ends up with a certificate rotation problem in exchange for a rule the
API server would have enforced for free. The rule of thumb is **the leftmost column that can see
what the rule needs**.

| Enforced by                      | Can see                                                      | Costs                                           | Fails by                                  |
| -------------------------------- | ------------------------------------------------------------ | ----------------------------------------------- | ----------------------------------------- |
| OpenAPI schema (markers)         | one field, one value                                         | nothing; it is in the CRD                       | cannot express relationships              |
| CEL (`x-kubernetes-validations`) | the whole object, and `oldSelf` on update                    | nothing at runtime; rules are compiled at apply | cannot read other objects or the network  |
| Validating webhook               | the object, its old version, and anything a client can reach | PKI, a Service, an availability risk            | a down webhook blocks writes cluster-wide |
| Controller                       | the world, over time                                         | a reconcile and a status write                  | reports after the fact, never rejects     |

The last row is a different job, not a weaker webhook: a controller cannot say no. By the time it
runs, the object is stored. It can only observe and report — which is exactly what drift detection
is.

## Immutability without a webhook

Since validation rules reached beta in 1.25 and went GA, an immutable field is a marker:

```go
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="uri is immutable"
Uri *string `json:"uri"`
```

`controller-gen` turns it into `x-kubernetes-validations` in the CRD, and the **API server**
enforces it. No certificate, no `Service`, no Go code, and no window where a crashed webhook pod
makes the resource unwritable.

The sharp edge is optional fields. Kubernetes **skips a transition rule when the old value is
absent**, so a field-scoped `self == oldSelf` on an optional `rows` does not stop `nil → 5`. Making
an optional field immutable-once-set takes two rules: the field-scoped one above, plus a type-scoped
rule on the parent struct asserting the field cannot disappear —
`!has(oldSelf.rows) || has(self.rows)`. The Kubernetes blog post linked below walks through the
pair.

So why does task 1.1 still ask for a webhook? Because the admission path is the thing being taught,
and because rules that need a client — "this URI is already registered by another `Dataset`" — have
no CEL equivalent. Write the webhook to learn the mechanism, then know that a pure field-equality
rule belongs in the schema.

## If it is a webhook, use the helpers that ship with it

`ValidateUpdate` returning `field.ErrorList` is the Kubernetes house style, and most of the list is
already written for you:

```go
// k8s.io/apimachinery/pkg/api/validation/objectmeta.go
func ValidateImmutableField(newVal, oldVal interface{}, fldPath *field.Path) field.ErrorList {
	allErrs := field.ErrorList{}
	if !apiequality.Semantic.DeepEqual(oldVal, newVal) {
		allErrs = append(allErrs, field.Invalid(fldPath, newVal, FieldImmutableErrorMsg))
	}
	return allErrs
}
```

`FieldImmutableErrorMsg` is the constant `field is immutable` — the phrase every Kubernetes user has
already read in some other resource's rejection. `Semantic.DeepEqual` compares pointees, so the
nil/non-nil asymmetry of optional `*int` and `*string` fields is handled rather than dereferenced.
Kueue calls it directly:

```go
// kueue/pkg/webhooks/clusterqueue_webhook.go
allErrs = append(allErrs, apimachineryvalidation.ValidateImmutableField(
	newCQ.Spec.ConcurrentAdmissionPolicy,
	oldCQ.Spec.ConcurrentAdmissionPolicy,
	field.NewPath("spec", "concurrentAdmissionPolicy"),
)...)
```

Three conventions in that call worth copying:

- **`field.NewPath("spec", "rows")`, never the bare string `"rows"`.** The path is what `kubectl`
  prints and what a client parses.
- **The error type is part of the message.** `field.Invalid` means the user sent something wrong;
  `field.Forbidden` means the field exists but may not be set here; `field.ErrorTypeInternal` means
  _the server broke_ and is wrong for anything a user caused.
- **An empty list is the success case.** `ErrorList.ToAggregate()` returns a true `nil` when the
  list is empty, so the guard is free — but only if the return value is actually the aggregate.
  Returning `apierrors.NewInvalid(...)` unconditionally rejects every update, including the ones
  with nothing wrong.

### `ToAggregate()` or `NewInvalid()`

Both are used upstream — Kueue returns `allErrs.ToAggregate()`, Cluster API wraps in
`apierrors.NewInvalid(gk, name, allErrs)` — and controller-runtime decides what the difference buys:

```go
// controller-runtime/pkg/webhook/admission/validator_custom.go
var apiStatus apierrors.APIStatus
if errors.As(err, &apiStatus) {
	return validationResponseFromStatus(false, apiStatus.Status()).WithWarnings(warnings...)
}
return Denied(err.Error()).WithWarnings(warnings...)
```

`NewInvalid` implements `APIStatus`, so the user gets a `422` carrying structured `.details.causes`
— one entry per field, machine-readable. A bare aggregate gets a `403` with a flat string. Its
arguments are the object's `GroupKind` and the object's **name**, not a message.

## Drift is not validation

A webhook compares `oldObj.Spec` to `newObj.Spec` and asks _did the user change a frozen field?_ A
reconciler compares `spec` to what it just observed and asks _does the artifact still match the
claim?_ The comparison looks identical and the verdicts are opposite:

| Transition    | Webhook (old spec → new spec) | Drift (spec claim → observation) |
| ------------- | ----------------------------- | -------------------------------- |
| `nil → value` | a change; reject              | no claim was made; ignore        |
| `a → b`       | a change; reject              | drift; report                    |

Share the vocabulary — `field.ErrorList`, the field paths, the message wording — and keep the
predicate separate. One function serving both grows a boolean argument, and a boolean argument that
inverts a rule is how the rule gets forgotten.

### Reporting drift without retrying it

Drift is permanent until something outside the cluster changes. Returning it as an error from
`Reconcile` requeues it with exponential backoff, logs it at error level every time, and never
converges — and if the error is returned _before_ the status write, the condition describing the
drift is never persisted at all. Two correct endings:

- **Write the condition, emit the Warning event, return `ctrl.Result{RequeueAfter: d}, nil`.** The
  controller is not failing; it is accurately reporting a `Failed` phase. The resync is when to look
  again, since the artifact may be restored.
- **`reconcile.TerminalError(err)`** (controller-runtime v0.22, `pkg/reconcile/reconcile.go`) —
  logged once, counted in metrics, never requeued. For "this will never succeed and there is nothing
  useful to report".

Either way the status write happens first. A condition computed and then skipped is invisible.

## Where shared rules live

Two upstream structures, both of which keep the rules out of the webhook file:

- **A pure `validation` package, Kubernetes-core style.** Core API groups put
  `ValidateXxx(new, old) field.ErrorList` in `pkg/apis/<group>/validation/`, importing nothing but
  `apimachinery`. Kueue mirrors it with `ValidateClusterQueueUpdate(oldCQ, newCQ)`. The webhook
  becomes an adapter: convert `runtime.Object`, call the function, render the list. Table-driven
  unit tests, no envtest, no client.
- **Knative's interface style.** `knative.dev/pkg/apis` defines `Validatable` (`Validate(ctx)`) and
  `Immutable` (`CheckImmutableFields(ctx, original)`) as methods on the API type, with one shared
  webhook dispatching to any resource that implements them. More machinery, and it pays off across
  dozens of CRDs rather than five.

Either way the test is the same: the package holding the rules must not import `client.Client`, and
must be testable without an API server.

## What this repository tried first

Task 1.1 part two was written twice, and both attempts are in the history — useful because each one
is a different wrong turn, and neither is a silly one.

`b228318` — the first `detectDrift`: compared `*dset.Spec.Rows` against `*dset.Status.ObservedRows`
after a `!= nil` guard on the spec side only, built `[]string` details, and wrote the `Resolved`
condition itself with `Reason: DriftDetected`. Three lessons: the status side can be nil on the
branch where resolution failed (the existing store-unreachable envtest case panics on it, which is
the test earning its keep); `fmt.Sprintf("%v", []string)` renders Go syntax into a message a human
reads in `kubectl describe`; and overloading `Resolved` with a drift reason means a consumer
filtering on `Resolved=False` cannot separate "the store is down" from "your checksum is wrong",
against the convention that _errors name the layer at fault_.

`1b4c1ff` — the second: moved both sides onto `field.ErrorList` and `ptr.Equal`, which is the right
direction and the reason this page exists. What it shows is that the remaining mistakes are all in
the gaps the helpers above already fill — a hand-rolled comparison that dropped the "only if the
user claimed it" guard, `field.ErrorTypeInternal` on a user error, bare string field names, an
unconditional `NewInvalid` that rejects every update, a `GroupKind{}` with nothing in it, and the
drift error returned from `Reconcile` before the status write. The duplicated per-field block also
drifted between its two copies, in both files, in the way duplicated blocks do.

Neither commit is the finished feature; the build half of
[1.1-dataset-reconciler.md](../curriculum/1.1-dataset-reconciler.md) is still open.

## Sources

- [Enforce CRD Immutability with CEL Transition Rules](https://kubernetes.io/blog/2022/09/29/enforce-immutability-using-cel/)
- [CustomResourceDefinition Validation Rules Graduate to Beta](https://kubernetes.io/blog/2022/09/23/crd-validation-rules-beta/)
- [CRD Validation markers — Kubebuilder Book](https://book.kubebuilder.io/reference/markers/crd-validation.html)
- [Implementing defaulting/validating webhooks — Kubebuilder Book](https://book.kubebuilder.io/cronjob-tutorial/webhook-implementation.html)
- [Kueue — `pkg/webhooks/clusterqueue_webhook.go`](https://github.com/kubernetes-sigs/kueue/blob/main/pkg/webhooks/clusterqueue_webhook.go)
- [controller-runtime `TerminalError`](https://github.com/kubernetes-sigs/controller-runtime/blob/main/pkg/reconcile/reconcile.go)
- [`reconcile` package reference](https://pkg.go.dev/sigs.k8s.io/controller-runtime/pkg/reconcile)
- [`knative.dev/pkg/apis` — `Validatable`, `Immutable`](https://pkg.go.dev/knative.dev/pkg/apis)
- [Webhooks — Cluster API Book](https://cluster-api.sigs.k8s.io/developer/providers/getting-started/webhooks)
