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

// Condition types reported on a Dataset. A user diagnoses from these, so the
// names are part of the API and changing one is a breaking change.
const (
	// DatasetConditionResolved reports whether the artifact named by spec.uri
	// was found and read.
	DatasetConditionResolved = "Resolved"
	// DatasetConditionVerified reports whether what was read matches what the
	// spec declared — row count and checksum.
	DatasetConditionVerified = "Verified"
	// DatasetConditionReady is the roll-up other controllers gate on. A
	// TrainingJob waits on this rather than on the two above, so that the
	// meaning of "usable" can change without every consumer changing with it.
	DatasetConditionReady = "Ready"
)

// DatasetPhase is a human-facing summary of the conditions. It is derived, never
// authoritative: conditions can express "resolved but not yet verified", and a
// single string cannot. It exists because `kubectl get` needs one column, not
// because the controller needs one variable.
// +kubebuilder:validation:Enum=Pending;Resolving;Ready;Failed
type DatasetPhase string

const (
	DatasetPhasePending   DatasetPhase = "Pending"
	DatasetPhaseResolving DatasetPhase = "Resolving"
	DatasetPhaseReady     DatasetPhase = "Ready"
	DatasetPhaseFailed    DatasetPhase = "Failed"
)

// DatasetSpec is the user's declaration about an immutable input.
//
// Every field here is a claim the controller will check, not a value it will
// trust. Rows and checksum are the user saying "this is what I believe I
// registered"; the observed values in status are what was actually found. The
// pair is what makes a silently rewritten artifact detectable — which is the
// entire reason a Dataset object exists rather than a URI string on a job.
type DatasetSpec struct {
	// URI locates the dataset. The scheme selects the artifact backend, so the
	// set is closed: an unknown scheme must fail at admission rather than at
	// reconcile, where the user is no longer watching.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=2048
	// +kubebuilder:validation:Pattern=`^(s3|gs|pvc|https)://[^\s]+$`
	URI string `json:"uri"`

	// Owner is the team accountable for this dataset. It is a spec field for now
	// and becomes webhook-stamped from the authenticated identity in 5.3; until
	// then it is an honour system, which is worth knowing when reading an audit
	// trail from this period.
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=63
	// +kubebuilder:validation:Pattern=`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`
	Owner string `json:"owner"`

	// Rows is the expected row count. Optional: omit it and the controller
	// records what it finds without complaint; set it and a mismatch fails
	// verification.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Minimum=1
	Rows *int64 `json:"rows,omitempty"`

	// Checksum is the expected content hash, sha256 only. Same contract as Rows:
	// optional, but load-bearing once set.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:Pattern=`^sha256:[a-f0-9]{64}$`
	Checksum string `json:"checksum,omitempty"`

	// Golden marks an evaluation dataset. Evaluations may score against golden
	// datasets only, so this flag is a policy input, not a label — a label could
	// be edited by anyone with patch access without passing through this API's
	// validation.
	// +kubebuilder:default=false
	// +optional
	Golden bool `json:"golden,omitempty"`
}

// DatasetStatus is what the controller observed. Nothing here is user input, and
// nothing here should be read as a promise about the future.
type DatasetStatus struct {
	// Conditions is the authoritative state. Each carries observedGeneration, so
	// a reader can tell "verification failed" from "verification has not run
	// against your latest edit yet".
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// Phase summarises Conditions for `kubectl get`. Derived on every reconcile;
	// never read back by the controller as input.
	// +optional
	Phase DatasetPhase `json:"phase,omitempty"`

	// ObservedRows is the row count actually read at the URI.
	// +optional
	ObservedRows *int64 `json:"observedRows,omitempty"`

	// ObservedChecksum is the hash actually computed over the artifact.
	// +optional
	ObservedChecksum string `json:"observedChecksum,omitempty"`

	// LastResolvedTime is when the artifact was last successfully read. A stale
	// value on a Ready dataset is the signal that the controller has stopped
	// working, which no condition can express on its own.
	// +optional
	LastResolvedTime *metav1.Time `json:"lastResolvedTime,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:resource:shortName=dset,categories=ml
// +kubebuilder:printcolumn:name="Owner",type=string,JSONPath=`.spec.owner`
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=`.status.phase`
// +kubebuilder:printcolumn:name="Rows",type=integer,JSONPath=`.status.observedRows`
// +kubebuilder:printcolumn:name="Golden",type=boolean,JSONPath=`.spec.golden`
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=`.metadata.creationTimestamp`

// Dataset is a versioned, immutable input to training and evaluation.
type Dataset struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is the standard object metadata.
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// spec defines the desired state of Dataset.
	// +required
	Spec DatasetSpec `json:"spec"`

	// status defines the observed state of Dataset.
	// +optional
	Status DatasetStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// DatasetList contains a list of Dataset.
type DatasetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Dataset `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Dataset{}, &DatasetList{})
}
