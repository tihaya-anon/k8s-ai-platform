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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

// TrainingJobReconciler reconciles a TrainingJob object
type TrainingJobReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
}

// ensure interface implementation
var _ reconcile.Reconciler = (*TrainingJobReconciler)(nil)

const (
	// queueNameLabel is Kueue's. Its batch/v1 integration selects on this label alone: a suspended
	// Job without it is one Kueue never looks at, and no Workload is ever created for it.
	queueNameLabel = "kueue.x-k8s.io/queue-name"

	// Identity carried onto the pods, so a running pod can be traced back without reading its
	// ownerReferences. The owner is an annotation rather than a label because a label value is
	// limited to 63 characters of [-_.alphanumeric] and TrainingJobSpec.Owner allows 128 of
	// anything -- an email address would make the Job unadmissible rather than merely unlabelled.
	labelTrainingJob = "ml.aiplatform.io/training-job"
	annotationOwner  = "ml.aiplatform.io/owner"

	// Event reasons are part of the controller's observable contract, same as condition reasons.
	reasonJobCreated = "JobCreated"

	trainerContainerName = "trainer"
)

// +kubebuilder:rbac:groups=events.k8s.io,resources=events,verbs=create;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      tj.Name,
			Namespace: tj.Namespace,
		},
	}

	op, err := ctrl.CreateOrUpdate(ctx, r.Client, job, func() error {
		// CreateOrUpdate has already loaded the live Job into `job` when one exists, so an empty
		// creationTimestamp is how this closure tells "about to create" from "already there".
		if !job.CreationTimestamp.IsZero() {
			// The name is derived, so an unrelated Job can already hold it. Adopting that one
			// would hand a stranger's pods this TrainingJob's ownerReference and delete them
			// along with it.
			if !metav1.IsControlledBy(job, &tj) {
				return fmt.Errorf("job %s/%s exists and is not controlled by this TrainingJob", job.Namespace, job.Name)
			}
			// Nothing below runs for an existing Job. Almost all of JobSpec is immutable after
			// creation, and the one field that is not -- suspend -- belongs to Kueue from the
			// moment the Job exists: rewriting the spec here would re-suspend a run Kueue had
			// already admitted. A TrainingJob spec edit therefore needs a new Job rather than an
			// update, which is what makes those spec fields a job for the admission webhook.
			return nil
		}

		job.Labels = map[string]string{
			queueNameLabel:   tj.Spec.Queue,
			labelTrainingJob: tj.Name,
		}
		job.Annotations = map[string]string{
			annotationOwner: tj.Spec.Owner,
		}
		job.Spec = batchV1JobSpec(&tj)

		// The ownerReference is what makes `kubectl delete trainingjob` reach the pods: it deletes
		// the Job, and the Job's Workload goes with it, because Kueue makes the Job that
		// Workload's owner in exactly the same way.
		return ctrl.SetControllerReference(&tj, job, r.Scheme)
	})
	if err != nil {
		// Name the layer: this is the API server refusing our write, not Kueue refusing quota and
		// not the training image failing.
		log.Error(err, "Job reconcile failed", "job", client.ObjectKeyFromObject(job))
		return ctrl.Result{}, err
	}

	if op == controllerutil.OperationResultCreated {
		r.Recorder.Eventf(&tj, job, corev1.EventTypeNormal, reasonJobCreated, "CreateJob",
			"Created suspended Job %s for LocalQueue %s", job.Name, tj.Spec.Queue)
	}

	return ctrl.Result{}, nil
}

// batchV1JobSpec lowers a TrainingJob onto the runtime that actually runs it. The Job is created
// suspended on purpose: a suspended Job has no pods, which is what leaves Kueue something to grant.
func batchV1JobSpec(tj *mlv1alpha1.TrainingJob) batchv1.JobSpec {
	// Resources.Accelerator is deliberately not lowered yet. A container resource name must be a
	// standard one or fully qualified -- the API server rejects the pod otherwise -- and
	// AcceleratorClassPlaceholder is neither. Turning a class into a real extended resource name
	// such as nvidia.com/gpu is task 5.1; until then a TrainingJob asking for an accelerator gets
	// a Job that does not, which Kueue will admit against cpu and memory alone.
	requests := corev1.ResourceList{
		corev1.ResourceCPU:    tj.Spec.Resources.CPU,
		corev1.ResourceMemory: tj.Spec.Resources.Memory,
	}

	return batchv1.JobSpec{
		Suspend: new(true),
		Template: corev1.PodTemplateSpec{
			ObjectMeta: metav1.ObjectMeta{
				// Pod labels, not a Job selector: the Job controller generates its own selector
				// from a controller-uid it owns, and honours a handwritten one only under
				// manualSelector. These are for humans and for `kubectl get pods -l`.
				Labels: map[string]string{
					labelTrainingJob: tj.Name,
				},
				Annotations: map[string]string{
					annotationOwner: tj.Spec.Owner,
				},
			},
			Spec: corev1.PodSpec{
				RestartPolicy: corev1.RestartPolicyNever,
				Containers: []corev1.Container{
					{
						Name:  trainerContainerName,
						Image: tj.Spec.Image,
						Resources: corev1.ResourceRequirements{
							Requests: requests,
						},
					},
				},
			},
		},
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *TrainingJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&mlv1alpha1.TrainingJob{}).
		// Owns, not Watches: the ownerReference set above is the index controller-runtime maps a
		// Job event back through, so Kueue flipping suspend wakes this reconciler.
		Owns(&batchv1.Job{}).
		Named("trainingjob").
		Complete(r)
}
