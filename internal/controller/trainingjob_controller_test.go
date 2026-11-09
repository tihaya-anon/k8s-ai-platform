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
	"errors"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	batchv1 "k8s.io/api/batch/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

var _ = Describe("TrainingJob Controller", func() {
	Context("When reconciling a resource", func() {
		const (
			resourceName      = "test-resource"
			resourceNamespace = "default"
		)

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: resourceNamespace,
		}
		trainingjob := &mlv1alpha1.TrainingJob{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind TrainingJob")
			err := k8sClient.Get(ctx, typeNamespacedName, trainingjob)
			if err != nil && apierrors.IsNotFound(err) {
				resource := &mlv1alpha1.TrainingJob{
					Name:      resourceName,
					Namespace: resourceNamespace,
					Spec:      ValidTrainingJobSpec(),
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			// TODO(user): Cleanup logic after each test, like removing the resource instance.
			resource := &mlv1alpha1.TrainingJob{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance TrainingJob")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())

			// envtest runs no garbage collector, so the ownerReference deletes nothing here and the
			// next case would reconcile against the previous one's Job.
			job := &batchv1.Job{Name: resourceName, Namespace: resourceNamespace}
			// Background propagation on purpose: deleting a Job defaults to Orphan, which makes the
			// API server add an `orphan` finalizer for the garbage collector to clear -- and
			// envtest has no garbage collector, so the Job would sit terminating forever.
			Expect(client.IgnoreNotFound(k8sClient.Delete(ctx, job, client.PropagationPolicy(metav1.DeletePropagationBackground)))).To(Succeed())
			// And wait for it: a Job still terminating when the next case starts is owned by the
			// previous TrainingJob's UID, so the adoption guard would reject it.
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, &batchv1.Job{}))
			}).Should(BeTrue())
			Eventually(func() bool {
				return apierrors.IsNotFound(k8sClient.Get(ctx, typeNamespacedName, &mlv1alpha1.TrainingJob{}))
			}).Should(BeTrue())
		})
		// The finder is stubbed here on purpose. envtest installs this project's CRDs and not
		// Kueue's, so there is no Workload kind to list; the three real strategies have their own
		// contract tests against a fake client in workload_finder_test.go. These cases are about
		// what the platform renders, which is the half that needs an API server.
		reconcilerWith := func(f WorkloadFinder) *TrainingJobReconciler {
			return &TrainingJobReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: events.NewFakeRecorder(10),
				Finder:   f,
			}
		}
		reconcileOnce := func() {
			_, err := reconcilerWith(stubFinder{}).Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		}
		childJob := func() *batchv1.Job {
			job := &batchv1.Job{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, job)).To(Succeed())
			return job
		}

		It("renders exactly one suspended, owned, queue-labelled Job", func() {
			reconcileOnce()

			jobs := &batchv1.JobList{}
			Expect(k8sClient.List(ctx, jobs, client.InNamespace(resourceNamespace))).To(Succeed())
			Expect(jobs.Items).To(HaveLen(1), "a TrainingJob renders one child Job and no more")

			job := jobs.Items[0]
			Expect(job.Name).To(Equal(resourceName))
			Expect(job.Spec.Suspend).To(HaveValue(BeTrue()),
				"an unsuspended Job starts immediately and quota means nothing")
			Expect(job.Labels).To(HaveKeyWithValue(queueNameLabel, ValidTrainingJobSpec().Queue),
				"without this label Kueue never looks at the Job")

			owner := metav1.GetControllerOf(&job)
			Expect(owner).NotTo(BeNil(), "deleting the TrainingJob must reach the pods")
			Expect(owner.Kind).To(Equal("TrainingJob"))
			Expect(owner.Name).To(Equal(resourceName))

			// Kueue does arithmetic on these and nothing else; a request that does not land on the
			// pod template is quota that cannot be charged.
			requests := job.Spec.Template.Spec.Containers[0].Resources.Requests
			want := ValidTrainingJobSpec().Resources
			wantCPU, wantMemory := want.CPU, want.Memory
			Expect(requests.Cpu().String()).To(Equal(wantCPU.String()))
			Expect(requests.Memory().String()).To(Equal(wantMemory.String()))
		})

		It("reports Rendered and an honest Accepted before Kueue has acted", func() {
			reconcileOnce()

			tj := &mlv1alpha1.TrainingJob{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, tj)).To(Succeed())

			rendered := apimeta.FindStatusCondition(tj.Status.Conditions, conditionRendered)
			Expect(rendered).NotTo(BeNil())
			Expect(rendered.Status).To(Equal(metav1.ConditionTrue))

			accepted := apimeta.FindStatusCondition(tj.Status.Conditions, conditionAccepted)
			Expect(accepted).NotTo(BeNil())
			Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
			Expect(accepted.Reason).To(Equal(reasonAwaitingAdmission))

			Expect(tj.Status.Phase).To(Equal(mlv1alpha1.TrainingJobPhasePending))
			Expect(tj.Status.JobRef).To(Equal(resourceName))
		})

		It("does not re-suspend a Job that Kueue has admitted", func() {
			reconcileOnce()

			// Stand in for Kueue: flip the one field that belongs to it.
			job := childJob()
			job.Spec.Suspend = new(false)
			Expect(k8sClient.Update(ctx, job)).To(Succeed())

			reconcileOnce()
			reconcileOnce()

			Expect(childJob().Spec.Suspend).To(HaveValue(BeFalse()),
				"re-applying the rendered spec would un-admit a running job on every pass")
		})

		It("refuses to adopt a Job it does not control", func() {
			stranger := &batchv1.Job{
				Name: resourceName, Namespace: resourceNamespace,
				Spec: batchV1JobSpec(&mlv1alpha1.TrainingJob{Spec: ValidTrainingJobSpec()}),
			}
			Expect(k8sClient.Create(ctx, stranger)).To(Succeed())

			_, err := reconcilerWith(stubFinder{}).Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred(),
				"adopting it would hand a stranger's pods this TrainingJob's ownerReference")
			Expect(err.Error()).To(ContainSubstring("not controlled by this TrainingJob"))

			Expect(metav1.GetControllerOf(childJob())).To(BeNil(), "the stranger keeps its own owner")
		})

		It("surfaces a lookup failure without describing the run wrongly", func() {
			_, err := reconcilerWith(failingFinder{}).Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).To(HaveOccurred(), "a failed lookup is retried, not swallowed")

			tj := &mlv1alpha1.TrainingJob{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, tj)).To(Succeed())
			accepted := apimeta.FindStatusCondition(tj.Status.Conditions, conditionAccepted)
			Expect(accepted.Status).To(Equal(metav1.ConditionFalse))
			Expect(tj.Status.Phase).To(Equal(mlv1alpha1.TrainingJobPhasePending),
				"an unreadable admission state leaves the run Pending, never Running")
		})

		DescribeTable("shoud not accept invalid spec", func(mutate func(spec *mlv1alpha1.TrainingJobSpec)) {
			spec := ValidTrainingJobSpec()
			mutate(&spec)
			err := k8sClient.Create(ctx, &mlv1alpha1.TrainingJob{
				GenerateName: "invalid-",
				Namespace:    DefaultNamespace,
				Spec:         spec,
			})
			Expect(apierrors.IsInvalid(err)).Should(BeTrue(), "want Invalid from the API server, got %v", err)
		},
			Entry("empty owner", func(s *mlv1alpha1.TrainingJobSpec) {
				s.Owner = ""
			}),
			Entry("owner over MaxLength", func(s *mlv1alpha1.TrainingJobSpec) {
				s.Owner = strings.Repeat("x", 129)
			}),

			Entry("empty dataset", func(s *mlv1alpha1.TrainingJobSpec) {
				s.Dataset = ""
			}),
			Entry("dataset over MaxLength", func(s *mlv1alpha1.TrainingJobSpec) {
				s.Dataset = strings.Repeat("x", 254)
			}),

			Entry("empty baseModel", func(s *mlv1alpha1.TrainingJobSpec) {
				s.BaseModel = ""
			}),
			Entry("baseModel over maxLength", func(s *mlv1alpha1.TrainingJobSpec) {
				s.BaseModel = strings.Repeat("x", 257)
			}),

			Entry("empty image", func(s *mlv1alpha1.TrainingJobSpec) {
				s.Image = ""
			}),
			Entry("image over MaxLength", func(s *mlv1alpha1.TrainingJobSpec) {
				s.Image = strings.Repeat("x", 257)
			}),

			Entry("empty queue", func(s *mlv1alpha1.TrainingJobSpec) {
				s.Queue = ""
			}),
			Entry("queue over MaxLength", func(s *mlv1alpha1.TrainingJobSpec) {
				s.Queue = strings.Repeat("x", 254)
			}),
		)
	})
})

// stubFinder stands in for Kueue not having acted yet, which is the state every rendering case
// above is asserted against.
type stubFinder struct{}

func (stubFinder) Name() string { return "stub" }

func (stubFinder) SetupWithManager(context.Context, ctrl.Manager) error { return nil }

func (stubFinder) Find(context.Context, client.Client, *batchv1.Job) (*kueuev1beta2.Workload, error) {
	return nil, nil
}

// failingFinder stands in for the lookup itself failing -- RBAC denied, cache not started, two
// Workloads for one Job -- which is the platform's own layer and not Kueue refusing quota.
type failingFinder struct{}

func (failingFinder) Name() string { return "failing" }

func (failingFinder) SetupWithManager(context.Context, ctrl.Manager) error { return nil }

func (failingFinder) Find(context.Context, client.Client, *batchv1.Job) (*kueuev1beta2.Workload, error) {
	return nil, errors.New("workload lookup is broken")
}
