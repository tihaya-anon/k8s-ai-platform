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
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

// TrainingJobReconciler reconciles a TrainingJob object
type TrainingJobReconciler struct {
	client.Client
	Scheme   *runtime.Scheme
	Recorder events.EventRecorder
	// Finder locates the Kueue Workload for the child Job. Three implementations exist; which one
	// belongs here is task 1.2's Review beat. See workload_finder.go.
	Finder WorkloadFinder
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
		Name:      tj.Name,
		Namespace: tj.Namespace,
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

	// One status write per pass: compute every condition in memory first, so a partial failure
	// cannot leave a status that is half this reconcile and half the last one.
	conds := make([]metav1.Condition, 0, 3)
	conds = append(conds, metav1.Condition{
		Type:               conditionRendered,
		Status:             metav1.ConditionTrue,
		Reason:             reasonJobRendered,
		Message:            fmt.Sprintf("the platform created Job %s for this run", job.Name),
		ObservedGeneration: tj.Generation,
	})

	wl, findErr := r.Finder.Find(ctx, r.Client, job)
	if findErr != nil {
		// Naming the layer: failing to read the admission request is ours, not Kueue's refusal and
		// not the training code's. The run is left Pending rather than described wrongly.
		log.Error(findErr, "Workload lookup failed", "finder", r.Finder.Name(), "job", job.Name)
	}
	conds = append(conds, acceptedCondition(wl, tj.Generation), finishedCondition(job, tj.Generation))

	if err := r.writeStatus(ctx, &tj, job, conds); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, findErr
}

// writeStatus applies the computed conditions, derives the phase, and emits an Event when the phase
// moved. The previous phase is read off the object rather than remembered on the reconciler: a
// restart or a second replica must reach the same answer from the same cluster state.
func (r *TrainingJobReconciler) writeStatus(ctx context.Context, tj *mlv1alpha1.TrainingJob, job *batchv1.Job, conds []metav1.Condition) error {
	previous := tj.Status.Phase
	for _, c := range conds {
		apimeta.SetStatusCondition(&tj.Status.Conditions, c)
	}
	tj.Status.Phase = phaseFor(tj.Status.Conditions)
	tj.Status.JobRef = job.Name
	tj.Status.LastResolvedTime = metav1.Now()

	if err := r.Status().Update(ctx, tj); err != nil {
		return fmt.Errorf("updating TrainingJob status: %w", err)
	}

	if tj.Status.Phase == previous {
		return nil
	}
	accepted := apimeta.FindStatusCondition(tj.Status.Conditions, conditionAccepted)
	eventType := corev1.EventTypeNormal
	if tj.Status.Phase == mlv1alpha1.TrainingJobPhaseFailed {
		eventType = corev1.EventTypeWarning
	}
	message := string(tj.Status.Phase)
	if accepted != nil {
		message = accepted.Message
	}
	// events.k8s.io/v1 wants an action -- the verb the controller was performing -- with the state
	// it arrived at as the reason, which is what `kubectl get events` groups by.
	r.Recorder.Eventf(tj, job, eventType, string(tj.Status.Phase), "Reconcile",
		"TrainingJob %s: %s", tj.Status.Phase, message)
	return nil
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

// +kubebuilder:rbac:groups=kueue.x-k8s.io,resources=workloads,verbs=get;list;watch

// SetupWithManager sets up the controller with the Manager.
func (r *TrainingJobReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := r.Finder.SetupWithManager(context.Background(), mgr); err != nil {
		return fmt.Errorf("setting up the %s workload finder: %w", r.Finder.Name(), err)
	}
	return ctrl.NewControllerManagedBy(mgr).
		For(&mlv1alpha1.TrainingJob{}).
		// Owns, not Watches: the ownerReference set above is the index controller-runtime maps a
		// Job event back through, so Kueue flipping suspend wakes this reconciler.
		Owns(&batchv1.Job{}).
		// The Workload is a grandchild -- TrainingJob -> Job -> Workload -- and Owns matches direct
		// controller references only. Without this watch a run that cannot be admitted reconciles
		// once and goes quiet, because a refusal updates the Workload and leaves the Job untouched.
		Watches(&kueuev1beta2.Workload{}, handler.EnqueueRequestsFromMapFunc(r.trainingJobForWorkload)).
		Named("trainingjob").
		Complete(r)
}

// trainingJobForWorkload walks Workload -> Job -> TrainingJob by ownerReference rather than by
// assuming the names match. The naming is this controller's own convention and could change; the
// ownerReferences are what the garbage collector uses and cannot.
func (r *TrainingJobReconciler) trainingJobForWorkload(ctx context.Context, obj client.Object) []reconcile.Request {
	owner := metav1.GetControllerOf(obj)
	if owner == nil || owner.Kind != "Job" {
		return nil
	}
	var job batchv1.Job
	if err := r.Get(ctx, types.NamespacedName{Name: owner.Name, Namespace: obj.GetNamespace()}, &job); err != nil {
		return nil
	}
	jobOwner := metav1.GetControllerOf(&job)
	if jobOwner == nil || jobOwner.Kind != "TrainingJob" {
		return nil
	}
	return []reconcile.Request{{
		Name:      jobOwner.Name,
		Namespace: job.Namespace}}
}
