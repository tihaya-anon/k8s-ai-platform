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

package v1alpha1

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
	// TODO (user): Add any additional imports if needed
)

var _ = Describe("Dataset Webhook", func() {
	var (
		obj       *mlv1alpha1.Dataset
		oldObj    *mlv1alpha1.Dataset
		validator DatasetValidator
		defaulter DatasetDefaulter
	)

	BeforeEach(func() {
		obj = &mlv1alpha1.Dataset{}
		oldObj = &mlv1alpha1.Dataset{}
		validator = DatasetValidator{}
		Expect(validator).NotTo(BeNil(), "Expected validator to be initialized")
		defaulter = DatasetDefaulter{}
		Expect(defaulter).NotTo(BeNil(), "Expected defaulter to be initialized")
		Expect(oldObj).NotTo(BeNil(), "Expected oldObj to be initialized")
		Expect(obj).NotTo(BeNil(), "Expected obj to be initialized")
		// TODO (user): Add any setup logic common to all tests
	})

	AfterEach(func() {
		// TODO (user): Add any teardown logic common to all tests
	})

	Context("When creating Dataset under Validating Webhook", func() {
		// The warning is advisory by construction, so every spec here asserts that
		// the create is still admitted.
		newValidator := func(existing ...client.Object) DatasetValidator {
			return DatasetValidator{
				Client: fake.NewClientBuilder().WithScheme(scheme.Scheme).WithObjects(existing...).Build(),
			}
		}
		registered := func(name, uri string, checksum *string) *mlv1alpha1.Dataset {
			return &mlv1alpha1.Dataset{
				Name: name, Namespace: "default",
				Spec: mlv1alpha1.DatasetSpec{
					Uri:      uri,
					Owner:    "owner",
					Golden:   new(false),
					Checksum: checksum,
				},
			}
		}

		BeforeEach(func() {
			obj = registered("new", "s3://bucket/train.parquet", new("sha256:a"))
		})

		It("Should admit a URI nobody has registered", func() {
			validator = newValidator(registered("other", "s3://bucket/eval.parquet", new("sha256:b")))
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(BeEmpty())
		})

		It("Should warn, not reject, when the same artifact is registered twice", func() {
			validator = newValidator(registered("first", "s3://bucket/train.parquet", new("sha256:a")))
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred(), "a duplicate URI is advisory, never an error")
			Expect(warnings).To(ConsistOf(ContainSubstring("already registered by Dataset first")))
			Expect(warnings[0]).To(ContainSubstring("same spec.checksum"))
		})

		It("Should say which claim is in doubt when the checksums disagree", func() {
			validator = newValidator(registered("first", "s3://bucket/train.parquet", new("sha256:different")))
			warnings, err := validator.ValidateCreate(ctx, obj)
			Expect(err).NotTo(HaveOccurred())
			Expect(warnings).To(ConsistOf(ContainSubstring("different spec.checksum")))
		})

		It("Should not warn about a Dataset in another namespace", func() {
			elsewhere := registered("first", "s3://bucket/train.parquet", new("sha256:a"))
			elsewhere.Namespace = "other"
			validator = newValidator(elsewhere)
			Expect(validator.ValidateCreate(ctx, obj)).To(BeEmpty())
		})

		It("Should not warn about the object being created", func() {
			validator = newValidator(registered("new", "s3://bucket/train.parquet", new("sha256:a")))
			Expect(validator.ValidateCreate(ctx, obj)).To(BeEmpty())
		})
	})

	Context("When creating Dataset under Defaulting Webhook", func() {
		// TODO (user): Add logic for defaulting webhooks
		// Example:
		// It("Should apply defaults when a required field is empty", func() {
		//     By("simulating a scenario where defaults should be applied")
		//     obj.SomeFieldWithDefault = ""
		//     By("calling the Default method to apply defaults")
		//     defaulter.Default(ctx, obj)
		//     By("checking that the default values are set")
		//     Expect(obj.SomeFieldWithDefault).To(Equal("default_value"))
		// })
	})

	Context("When creating or updating Dataset under Validating Webhook", func() {
		// Example:
		// It("Should deny creation if a required field is missing", func() {
		//     By("simulating an invalid creation scenario")
		//     obj.SomeRequiredField = ""
		//     Expect(validator.ValidateCreate(ctx, obj)).Error().To(HaveOccurred())
		// })
		//
		// It("Should admit creation if all required fields are present", func() {
		//     By("simulating an invalid creation scenario")
		//     obj.SomeRequiredField = "valid_value"
		//     Expect(validator.ValidateCreate(ctx, obj)).To(BeNil())
		// })
		//
		It("Should validate updates correctly", func() {
			By("simulating a valid update scenario")
			oldObj.Spec.Owner = "old_owner"
			obj.Spec.Owner = "new_owner"
			Expect(validator.ValidateUpdate(ctx, oldObj, obj)).To(BeNil())
			oldObj.Spec.Golden = new(false)
			obj.Spec.Golden = new(true)
			Expect(validator.ValidateUpdate(ctx, oldObj, obj)).To(BeNil())
		})

		DescribeTable("Should reject frozen field modification",
			func(mutate func(oldSpec, newSpec *mlv1alpha1.DatasetSpec), wantFields ...string) {
				By("simulating an invalid update scenario")
				mutate(&oldObj.Spec, &obj.Spec)

				_, err := validator.ValidateUpdate(ctx, oldObj, obj)

				By("rejecting it as a 422 with one cause per frozen field")
				Expect(apierrors.IsInvalid(err)).To(BeTrue(), "expected a StatusError the API server reports as Invalid, got %v", err)
				var statusErr *apierrors.StatusError
				Expect(errors.As(err, &statusErr)).To(BeTrue())

				gotFields := make([]string, 0, len(statusErr.ErrStatus.Details.Causes))
				for _, cause := range statusErr.ErrStatus.Details.Causes {
					Expect(cause.Type).To(Equal(metav1.CauseTypeFieldValueInvalid))
					gotFields = append(gotFields, cause.Field)
				}
				Expect(gotFields).To(ConsistOf(wantFields))

				By("telling the user what to do instead")
				Expect(err).To(MatchError(ContainSubstring("Do not modify the immutable fields, create a new Dataset instead")))
				Expect(err).To(MatchError(ContainSubstring("field is immutable")))
			},
			Entry("uri changed", func(oldSpec, newSpec *mlv1alpha1.DatasetSpec) {
				oldSpec.Uri = "old_uri"
				newSpec.Uri = "new_uri"
			}, "spec.uri"),
			Entry("rows changed", func(oldSpec, newSpec *mlv1alpha1.DatasetSpec) {
				oldSpec.Rows = new(int64(100))
				newSpec.Rows = new(int64(200))
			}, "spec.rows"),
			Entry("checksum changed", func(oldSpec, newSpec *mlv1alpha1.DatasetSpec) {
				oldSpec.Checksum = new("sha256:old")
				newSpec.Checksum = new("sha256:new")
			}, "spec.checksum"),
			// An optional field that was absent at registration is still a claim about
			// the artifact once supplied, so nil -> set is a change like any other.
			Entry("rows supplied after registration", func(oldSpec, newSpec *mlv1alpha1.DatasetSpec) {
				newSpec.Rows = new(int64(100))
			}, "spec.rows"),
			Entry("checksum dropped after registration", func(oldSpec, newSpec *mlv1alpha1.DatasetSpec) {
				oldSpec.Checksum = new("sha256:old")
			}, "spec.checksum"),
			Entry("all three changed at once", func(oldSpec, newSpec *mlv1alpha1.DatasetSpec) {
				oldSpec.Uri = "old_uri"
				newSpec.Uri = "new_uri"
				oldSpec.Rows = new(int64(100))
				newSpec.Rows = new(int64(200))
				oldSpec.Checksum = new("sha256:old")
				newSpec.Checksum = new("sha256:new")
			}, "spec.uri", "spec.rows", "spec.checksum"),
		)
	})

})
