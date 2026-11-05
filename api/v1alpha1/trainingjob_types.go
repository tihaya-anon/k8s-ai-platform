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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

type AcceleratorClass string

const (
	AcceleratorClassPlaceholder AcceleratorClass = "AcceleratorClassPlaceholder"
)

type Accelerator struct {

	// +kubebuilder:validation:Enum=AcceleratorClassPlaceholder
	// +required

	Class AcceleratorClass `json:"class"`

	// +kubebuilder:validation:Minimum=1
	// +required

	Count int64 `json:"count"`
}

// Resources defines CPU, memory, and optionally an accelerator class and a device count
type Resources struct {

	// +kubebuilder:validation:XValidation:rule="self == quantity(self) && quantity(self).compareTo(quantity('0')) > 0",message="cpu must larger than 0"
	// +required

	CPU resource.Quantity `json:"cpu"`

	// +kubebuilder:validation:XValidation:rule="self == quantity(self) && quantity(self).compareTo(quantity('0')) > 0",message="memory must larger than 0"
	// +required

	Memory resource.Quantity `json:"memory"`

	// +optional

	Accelerator *Accelerator `json:"accelerator"`
}

// TrainingJobSpec defines the desired state of TrainingJob
type TrainingJobSpec struct {

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=128
	// +required

	// Owner non-empty human or team owner used for accountability and incident routing
	Owner *string `json:"owner"`

	// +required

	// Dataset the registered dataset this run trains on, by name
	Dataset *string `json:"dataset"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required

	// BaseModel the trained weights this run starts from
	BaseModel *string `json:"baseModel"`

	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=256
	// +required

	// Image the container holding the training code, framework and GPU libraries
	Image *string `json:"image"`

	// +required

	// Resources CPU, memory, and optionally an accelerator class and a device count
	Resources Resources `json:"resources"`

	// +required

	// Queue points to the LocalQueue this run belongs to, by name
	Queue *string `json:"queue"`
}

type TrainingJobPhase string

const (
	TrainingJobPhasePending TrainingJobPhase = "Pending"
	TrainingJobPhaseRunning TrainingJobPhase = "Running"
	TrainingJobPhaseSuccess TrainingJobPhase = "Success"
	TrainingJobPhaseFailed  TrainingJobPhase = "Failed"
)

// TrainingJobStatus defines the observed state of TrainingJob.
type TrainingJobStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the TrainingJob resource.
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

	// +kubebuilder:validation:Enum=Pending;Running;Success;Failed
	// +required

	// Phase short display summary, one of Pending, Running, Success or Failed
	Phase TrainingJobPhase `json:"phase"`

	// +optional

	// LastResolvedTime timestamp of the last successful metadata resolution
	LastResolvedTime metav1.Time `json:"lastResolvedTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:path=trainingjobs,singular=trainingjob,shortName=tj,scope=Namespaced

// TrainingJob is the Schema for the trainingjobs API
type TrainingJob struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="spec is immutable"
	// +required

	// spec defines the desired state of TrainingJob
	Spec TrainingJobSpec `json:"spec"`

	// status defines the observed state of TrainingJob
	// +optional
	Status TrainingJobStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// TrainingJobList contains a list of TrainingJob
type TrainingJobList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []TrainingJob `json:"items"`
}

func init() {
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(SchemeGroupVersion, &TrainingJob{}, &TrainingJobList{})
		return nil
	})
}
