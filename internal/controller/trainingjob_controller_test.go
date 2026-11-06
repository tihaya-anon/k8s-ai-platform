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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

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
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			controllerReconciler := &TrainingJobReconciler{
				Client: k8sClient,
				Scheme: k8sClient.Scheme(),
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
			// TODO(user): Add more specific assertions depending on your controller's reconciliation logic.
			// Example: If you expect a certain status condition after reconciliation, verify it here.
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
