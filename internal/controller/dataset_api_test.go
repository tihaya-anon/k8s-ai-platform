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
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

// These tests assert the API contract, not the controller. Nothing here runs a
// reconciler: every rejection below comes from the API server enforcing the
// OpenAPI schema that controller-gen produced from the markers. That is the
// point — if a rule can be expressed as a marker, no Go code should be able to
// forget it.

var _ = Describe("Dataset API", func() {
	var counter int

	// A distinct name per spec, because envtest's API server is shared across
	// the whole suite and objects created here are never cleaned up.
	newName := func() string {
		counter++
		return fmt.Sprintf("api-test-%d", counter)
	}

	validDataset := func(name string) *mlv1alpha1.Dataset {
		return &mlv1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default"},
			Spec: mlv1alpha1.DatasetSpec{
				URI:   "s3://bucket/reviews/v3.parquet",
				Owner: "team-search",
			},
		}
	}

	Context("when the spec is valid", func() {
		It("accepts it and defaults golden to false", func() {
			ds := validDataset(newName())
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())

			fetched := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: ds.Name, Namespace: ds.Namespace,
			}, fetched)).To(Succeed())

			// The default came from the CRD schema, not from Go. A controller
			// reading this field never has to handle "unset".
			Expect(fetched.Spec.Golden).To(BeFalse())
			Expect(fetched.Status.Phase).To(BeEmpty())
		})

		It("accepts every supported URI scheme", func() {
			for _, uri := range []string{
				"s3://bucket/key",
				"gs://bucket/key",
				"pvc://datasets/reviews.parquet",
				"https://example.invalid/reviews.parquet",
			} {
				ds := validDataset(newName())
				ds.Spec.URI = uri
				Expect(k8sClient.Create(ctx, ds)).To(Succeed(), "uri %q should be accepted", uri)
			}
		})
	})

	Context("when the spec violates a marker", func() {
		DescribeTable("the API server rejects it",
			func(mutate func(*mlv1alpha1.Dataset)) {
				ds := validDataset(newName())
				mutate(ds)
				Expect(k8sClient.Create(ctx, ds)).NotTo(Succeed())
			},
			Entry("uri is empty", func(d *mlv1alpha1.Dataset) {
				d.Spec.URI = ""
			}),
			Entry("uri scheme is unknown", func(d *mlv1alpha1.Dataset) {
				d.Spec.URI = "ftp://bucket/key"
			}),
			Entry("uri has no scheme", func(d *mlv1alpha1.Dataset) {
				d.Spec.URI = "/mnt/data/reviews.parquet"
			}),
			Entry("owner is empty", func(d *mlv1alpha1.Dataset) {
				d.Spec.Owner = ""
			}),
			Entry("owner is not a DNS label", func(d *mlv1alpha1.Dataset) {
				d.Spec.Owner = "Team Search"
			}),
			Entry("rows is zero", func(d *mlv1alpha1.Dataset) {
				d.Spec.Rows = ptr.To(int64(0))
			}),
			Entry("rows is negative", func(d *mlv1alpha1.Dataset) {
				d.Spec.Rows = ptr.To(int64(-1))
			}),
			Entry("checksum is not sha256", func(d *mlv1alpha1.Dataset) {
				d.Spec.Checksum = "md5:0123456789abcdef"
			}),
			Entry("checksum is truncated", func(d *mlv1alpha1.Dataset) {
				d.Spec.Checksum = "sha256:abc123"
			}),
		)

		It("names the offending field in the error", func() {
			ds := validDataset(newName())
			ds.Spec.URI = "ftp://bucket/key"

			err := k8sClient.Create(ctx, ds)
			Expect(err).To(HaveOccurred())
			// The user sees this text, so it is part of the contract.
			Expect(err.Error()).To(ContainSubstring("spec.uri"))
		})
	})

	Context("the status subresource", func() {
		It("ignores status written through the main resource", func() {
			ds := validDataset(newName())
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())

			ds.Status.Phase = mlv1alpha1.DatasetPhaseReady
			Expect(k8sClient.Update(ctx, ds)).To(Succeed())

			fetched := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: ds.Name, Namespace: ds.Namespace,
			}, fetched)).To(Succeed())

			// The write was accepted and silently dropped. This is why a
			// controller that updates status with Update() instead of
			// Status().Update() fails without any error to debug.
			Expect(fetched.Status.Phase).To(BeEmpty())
		})

		It("ignores spec written through the status subresource", func() {
			ds := validDataset(newName())
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())

			ds.Spec.Owner = "team-impostor"
			ds.Status.Phase = mlv1alpha1.DatasetPhaseReady
			Expect(k8sClient.Status().Update(ctx, ds)).To(Succeed())

			fetched := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: ds.Name, Namespace: ds.Namespace,
			}, fetched)).To(Succeed())

			Expect(fetched.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseReady))
			Expect(fetched.Spec.Owner).To(Equal("team-search"))
		})

		It("does not bump the generation when only status changes", func() {
			ds := validDataset(newName())
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())
			generationAtCreate := ds.Generation

			ds.Status.Phase = mlv1alpha1.DatasetPhaseResolving
			Expect(k8sClient.Status().Update(ctx, ds)).To(Succeed())

			fetched := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: ds.Name, Namespace: ds.Namespace,
			}, fetched)).To(Succeed())

			// This is the property observedGeneration relies on: if status
			// writes bumped the generation, a controller comparing the two
			// would never converge.
			Expect(fetched.Generation).To(Equal(generationAtCreate))
		})

		It("rejects a phase outside the enum", func() {
			ds := validDataset(newName())
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())

			ds.Status.Phase = mlv1alpha1.DatasetPhase("Excellent")
			Expect(k8sClient.Status().Update(ctx, ds)).NotTo(Succeed())
		})
	})

	Context("conditions", func() {
		It("keeps one entry per type", func() {
			ds := validDataset(newName())
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())

			meta := func(reason string) metav1.Condition {
				return metav1.Condition{
					Type:               mlv1alpha1.DatasetConditionReady,
					Status:             metav1.ConditionFalse,
					Reason:             reason,
					Message:            "not yet resolved",
					ObservedGeneration: ds.Generation,
					LastTransitionTime: metav1.Now(),
				}
			}

			ds.Status.Conditions = []metav1.Condition{meta("Pending")}
			Expect(k8sClient.Status().Update(ctx, ds)).To(Succeed())

			ds.Status.Conditions = []metav1.Condition{meta("Resolving")}
			Expect(k8sClient.Status().Update(ctx, ds)).To(Succeed())

			fetched := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, types.NamespacedName{
				Name: ds.Name, Namespace: ds.Namespace,
			}, fetched)).To(Succeed())

			// listType=map keyed on type is what makes server-side apply merge
			// conditions by type instead of appending duplicates.
			Expect(fetched.Status.Conditions).To(HaveLen(1))
			Expect(fetched.Status.Conditions[0].Reason).To(Equal("Resolving"))
		})

		It("requires a reason", func() {
			ds := validDataset(newName())
			Expect(k8sClient.Create(ctx, ds)).To(Succeed())

			ds.Status.Conditions = []metav1.Condition{{
				Type:               mlv1alpha1.DatasetConditionReady,
				Status:             metav1.ConditionFalse,
				LastTransitionTime: metav1.Now(),
			}}
			// metav1.Condition carries its own markers; a condition without a
			// reason is refused by the schema, not by the controller.
			Expect(k8sClient.Status().Update(ctx, ds)).NotTo(Succeed())
		})
	})
})
