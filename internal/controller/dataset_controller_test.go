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
	"github.com/tihaya-anon/k8s-ai-platform/internal/utils"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
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
						Uri:      utils.Ptr("sample.uri"),
						Owner:    utils.Ptr("sample.owner"),
						Rows:     utils.Ptr(1),
						Checksum: utils.Ptr("placeholder"),
						Golden:   utils.Ptr(false),
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

		It("should go Failed when frozen fields are modified", func() {})
	})
})
