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

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

// DatasetReconciler reconciles a Dataset object
type DatasetReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

const (
	typeAvailableDataset   = "Available"
	typeProgressingDataset = "Progressing"
	typeDegradedDataset    = "Degraded"
)

// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=datasets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=datasets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=datasets/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the Dataset object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *DatasetReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)
	var dataset mlv1alpha1.Dataset

	// Cannot get the object
	if err := r.Get(ctx, req.NamespacedName, &dataset); err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("Dataset not found. Ignoring since the object must be deleted")
			return ctrl.Result{}, nil
		}
		log.Error(err, "Fail to get Dataset")
		return ctrl.Result{}, err
	}

	// Initialize status
	if len(dataset.Status.Conditions) == 0 {
		meta.SetStatusCondition(&dataset.Status.Conditions, metav1.Condition{
			Type:    typeProgressingDataset,
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Starting reconciliation",
		})
		if err := r.Status().Update(ctx, &dataset); err != nil {
			log.Error(err, "Failed to update Dataset status")
			return ctrl.Result{}, err
		}
		if err := r.Get(ctx, req.NamespacedName, &dataset); err != nil {
			log.Error(err, "Failed to re-fetch Dataset")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *DatasetReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mlv1alpha1.Dataset{}).
		Named("dataset").
		Complete(r)
}
