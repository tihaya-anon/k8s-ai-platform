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

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

// The platform's status contract, specified in docs/product/domain.md#trainingjob-status. Alerts,
// scripts and the 1.4 gate match on these exact strings, so they live here as constants: a rename
// is one edit and a typo is a compile error.
const (
	conditionRendered = "Rendered"
	conditionAccepted = "Accepted"
	conditionFinished = "Finished"

	reasonJobRendered  = "JobRendered"
	reasonRenderFailed = "RenderFailed"

	reasonAccepted              = "Accepted"
	reasonAwaitingAdmission     = "AwaitingAdmission"
	reasonWaitingForCapacity    = "WaitingForCapacity"
	reasonExceedsTeamLimit      = "ExceedsTeamLimit"
	reasonPlatformMisconfigured = "PlatformMisconfigured"
	reasonPreempted             = "Preempted"

	reasonRunning   = "Running"
	reasonSucceeded = "Succeeded"
	reasonFailed    = "Failed"
)

// kueueReasonToPlatform maps Kueue's QuotaReserved reasons onto the three platform reasons, which
// are the three different next actions a user can take. The merges are argued in domain.md; the
// short version is that "the team is full" and "you are behind a preemption" are the same sentence
// to a user, while "this can never fit" and "an operator broke something" are not.
var kueueReasonToPlatform = map[string]string{
	kueuev1beta2.WorkloadQuotaReservedReasonWaitingForQuota:              reasonWaitingForCapacity,
	kueuev1beta2.WorkloadQuotaReservedReasonWaitingForPreemptedWorkloads: reasonWaitingForCapacity,
	kueuev1beta2.WorkloadQuotaReservedReasonPendingEvaluation:            reasonWaitingForCapacity,
	kueuev1beta2.WorkloadQuotaReservedReasonWaitingForPodsReady:          reasonWaitingForCapacity,
	kueuev1beta2.WorkloadQuotaReservedReasonSuspended:                    reasonWaitingForCapacity,

	kueuev1beta2.WorkloadQuotaReservedReasonExceedsMaxQuota: reasonExceedsTeamLimit,

	kueuev1beta2.WorkloadQuotaReservedReasonMisconfigured:             reasonPlatformMisconfigured,
	kueuev1beta2.WorkloadQuotaReservedReasonNoMatchingFlavor:          reasonPlatformMisconfigured,
	kueuev1beta2.WorkloadQuotaReservedReasonUnrecordableResourceUsage: reasonPlatformMisconfigured,
	kueuev1beta2.WorkloadQuotaReservedReasonDRAResourcesUnresolved:    reasonPlatformMisconfigured,
	kueuev1beta2.WorkloadQuotaReservedReasonTopologyPlacementFailed:   reasonPlatformMisconfigured,
}

