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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/tihaya-anon/k8s-ai-platform/internal/artifact"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

var _ = Describe("Dataset Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		dataset := &mlv1alpha1.Dataset{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Dataset")
			err := k8sClient.Get(ctx, typeNamespacedName, dataset)
			if err != nil && errors.IsNotFound(err) {
				resource := &mlv1alpha1.Dataset{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: mlv1alpha1.DatasetSpec{
						Uri:      ptr.To("sample.uri"),
						Owner:    ptr.To("sample.owner"),
						Rows:     ptr.To(1),
						Checksum: ptr.To("placeholder"),
						Golden:   ptr.To(false),
					},
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &mlv1alpha1.Dataset{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Dataset")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should record resolved metadata and go Ready", func() {
			By("Reconciling the created resource")
			controllerReconciler := &DatasetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
				Resolver: &artifact.FakeResolver{Entries: map[string]artifact.Metadata{
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
				Resolver: &artifact.FakeResolver{Err: errors.NewServiceUnavailable("store down")},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			failed := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, failed)).To(Succeed())
			Expect(failed.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseFailed))
			condition := apimeta.FindStatusCondition(failed.Status.Conditions, "Resolved")
			Expect(condition).NotTo(BeNil())
			Expect(condition.Reason).To(Equal("ArtifactUnreachable"))
			Expect(condition.ObservedGeneration).To(Equal(failed.Generation))
		})

		It("should go Failed when the artifact resolves to no rows", func() {
			By("resolving an artifact the store can read but nothing can train on")
			controllerReconciler := &DatasetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: record.NewFakeRecorder(10),
				Resolver: &artifact.FakeResolver{Entries: map[string]artifact.Metadata{
					"sample.uri": {Rows: 0, Checksum: "placeholder"},
				}},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred(), "an empty artifact is a reportable state, not a reconcile failure")

			empty := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, empty)).To(Succeed())
			Expect(empty.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseFailed))

			condition := apimeta.FindStatusCondition(empty.Status.Conditions, "Resolved")
			Expect(condition).NotTo(BeNil())
			By("naming the artifact as the layer at fault, not the registrant's claim")
			Expect(condition.Reason).To(Equal("ArtifactEmpty"))

			By("recording no row count, which Minimum=1 would have rejected")
			Expect(empty.Status.ObservedRows).To(BeNil())
			Expect(empty.Status.ObservedChecksum).To(HaveValue(Equal("placeholder")))
		})

		It("should clear a row count that an emptied artifact has invalidated", func() {
			reconcileWith := func(rows int) {
				controllerReconciler := &DatasetReconciler{
					Client:   k8sClient,
					Scheme:   k8sClient.Scheme(),
					Recorder: record.NewFakeRecorder(10),
					Resolver: &artifact.FakeResolver{Entries: map[string]artifact.Metadata{
						"sample.uri": {Rows: rows, Checksum: "placeholder"},
					}},
				}
				_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
					NamespacedName: typeNamespacedName,
				})
				Expect(err).NotTo(HaveOccurred())
			}

			By("observing a usable artifact first")
			reconcileWith(1)
			ready := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, ready)).To(Succeed())
			Expect(ready.Status.ObservedRows).To(HaveValue(Equal(1)))

			By("resolving the same uri after the artifact was emptied")
			reconcileWith(0)
			emptied := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, emptied)).To(Succeed())
			Expect(emptied.Status.ObservedRows).To(BeNil(), "a stale count would claim an observation this pass contradicts")
			Expect(emptied.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseFailed))
		})

		It("should go Failed when frozen fields are modified", func() {})
	})
})
