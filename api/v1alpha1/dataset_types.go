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
)

const (
	DatasetKind     = "Dataset"
	DatasetListKind = "DatasetList"
)

// DatasetSpec defines the desired state of Dataset.
type DatasetSpec struct {
	// Uri non-empty dataset artifact location
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +required
	Uri *string `json:"uri"`

	// Owner non-empty human or team owner used for accountability and incident routing
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +required
	Owner *string `json:"owner"`

	// Rows claimed positive row count supplied by the registrant
	// +kubebuilder:validation:Minimum=1
	// +optional
	Rows *int `json:"rows,omitempty"`

	// Checksum claimed artifact checksum supplied by the registrant
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	Checksum *string `json:"checksum,omitempty"`

	// Golden whether the dataset is approved for evaluation gates; default false
	// +default=false
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

	// Conditions represent the current state of the Dataset resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase short display summary, one of Pending, Ready, or Failed
	// +kubebuilder:validation:Enum=Pending;Ready;Failed
	// +required
	Phase DatasetPhase `json:"phase"`

	// ObservedRows metadata resolved by the controller. Absent means the last
	// pass observed no usable row count: a zero-row artifact is unusable, so the
	// controller reports ArtifactEmpty instead of recording the observation.
	// +kubebuilder:validation:Minimum=1
	// +optional
	ObservedRows *int `json:"observedRows,omitempty"`

	// ObservedChecksum metadata resolved by the controller
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +optional
	ObservedChecksum *string `json:"observedChecksum,omitempty"`

	// LastResolvedTime timestamp of the last successful metadata resolution
	// +optional
	LastResolvedTime metav1.Time `json:"lastResolvedTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Dataset is the Schema for the datasets API
// +kubebuilder:printcolumn:name=Owner,type=string,JSONPath=`.spec.owner`
// +kubebuilder:printcolumn:name=ExpectedRows,type=integer,JSONPath=`.spec.rows`
// +kubebuilder:printcolumn:name=Golden,type=boolean,JSONPath=`.spec.golden`
// +kubebuilder:printcolumn:name=Phase,type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name=Age,type=date,JSONPath=`.metadata.creationTimestamp`
// +kubebuilder:resource:path=datasets,singular=dataset,shortName=dset,categories=all,scope=Namespaced
type Dataset struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of Dataset
	// +required
	Spec DatasetSpec `json:"spec"`

	// status defines the observed state of Dataset
	// +optional
	Status DatasetStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// DatasetList contains a list of Dataset
type DatasetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dataset `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dataset{}, &DatasetList{})
}
