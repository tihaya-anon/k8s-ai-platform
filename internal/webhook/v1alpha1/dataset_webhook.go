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
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apivalidation "k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var datasetlog = logf.Log.WithName("dataset-resource")

// SetupDatasetWebhookWithManager registers the webhook for Dataset in the manager.
func SetupDatasetWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).For(&mlv1alpha1.Dataset{}).
		WithValidator(&DatasetCustomValidator{}).
		WithDefaulter(&DatasetCustomDefaulter{}).
		Complete()
}

// TODO(user): EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// +kubebuilder:webhook:path=/mutate-ml-aiplatform-io-v1alpha1-dataset,mutating=true,failurePolicy=fail,sideEffects=None,groups=ml.aiplatform.io,resources=datasets,verbs=create;update,versions=v1alpha1,name=mdataset-v1alpha1.kb.io,admissionReviewVersions=v1

// DatasetCustomDefaulter struct is responsible for setting default values on the custom resource of the
// Kind Dataset when those are created or updated.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as it is used only for temporary operations and does not need to be deeply copied.
type DatasetCustomDefaulter struct {
	// TODO(user): Add more fields as needed for defaulting
}

var _ webhook.CustomDefaulter = &DatasetCustomDefaulter{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind Dataset.
func (d *DatasetCustomDefaulter) Default(_ context.Context, obj runtime.Object) error {
	dataset, ok := obj.(*mlv1alpha1.Dataset)

	if !ok {
		return fmt.Errorf("expected an Dataset object but got %T", obj)
	}
	datasetlog.Info("Defaulting for Dataset", "name", dataset.GetName())

	// TODO(user): fill in your defaulting logic.

	return nil
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
// NOTE: The 'path' attribute must follow a specific pattern and should not be modified directly here.
// Modifying the path for an invalid path can cause API server errors; failing to locate the webhook.
// +kubebuilder:webhook:path=/validate-ml-aiplatform-io-v1alpha1-dataset,mutating=false,failurePolicy=fail,sideEffects=None,groups=ml.aiplatform.io,resources=datasets,verbs=create;update,versions=v1alpha1,name=vdataset-v1alpha1.kb.io,admissionReviewVersions=v1

// DatasetCustomValidator struct is responsible for validating the Dataset resource
// when it is created, updated, or deleted.
//
// NOTE: The +kubebuilder:object:generate=false marker prevents controller-gen from generating DeepCopy methods,
// as this struct is used only for temporary operations and does not need to be deeply copied.
type DatasetCustomValidator struct {
	// TODO(user): Add more fields as needed for validation
}

var _ webhook.CustomValidator = &DatasetCustomValidator{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type Dataset.
func (v *DatasetCustomValidator) ValidateCreate(_ context.Context, obj runtime.Object) (admission.Warnings, error) {
	dataset, ok := obj.(*mlv1alpha1.Dataset)
	if !ok {
		return nil, fmt.Errorf("expected a Dataset object but got %T", obj)
	}
	datasetlog.Info("Validation for Dataset upon creation", "name", dataset.GetName())

	// TODO(user): fill in your validation logic upon object creation.

	return nil, nil
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type Dataset.
func (v *DatasetCustomValidator) ValidateUpdate(_ context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	dataset, ok := newObj.(*mlv1alpha1.Dataset)
	if !ok {
		return nil, fmt.Errorf("expected a Dataset object for the newObj but got %T", newObj)
	}
	datasetlog.Info("Validation for Dataset upon update", "name", dataset.GetName())

	oldDataset, ok := oldObj.(*mlv1alpha1.Dataset)
	if !ok {
		return nil, fmt.Errorf("expected a Dataset object for the oldObj but got %T", oldObj)
	}

	// A Dataset is lineage: what a model was trained on must still be true after
	// the fact, so the registered artifact and the claims about it are frozen.
	// Everything else about the object stays editable.
	var frozen field.ErrorList
	frozen = append(frozen, apivalidation.ValidateImmutableField(
		dataset.Spec.Uri, oldDataset.Spec.Uri, field.NewPath("spec", "uri"))...)
	frozen = append(frozen, apivalidation.ValidateImmutableField(
		dataset.Spec.Rows, oldDataset.Spec.Rows, field.NewPath("spec", "rows"))...)
	frozen = append(frozen, apivalidation.ValidateImmutableField(
		dataset.Spec.Checksum, oldDataset.Spec.Checksum, field.NewPath("spec", "checksum"))...)

	if len(frozen) == 0 {
		return nil, nil
	}

	// NewInvalid carries APIStatus, which controller-runtime turns into a 422 with
	// one cause per field. A bare aggregate would reach the user as a flat 403.
	return nil, apierrors.NewInvalid(
		mlv1alpha1.GroupVersion.WithKind(mlv1alpha1.DatasetKind).GroupKind(), dataset.Name, frozen)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type Dataset.
func (v *DatasetCustomValidator) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	dataset, ok := obj.(*mlv1alpha1.Dataset)
	if !ok {
		return nil, fmt.Errorf("expected a Dataset object but got %T", obj)
	}
	datasetlog.Info("Validation for Dataset upon deletion", "name", dataset.GetName())

	// TODO(user): fill in your validation logic upon object deletion.

	return nil, nil
}
