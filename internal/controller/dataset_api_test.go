package controller

import (
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
)

var _ = Describe("Dataset API", func() {
	It("should accept valid spec", func() {
		spec := ValidSpec()
		validDataset := &mlv1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "valid-dataset",
				Namespace: "default",
			},
			Spec: spec,
		}
		Expect(k8sClient.Create(ctx, validDataset)).Should(Succeed())
		By("Cleanup the specific resource instance Dataset")
		Expect(k8sClient.Delete(ctx, validDataset)).To(Succeed())
	})
	It("should set default golden entry", func() {
		spec := ValidSpec()
		spec.Golden = nil
		resourceName := "valid-dataset"
		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		validDataset := &mlv1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{
				Name:      resourceName,
				Namespace: "default",
			},
			Spec: spec,
		}
		Expect(k8sClient.Create(ctx, validDataset)).Should(Succeed())
		By("missing golden entry should be set as default value false")
		Expect(k8sClient.Get(ctx, typeNamespacedName, validDataset)).Should(Succeed())
		Expect(*validDataset.Spec.Golden).To(BeFalse())
	})
	DescribeTable("should not accept invalid spec", func(mutate func(spec *mlv1alpha1.DatasetSpec)) {
		spec := ValidSpec()
		mutate(&spec)
		err := k8sClient.Create(ctx, &mlv1alpha1.Dataset{
			ObjectMeta: metav1.ObjectMeta{
				GenerateName: "invalid-",
				Namespace:    "default",
			},
			Spec: spec,
		})
		Expect(apierrors.IsInvalid(err)).Should(BeTrue(), "want Invalid from the API server, got %v", err)
	},
		Entry("missing uri", func(s *mlv1alpha1.DatasetSpec) { s.Uri = nil }),
		Entry("empty uri", func(s *mlv1alpha1.DatasetSpec) { s.Uri = ptr.To("") }),
		Entry("uri over MaxLength", func(s *mlv1alpha1.DatasetSpec) { s.Uri = ptr.To(strings.Repeat("x", 129)) }),

		Entry("missing owner", func(s *mlv1alpha1.DatasetSpec) { s.Owner = nil }),
		Entry("empty owner", func(s *mlv1alpha1.DatasetSpec) { s.Owner = ptr.To("") }),
		Entry("owner over MaxLength", func(s *mlv1alpha1.DatasetSpec) {
			s.Owner = ptr.To(strings.Repeat("x", 129))
		}),

		Entry("zero rows", func(s *mlv1alpha1.DatasetSpec) {
			s.Rows = ptr.To(0)
		}),

		Entry("empty checksum", func(s *mlv1alpha1.DatasetSpec) {
			s.Checksum = ptr.To("")
		}),
		Entry("checksum over MaxLength", func(s *mlv1alpha1.DatasetSpec) {
			s.Checksum = ptr.To(strings.Repeat("x", 257))
		}),
	)
})
