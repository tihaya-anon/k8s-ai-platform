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
	"k8s.io/client-go/tools/record"
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
		apimeta.SetStatusCondition(&dset.Status.Conditions, metav1.Condition{
			Type:               typeResolvedDataset,
			Status:             metav1.ConditionFalse,
			ObservedGeneration: dset.Generation,
			Reason:             "ArtifactUnreachable",
			Message:            fmt.Sprintf("artifact store: %v", resolveErr),
		})
	} else {
		dset.Status.ObservedRows = &found.Rows
		dset.Status.ObservedChecksum = &found.Checksum
		dset.Status.LastResolvedTime = metav1.Now()
		dset.Status.Phase = mlv1alpha1.DatasetPhaseReady
		apimeta.SetStatusCondition(&dset.Status.Conditions, metav1.Condition{
			Type:               typeResolvedDataset,
			Status:             metav1.ConditionTrue,
			ObservedGeneration: dset.Generation,
			Reason:             "ArtifactResolved",
			Message:            fmt.Sprintf("resolved %d rows at %s", found.Rows, *dset.Spec.Uri),
		})
	}

	r.detectDrift(ctx, &dset)

	if err := r.Status().Update(ctx, &dset); err != nil {
		return ctrl.Result{}, err
	}

	if dset.Status.Phase != previousPhase {
		eventType := corev1.EventTypeNormal
		if dset.Status.Phase == mlv1alpha1.DatasetPhaseFailed {
			eventType = corev1.EventTypeWarning
		}
		r.Recorder.Eventf(&dset, eventType, string(dset.Status.Phase), "Dataset %s: %s", dset.Status.Phase, condition.Message)
		return ctrl.Result{RequeueAfter: r.resyncInterval()}, nil
	}

	return ctrl.Result{}, nil
}

func (r *DatasetReconciler) resyncInterval() time.Duration {
	if r.ResyncInterval > 0 {
		return r.ResyncInterval
	}
	return defaultResyncInterval
}

func (r *DatasetReconciler) detectDrift(_ context.Context, dset *mlv1alpha1.Dataset) {
	var driftErrors []string
	if dset.Spec.Rows != nil && *dset.Spec.Rows != *dset.Status.ObservedRows {
		driftErrors = append(driftErrors, fmt.Sprintf("rows set but mismatched, expect %v, get %v", *dset.Spec.Rows, *dset.Status.ObservedRows))
	}
	if dset.Spec.Checksum != nil && *dset.Spec.Checksum != *dset.Status.ObservedChecksum {
		driftErrors = append(driftErrors, fmt.Sprintf("checksum set but mismatched, expect %v, get %v", *dset.Spec.Checksum, *dset.Status.ObservedChecksum))
	}
	if len(driftErrors) == 0 {
		return
	}
	dset.Status.Phase = mlv1alpha1.DatasetPhaseFailed
	apimeta.SetStatusCondition(&dset.Status.Conditions, metav1.Condition{
		Type:               typeResolvedDataset,
		Status:             metav1.ConditionFalse,
		ObservedGeneration: dset.Generation,
		Reason:             "DriftDetected",
		Message:            fmt.Sprintf("drifts: %v", driftErrors),
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatasetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mlv1alpha1.Dataset{}).
		Named("dataset").
		Complete(r)
}
