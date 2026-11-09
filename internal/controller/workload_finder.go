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
	"context"
	"fmt"

	batchv1 "k8s.io/api/batch/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
	kueueconstants "sigs.k8s.io/kueue/pkg/controller/constants"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"
	kueuejob "sigs.k8s.io/kueue/pkg/controller/jobs/job"
)

// batchJobGVK is the owner kind Kueue records on the Workload it creates for a Job. It is written
// out rather than taken from the scheme because two of the finders below pass it to Kueue, which
// compares it against the apiVersion string on an ownerReference.
var batchJobGVK = schema.GroupVersionKind{Group: "batch", Version: "v1", Kind: "Job"}

// WorkloadFinder locates the Kueue Workload representing a Job's admission request.
//
// This is a seam rather than a single function because there are three defensible ways to do it and
// they differ on properties -- dependency weight, when the answer becomes available, and what
// happens on a Kueue upgrade -- rather than on correctness. Task 1.2's Review beat is choosing one.
// Find returns (nil, nil) when Kueue has not created the Workload yet, which is a normal state and
// not an error.
type WorkloadFinder interface {
	// Name identifies the strategy in logs and in test output.
	Name() string
	// SetupWithManager registers whatever the strategy needs on the manager, such as a field index.
	SetupWithManager(ctx context.Context, mgr ctrl.Manager) error
	Find(ctx context.Context, c client.Client, job *batchv1.Job) (*kueuev1beta2.Workload, error)
}

var (
	_ WorkloadFinder = OwnerIndexFinder{}
	_ WorkloadFinder = JobUIDLabelFinder{}
	_ WorkloadFinder = FrameworkFinder{}
)

// OwnerIndexFinder lists through the field index Kueue itself exports for this purpose.
//
// The index is built over the ownerReference Kueue sets, so it depends on nothing the platform
// controls: not the Workload's hashed name, not a label another controller has to have written.
// Its cost is the import -- pkg/controller/jobframework reaches every job integration Kueue
// supports, which is roughly thirty indirect modules including k8s.io/kubernetes.
type OwnerIndexFinder struct{}

func (OwnerIndexFinder) Name() string { return "owner-index" }

func (OwnerIndexFinder) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	return jobframework.SetupWorkloadOwnerIndex(ctx, mgr.GetFieldIndexer(), batchJobGVK)
}

func (OwnerIndexFinder) Find(ctx context.Context, c client.Client, job *batchv1.Job) (*kueuev1beta2.Workload, error) {
	var list kueuev1beta2.WorkloadList
	if err := c.List(ctx, &list,
		client.InNamespace(job.Namespace),
		jobframework.OwnerReferenceIndexFieldMatcher(batchJobGVK, job.Name),
	); err != nil {
		return nil, fmt.Errorf("listing workloads by owner index: %w", err)
	}
	return exactlyOne(list.Items, job)
}

// JobUIDLabelFinder lists on the label Kueue stamps with the owning Job's UID.
//
// The lightest of the three: it needs only the Workload API types and one string constant, so the
// module graph stays as it was. The UID also makes it exact where the owner index is keyed on a
// name -- a Job deleted and recreated under the same name has a new UID, and a stale Workload from
// the previous incarnation does not match. What it depends on is Kueue having written the label,
// which ConstructWorkload skips when the UID is not a valid label value.
type JobUIDLabelFinder struct{}

func (JobUIDLabelFinder) Name() string { return "job-uid-label" }

func (JobUIDLabelFinder) SetupWithManager(context.Context, ctrl.Manager) error { return nil }

func (JobUIDLabelFinder) Find(ctx context.Context, c client.Client, job *batchv1.Job) (*kueuev1beta2.Workload, error) {
	var list kueuev1beta2.WorkloadList
	if err := c.List(ctx, &list,
		client.InNamespace(job.Namespace),
		client.MatchingLabels{kueueconstants.JobUIDLabel: string(job.UID)},
	); err != nil {
		return nil, fmt.Errorf("listing workloads by job-uid label: %w", err)
	}
	return exactlyOne(list.Items, job)
}

// FrameworkFinder asks Kueue's own reconciler helper.
//
// The most direct of the three, and the one that stays correct if Kueue changes how a Job maps to
// its Workload, because it is the code Kueue's own reconciler runs. It needs a GenericJob, and the
// only batch/v1 implementation is kueuejob.Job -- a defined type over batchv1.Job, so the
// conversion below costs nothing at runtime. In exchange it couples the platform to an internal
// helper whose signature is not an API promise, and it returns a second list of Workloads to delete,
// which is a decision this controller has no business making about Kueue's objects.
type FrameworkFinder struct{}

func (FrameworkFinder) Name() string { return "jobframework" }

func (FrameworkFinder) SetupWithManager(ctx context.Context, mgr ctrl.Manager) error {
	// FindMatchingWorkloads lists by owner, so it needs the same index as OwnerIndexFinder.
	return jobframework.SetupWorkloadOwnerIndex(ctx, mgr.GetFieldIndexer(), batchJobGVK)
}

func (FrameworkFinder) Find(ctx context.Context, c client.Client, job *batchv1.Job) (*kueuev1beta2.Workload, error) {
	match, _, err := jobframework.FindMatchingWorkloads(ctx, c, (*kueuejob.Job)(job))
	if err != nil {
		return nil, fmt.Errorf("asking jobframework for matching workloads: %w", err)
	}
	return match, nil
}

// exactlyOne collapses a list result into the single Workload a Job should have. More than one is
// not a transient state to wait out: it means two admission requests exist for one Job, and
// picking either would make the status a coin flip.
func exactlyOne(items []kueuev1beta2.Workload, job *batchv1.Job) (*kueuev1beta2.Workload, error) {
	switch len(items) {
	case 0:
		return nil, nil
	case 1:
		return &items[0], nil
	default:
		return nil, fmt.Errorf("job %s/%s has %d workloads, expected one", job.Namespace, job.Name, len(items))
	}
}
