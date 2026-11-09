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
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
)

// workloadWith returns a Workload carrying one condition, which is all acceptedCondition reads.
func workloadWith(condType, reason, message string, status metav1.ConditionStatus) *kueuev1beta2.Workload {
	return &kueuev1beta2.Workload{
		Status: kueuev1beta2.WorkloadStatus{
			Conditions: []metav1.Condition{{
				Type:               condType,
				Status:             status,
				Reason:             reason,
				Message:            message,
				LastTransitionTime: metav1.Now(),
			}},
		},
	}
}

// TestAcceptedConditionBeforeKueueActs is the state every other case is measured against: the Job
// exists, its admission request does not yet, and that is normal rather than wrong.
func TestAcceptedConditionBeforeKueueActs(t *testing.T) {
	got := acceptedCondition(nil, 7)

	if got.Status != metav1.ConditionFalse {
		t.Errorf("status = %q, want False", got.Status)
	}
	if got.Reason != reasonAwaitingAdmission {
		t.Errorf("reason = %q, want %q", got.Reason, reasonAwaitingAdmission)
	}
	if got.ObservedGeneration != 7 {
		t.Errorf("observedGeneration = %d, want 7", got.ObservedGeneration)
	}
}

// TestAcceptedConditionWaitingForQuota is the worked example for the mapping in
// docs/product/domain.md#trainingjob-status. The assertions to copy from: the platform reason
// replaces Kueue's, Kueue's own message survives inside ours so an operator can still read it, and
// no Kueue vocabulary reaches the user-facing sentence.
func TestAcceptedConditionWaitingForQuota(t *testing.T) {
	wl := workloadWith(
		kueuev1beta2.WorkloadQuotaReserved,
		kueuev1beta2.WorkloadQuotaReservedReasonWaitingForQuota,
		"couldn't assign flavors to pod set main: insufficient quota for cpu in flavor default-flavor",
		metav1.ConditionFalse,
	)

	got := acceptedCondition(wl, 1)

	if got.Status != metav1.ConditionFalse {
		t.Errorf("status = %q, want False", got.Status)
	}
	if got.Reason != reasonWaitingForCapacity {
		t.Errorf("reason = %q, want %q", got.Reason, reasonWaitingForCapacity)
	}
	if !strings.Contains(got.Message, "insufficient quota for cpu") {
		t.Errorf("message %q drops Kueue's own detail", got.Message)
	}
	for _, leaked := range []string{"ClusterQueue", "QuotaReserved", "Workload"} {
		if strings.Contains(got.Message, leaked) {
			t.Errorf("message %q leaks Kueue's vocabulary (%q)", got.Message, leaked)
		}
	}
}

// The rest of the translation is task 1.2's Verify beat, and it is the maintainer's to write. One
// case per decision the Spec beat made, because each one is a decision that could have gone the
// other way and a test is what pins it:
//
//   - ExceedsMaxQuota maps to ExceedsTeamLimit and not to WaitingForCapacity. The distinction is
//     "shrink it" versus "wait", so assert the message says so.
//   - Misconfigured and NoMatchingFlavor both map to PlatformMisconfigured, and the message has to
//     say the fault is not the user's.
//   - A reason Kueue has not shipped yet -- invent one -- maps to PlatformMisconfigured with the
//     unknown reason quoted. This is the case that stops a Kueue upgrade turning into a run that is
//     Pending forever with nothing to read, and it is the one most worth writing first.
//   - Evicted=True wins over a QuotaReserved=True left behind, with reason Preempted. Build a
//     Workload carrying both conditions; the order they appear in the list must not matter.
//   - Admitted=True gives Accepted=True, and phaseFor then reports Running rather than Pending.
//   - finishedCondition over a Job with JobComplete, with JobFailed, and with neither.
//   - phaseFor for each of the four phases, including the Rendered=False path to Failed.
//
// A test that passes against a deliberately broken mapping is worth more attention than a test that
// passes: try inverting one arm of kueueReasonToPlatform and confirm your case fails.
