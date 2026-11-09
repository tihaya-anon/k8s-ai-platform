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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

// TrainingJobReconciler reconciles a TrainingJob object
type TrainingJobReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=trainingjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=trainingjobs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ml.aiplatform.io,resources=trainingjobs/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *TrainingJobReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx)

	var tj mlv1alpha1.TrainingJob
	if err := r.Get(ctx, req.NamespacedName, &tj); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	job := &batchv1.Job{
		Name:      tj.Name + "Job",
		Namespace: tj.Namespace,
		Labels: map[string]string{
			"kueue.x-k8s.io/queue-name": tj.Spec.Queue,
		},
	}

	_, err := ctrl.CreateOrUpdate(ctx, r.Client, job, func() error {
		if job.CreationTimestamp.IsZero() {
			job.Spec.Selector = &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"TrainingJob": tj.Name,
					"Owner":       tj.Spec.Owner,
				},
			}
		}
		job.Spec = r.batchV1JobSpec(&tj)
		return nil
	})
	if err != nil {
		log.Error(err, "Deployment reconcile failed")
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (r *TrainingJobReconciler) batchV1JobSpec(tj *mlv1alpha1.TrainingJob) batchv1.JobSpec {
	ps := corev1.PodSpec{
		Containers: []corev1.Container{
			{
				Image: tj.Spec.Image,
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    tj.Spec.Resources.CPU,
						corev1.ResourceMemory: tj.Spec.Resources.Memory,
					},
				},
			},
		},
		RestartPolicy: corev1.RestartPolicyNever,
	}
	js := batchv1.JobSpec{
		Suspend: new(true),
		Template: corev1.PodTemplateSpec{
			Spec: ps,
		},
	}
	return js
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrainingJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mlv1alpha1.TrainingJob{}).
		Named("trainingjob").
		Complete(r)
}
