/*
Copyright 2026.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/tihaya-anon/k8s-ai-platform/internal/artifact"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

// DatasetReconciler reconciles a Dataset object
type DatasetReconciler struct {
	client.Client
	Scheme         *runtime.Scheme
	Recorder       record.EventRecorder
	Resolver       artifact.Resolver
	ResyncInterval time.Duration
}

// ensure interface implementation
var _ reconcile.Reconciler = (*DatasetReconciler)(nil)

const defaultResyncInterval = 10 * time.Minute
const typeResolvedDataset = "Resolved"

// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=datasets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=datasets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=datasets/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *DatasetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var dset mlv1alpha1.Dataset
	if err := r.Get(ctx, req.NamespacedName, &dset); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	previousPhase := dset.Status.Phase

	condition := metav1.Condition{
		Type:               typeResolvedDataset,
		ObservedGeneration: dset.Generation,
	}

	found, resolveErr := r.Resolver.Resolve(ctx, *dset.Spec.Uri)
	if resolveErr != nil {
		log.Info("artifact resolution failed", "uri", *dset.Spec.Uri, "error", resolveErr)
		dset.Status.Phase = mlv1alpha1.DatasetPhaseFailed
		condition.Status = metav1.ConditionFalse
		condition.Reason = "ArtifactUnreachable"
		condition.Message = fmt.Sprintf("artifact store: %v", resolveErr)
	} else {
		dset.Status.ObservedRows = &found.Rows
		dset.Status.ObservedChecksum = &found.Checksum
		dset.Status.LastResolvedTime = metav1.Now()
		dset.Status.Phase = mlv1alpha1.DatasetPhaseReady
		condition.Status = metav1.ConditionTrue
		condition.Reason = "ArtifactResolved"
		condition.Message = fmt.Sprintf("resolved %d rows at %s", found.Rows, *dset.Spec.Uri)
	}

	// Drift is only computable against a fresh observation, so it cannot be
	// decided on the branch where resolution failed.
	var driftError errors.Aggregate
	if resolveErr == nil {
		driftError = r.detectDrift(&dset)
	}
	if driftError != nil {
		log.Info("artifact fields drifted", "error", driftError)
		dset.Status.Phase = mlv1alpha1.DatasetPhaseFailed
		condition.Status = metav1.ConditionFalse
		condition.Reason = "DriftDetected"
		condition.Message = driftError.Error()
	}
	apimeta.SetStatusCondition(&dset.Status.Conditions, condition)

	if err := r.Status().Update(ctx, &dset); err != nil {
		return ctrl.Result{}, err
	}

	if dset.Status.Phase != previousPhase {
		eventType := corev1.EventTypeNormal
		if dset.Status.Phase == mlv1alpha1.DatasetPhaseFailed {
			eventType = corev1.EventTypeWarning
		}
		r.Recorder.Eventf(&dset, eventType, string(dset.Status.Phase), "Dataset %s: %s", dset.Status.Phase, condition.Message)
	}

	// The artifact store has no watch: an untouched Dataset is only re-resolved
	// because this returns. A transition is a reason to announce, never a reason
	// to come back.
	return ctrl.Result{RequeueAfter: r.resyncInterval()}, nil
}

func (r *DatasetReconciler) resyncInterval() time.Duration {
	if r.ResyncInterval > 0 {
		return r.ResyncInterval
	}
	return defaultResyncInterval
}

func (r *DatasetReconciler) detectDrift(dset *mlv1alpha1.Dataset) errors.Aggregate {
	var driftErrors field.ErrorList
	if dset.Spec.Rows != nil && !ptr.Equal(dset.Spec.Rows, dset.Status.ObservedRows) {
		driftErrors = append(driftErrors, field.Invalid(
			field.NewPath("spec", "rows"),
			optional(dset.Status.ObservedRows),
			fmt.Sprintf("artifact resolved to %s rows, spec claims %s",
				optional(dset.Status.ObservedRows), optional(dset.Spec.Rows)),
		))
	}
	if dset.Spec.Checksum != nil && !ptr.Equal(dset.Spec.Checksum, dset.Status.ObservedChecksum) {
		driftErrors = append(driftErrors, field.Invalid(
			field.NewPath("spec", "checksum"),
			optional(dset.Status.ObservedChecksum),
			fmt.Sprintf("artifact resolved to checksum %s, spec claims %s",
				optional(dset.Status.ObservedChecksum), optional(dset.Spec.Checksum)),
		))
	}
	return driftErrors.ToAggregate()
}

// optional renders an optional field for a human-readable message. Formatting
// the pointer itself would print an address, and a dereference would panic on
// the branch where resolution failed and nothing was observed.
func optional[T any](p *T) string {
	if p == nil {
		return "unset"
	}
	return fmt.Sprintf("%v", *p)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatasetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mlv1alpha1.Dataset{}).
		Named("dataset").
		Complete(r)
}
