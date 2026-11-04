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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	DatasetKind     = "Dataset"
	DatasetListKind = "DatasetList"
)

// DatasetSpec defines the desired state of Dataset.
type DatasetSpec struct {
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +required

	// Uri non-empty dataset artifact location
	Uri *string `json:"uri"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +required

	// Owner non-empty human or team owner used for accountability and incident routing
	Owner *string `json:"owner"`

	// +kubebuilder:validation:Minimum=1
	// +optional

	// Rows claimed positive row count supplied by the registrant
	Rows *int `json:"rows,omitempty"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional

	// Checksum claimed artifact checksum supplied by the registrant
	Checksum *string `json:"checksum,omitempty"`

	// +default=false

	// Golden whether the dataset is approved for evaluation gates; default false
	Golden *bool `json:"golden"`
}

type DatasetPhase string

const (
	DatasetPhasePending DatasetPhase = "Pending"
	DatasetPhaseReady   DatasetPhase = "Ready"
	DatasetPhaseFailed  DatasetPhase = "Failed"
)

// DatasetStatus defines the observed state of Dataset.
type DatasetStatus struct {
	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// +listType=map
	// +listMapKey=type
	// +optional

	// Conditions represent the current state of the Dataset resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	// +required

	// Phase short display summary, one of Pending, Ready, or Failed
	Phase DatasetPhase `json:"phase"`

	// +kubebuilder:validation:Minimum=1
	// +optional

	// ObservedRows metadata resolved by the controller. Absent means the last
	// pass observed no usable row count: a zero-row artifact is unusable, so the
	// controller reports ArtifactEmpty instead of recording the observation.
	ObservedRows *int `json:"observedRows,omitempty"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional

	// ObservedChecksum metadata resolved by the controller
	ObservedChecksum *string `json:"observedChecksum,omitempty"`

	// +optional

	// LastResolvedTime timestamp of the last successful metadata resolution
	LastResolvedTime metav1.Time `json:"lastResolvedTime,omitempty"`
}

// All of this type's markers sit in one block, and above the doc comment rather than inside it.
// controller-gen looks at the godoc comment and at the one comment block before it, so a second
// marker block further up would be silently dropped -- and keeping them out of the godoc comment
// is what stops `go doc` and editor hovers from leading with six lines of printcolumn.
//
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=datasets,singular=dataset,shortName=dset,categories=all,scope=Namespaced
// +kubebuilder:printcolumn:name=Owner,type=string,JSONPath=`.spec.owner`
// +kubebuilder:printcolumn:name=ExpectedRows,type=integer,JSONPath=`.spec.rows`
// +kubebuilder:printcolumn:name=Golden,type=boolean,JSONPath=`.spec.golden`
// +kubebuilder:printcolumn:name=Phase,type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`

// Dataset is the Schema for the datasets API
type Dataset struct {
	metav1.TypeMeta `json:",inline"`

	// +optional

	// metadata is a standard object metadata
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +required

	// spec defines the desired state of Dataset
	Spec DatasetSpec `json:"spec"`

	// +optional

	// status defines the observed state of Dataset
	Status DatasetStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// DatasetList contains a list of Dataset
type DatasetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Dataset `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &Dataset{}, &DatasetList{})
		return nil
	})
}
