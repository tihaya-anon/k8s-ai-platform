package controller

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

var _ = Describe("Dataset API", func() {
	It("should accept valid spec", func() {
		spec := ValidDatasetSpec()
		validDataset := &mlv1alpha1.Dataset{
			Name:      "valid-dataset",
			Namespace: DefaultNamespace,
			Spec:      spec,
		}
		Expect(k8sClient.Create(ctx, validDataset)).Should(Succeed())
		By("Cleanup the specific resource instance Dataset")
		Expect(k8sClient.Delete(ctx, validDataset)).To(Succeed())
	})
	It("should set default golden entry", func() {
		spec := ValidDatasetSpec()
		spec.Golden = nil
		resourceName := "valid-dataset"
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: DefaultNamespace,
		}
		validDataset := &mlv1alpha1.Dataset{
			Name:      resourceName,
			Namespace: DefaultNamespace,
			Spec:      spec,
		}
		Expect(k8sClient.Create(ctx, validDataset)).Should(Succeed())
		By("missing golden entry should be set as default value false")
		Expect(k8sClient.Get(ctx, typeNamespacedName, validDataset)).Should(Succeed())
		Expect(*validDataset.Spec.Golden).To(BeFalse())
	})
	DescribeTable("should not accept invalid spec", func(mutate func(spec *mlv1alpha1.DatasetSpec)) {
		spec := ValidDatasetSpec()
		mutate(&spec)
		err := k8sClient.Create(ctx, &mlv1alpha1.Dataset{
			GenerateName: "invalid-",
			Namespace:    DefaultNamespace,
			Spec:         spec,
		})
		Expect(apierrors.IsInvalid(err)).Should(BeTrue(), "want Invalid from the API server, got %v", err)
	},
		Entry("empty uri", func(s *mlv1alpha1.DatasetSpec) { s.Uri = "" }),
		Entry("uri over MaxLength", func(s *mlv1alpha1.DatasetSpec) { s.Uri = strings.Repeat("x", 129) }),

		Entry("empty owner", func(s *mlv1alpha1.DatasetSpec) { s.Owner = "" }),
		Entry("owner over MaxLength", func(s *mlv1alpha1.DatasetSpec) {
			s.Owner = strings.Repeat("x", 129)
		}),

		Entry("zero rows", func(s *mlv1alpha1.DatasetSpec) {
			s.Rows = new(int64(0))
		}),

		Entry("empty checksum", func(s *mlv1alpha1.DatasetSpec) {
			s.Checksum = new("")
		}),
		Entry("checksum over MaxLength", func(s *mlv1alpha1.DatasetSpec) {
			s.Checksum = new(strings.Repeat("x", 257))
		}),
	)
})