// acceptedCondition translates Kueue's answer into the platform's. A nil Workload is not an error:
// Kueue's reconciler runs in another process, so there is always a window where the Job exists and
// its admission request does not.
func acceptedCondition(wl *kueuev1beta2.Workload, generation int64) metav1.Condition {
	cond := metav1.Condition{
		Type:               conditionAccepted,
		ObservedGeneration: generation,
	}

	if wl == nil {
		cond.Status = metav1.ConditionFalse
		cond.Reason = reasonAwaitingAdmission
		cond.Message = "the cluster has not yet picked up this run"
		return cond
	}

	// Eviction first: a run that was admitted and then preempted is waiting again, and the
	// QuotaReserved condition left behind may still read True. Level-triggered means reporting what
	// is true now, not the high-water mark.
	if evicted := apimeta.FindStatusCondition(wl.Status.Conditions, kueuev1beta2.WorkloadEvicted); evicted != nil && evicted.Status == metav1.ConditionTrue {
		cond.Status = metav1.ConditionFalse
		cond.Reason = reasonPreempted
		cond.Message = fmt.Sprintf("this run was stopped to free capacity and is queued again: %s", evicted.Message)
		return cond
	}

	if admitted := apimeta.FindStatusCondition(wl.Status.Conditions, kueuev1beta2.WorkloadAdmitted); admitted != nil && admitted.Status == metav1.ConditionTrue {
		cond.Status = metav1.ConditionTrue
		cond.Reason = reasonAccepted
		cond.Message = "the cluster accepted this run and its pods may start"
		return cond
	}

	cond.Status = metav1.ConditionFalse
	quota := apimeta.FindStatusCondition(wl.Status.Conditions, kueuev1beta2.WorkloadQuotaReserved)
	if quota == nil {
		cond.Reason = reasonWaitingForCapacity
		cond.Message = "the cluster has not decided on this run yet"
		return cond
	}

	platform, known := kueueReasonToPlatform[quota.Reason]
	if !known {
		// A reason the platform has never seen is a platform defect, not a user's problem: Kueue
		// adds reasons across releases, and a switch with no default is how a new one becomes a run
		// that is Pending forever with nothing to read. The original is quoted so an operator can
		// act on it without reading our source.
		cond.Reason = reasonPlatformMisconfigured
		cond.Message = fmt.Sprintf("the platform does not recognise this state and an operator should look: %q (%s)", quota.Reason, quota.Message)
		return cond
	}

	cond.Reason = platform
	switch platform {
	case reasonWaitingForCapacity:
		cond.Message = fmt.Sprintf("waiting for capacity in this team's queue: %s", quota.Message)
	case reasonExceedsTeamLimit:
		cond.Message = fmt.Sprintf("this run asks for more than the team's limit and cannot start at any time; reduce the request: %s", quota.Message)
	case reasonPlatformMisconfigured:
		cond.Message = fmt.Sprintf("the platform is misconfigured for this run and an operator should look, this is not a problem with your job: %s", quota.Message)
	}
	return cond
}

// finishedCondition reads the child Job rather than the Workload. Kueue's Finished condition
// follows the Job's, so the Job is the earlier and more precise source for the same fact.
func finishedCondition(job *batchv1.Job, generation int64) metav1.Condition {
	cond := metav1.Condition{
		Type:               conditionFinished,
		Status:             metav1.ConditionFalse,
		Reason:             reasonRunning,
		Message:            "the run has not finished",
		ObservedGeneration: generation,
	}
	for _, c := range job.Status.Conditions {
		if c.Status != corev1.ConditionTrue {
			continue
		}
		switch c.Type {
		case batchv1.JobComplete:
			cond.Status = metav1.ConditionTrue
			cond.Reason = reasonSucceeded
			cond.Message = "the run finished successfully"
		case batchv1.JobFailed:
			cond.Status = metav1.ConditionTrue
			cond.Reason = reasonFailed
			cond.Message = fmt.Sprintf("the training code failed: %s", c.Message)
		}
	}
	return cond
}

// phaseFor derives the one-word summary from the conditions. It is derived on every pass and never
// written independently, which is the rule that keeps it from disagreeing with the conditions it
// summarises: deleting it would lose no information.
func phaseFor(conds []metav1.Condition) mlv1alpha1.TrainingJobPhase {
	if finished := apimeta.FindStatusCondition(conds, conditionFinished); finished != nil && finished.Status == metav1.ConditionTrue {
		if finished.Reason == reasonSucceeded {
			return mlv1alpha1.TrainingJobPhaseSuccess
		}
		return mlv1alpha1.TrainingJobPhaseFailed
	}
	if rendered := apimeta.FindStatusCondition(conds, conditionRendered); rendered != nil && rendered.Status == metav1.ConditionFalse {
		return mlv1alpha1.TrainingJobPhaseFailed
	}
	if accepted := apimeta.FindStatusCondition(conds, conditionAccepted); accepted != nil && accepted.Status == metav1.ConditionTrue {
		return mlv1alpha1.TrainingJobPhaseRunning
	}
	return mlv1alpha1.TrainingJobPhasePending
}
