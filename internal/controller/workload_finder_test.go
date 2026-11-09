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
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	kueuev1beta2 "sigs.k8s.io/kueue/apis/kueue/v1beta2"
	kueueconstants "sigs.k8s.io/kueue/pkg/controller/constants"
	"sigs.k8s.io/kueue/pkg/controller/jobframework"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

// builderIndexer adapts a fake client builder to the client.FieldIndexer that Kueue's
// SetupWorkloadOwnerIndex expects, so the test registers Kueue's real index function rather than a
// reimplementation of it. A test that indexes differently from production proves nothing.
type builderIndexer struct{ b *fake.ClientBuilder }

func (i *builderIndexer) IndexField(_ context.Context, obj client.Object, field string, fn client.IndexerFunc) error {
	i.b = i.b.WithIndex(obj, field, fn)
	return nil
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("client-go scheme: %v", err)
	}
	if err := mlv1alpha1.AddToScheme(s); err != nil {
		t.Fatalf("platform scheme: %v", err)
	}
	if err := kueuev1beta2.AddToScheme(s); err != nil {
		t.Fatalf("kueue scheme: %v", err)
	}
	return s
}

// admittedJobAndWorkload builds the pair Kueue would have left behind: the Job this controller
// renders, and a Workload named by Kueue's own hashing scheme, owned by that Job, carrying the
// job-uid label, and with a podSet matching the Job -- which the jobframework strategy checks and
// the other two do not.
func admittedJobAndWorkload() (*batchv1.Job, *kueuev1beta2.Workload) {
	tj := &mlv1alpha1.TrainingJob{
		Name: "demo", Namespace: DefaultNamespace,
		Spec: ValidTrainingJobSpec(),
	}
	job := &batchv1.Job{
		Name:      tj.Name,
		Namespace: tj.Namespace,
		UID:       types.UID("11111111-2222-3333-4444-555555555555"),
		Labels:    map[string]string{queueNameLabel: tj.Spec.Queue},
		Spec:      batchV1JobSpec(tj),
	}
	// Parallelism and completions are set because the API server defaults them, so a Job read back
	// from the cluster always carries them. The fake client runs no defaulting, and leaving them
	// nil here would test an object that cannot exist -- see TestFrameworkFinderNeedsADefaultedJob.
	job.Spec.Parallelism = new(int32(1))
	job.Spec.Completions = new(int32(1))

	wl := &kueuev1beta2.Workload{
		Name:      jobframework.GetWorkloadNameForOwnerWithGVK(job.Name, job.UID, batchJobGVK),
		Namespace: job.Namespace,
		Labels:    map[string]string{kueueconstants.JobUIDLabel: string(job.UID)},
		OwnerReferences: []metav1.OwnerReference{{
			APIVersion: batchv1.SchemeGroupVersion.String(),
			Kind:       "Job",
			Name:       job.Name,
			UID:        job.UID,
			Controller: new(true),
		}},
		Spec: kueuev1beta2.WorkloadSpec{
			QueueName: kueuev1beta2.LocalQueueName(tj.Spec.Queue),
			PodSets: []kueuev1beta2.PodSet{{
				Name:     kueuev1beta2.DefaultPodSetName,
				Count:    1,
				Template: *job.Spec.Template.DeepCopy(),
			}},
		},
	}
	return job, wl
}

// TestFindersAgree is the contract the three strategies share. They differ on dependency weight and
// on what has to be true before the answer is available -- never on the answer itself -- which is
// what makes task 1.2's variant comparison a design question rather than a correctness one.
func TestFindersAgree(t *testing.T) {
	ctx := context.Background()
	s := testScheme(t)
	job, wl := admittedJobAndWorkload()

	for _, finder := range []WorkloadFinder{OwnerIndexFinder{}, JobUIDLabelFinder{}, FrameworkFinder{}} {
		t.Run(finder.Name(), func(t *testing.T) {
			idx := &builderIndexer{b: fake.NewClientBuilder().WithScheme(s)}
			if err := jobframework.SetupWorkloadOwnerIndex(ctx, idx, batchJobGVK); err != nil {
				t.Fatalf("registering the owner index: %v", err)
			}
			c := idx.b.WithObjects(job, wl).Build()

			got, err := finder.Find(ctx, c, job)
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if got == nil {
				t.Fatalf("no Workload found; the three strategies must agree")
			}
			if got.Name != wl.Name {
				t.Errorf("found %q, want %q", got.Name, wl.Name)
			}
		})
	}
}

// TestFindersReportNothingBeforeKueueActs covers the window every strategy has to tolerate: Kueue
// reconciles in another process, so the Job exists before its admission request does. Returning an
// error here would turn a normal state into a retry storm.
func TestFindersReportNothingBeforeKueueActs(t *testing.T) {
	ctx := context.Background()
	s := testScheme(t)
	job, _ := admittedJobAndWorkload()

	for _, finder := range []WorkloadFinder{OwnerIndexFinder{}, JobUIDLabelFinder{}, FrameworkFinder{}} {
		t.Run(finder.Name(), func(t *testing.T) {
			idx := &builderIndexer{b: fake.NewClientBuilder().WithScheme(s)}
			if err := jobframework.SetupWorkloadOwnerIndex(ctx, idx, batchJobGVK); err != nil {
				t.Fatalf("registering the owner index: %v", err)
			}
			c := idx.b.WithObjects(job).Build()

			got, err := finder.Find(ctx, c, job)
			if err != nil {
				t.Fatalf("Find: %v", err)
			}
			if got != nil {
				t.Errorf("found %q, want nil", got.Name)
			}
		})
	}
}

// TestFrameworkFinderNeedsADefaultedJob characterises an upstream behaviour that separates the
// three strategies. kueuejob.podsCount dereferences Spec.Parallelism unconditionally, on the
// comment "parallelism is always set as it is otherwise defaulted by k8s to 1" -- true of a Job read
// back from the API server, false of one held in memory. FindMatchingWorkloads therefore panics on
// an undefaulted Job, where a list by label or by owner index answers fine.
//
// Production is safe either way, because controllerutil.CreateOrUpdate leaves the server's response
// in the object. What this costs is testability: the jobframework strategy cannot be exercised
// against a hand-built Job, which is most of what a unit test has.
func TestFrameworkFinderNeedsADefaultedJob(t *testing.T) {
	ctx := context.Background()
	s := testScheme(t)
	job, wl := admittedJobAndWorkload()
	job.Spec.Parallelism = nil

	idx := &builderIndexer{b: fake.NewClientBuilder().WithScheme(s)}
	if err := jobframework.SetupWorkloadOwnerIndex(ctx, idx, batchJobGVK); err != nil {
		t.Fatalf("registering the owner index: %v", err)
	}
	c := idx.b.WithObjects(job, wl).Build()

	for _, finder := range []WorkloadFinder{OwnerIndexFinder{}, JobUIDLabelFinder{}} {
		got, err := finder.Find(ctx, c, job)
		if err != nil || got == nil {
			t.Errorf("%s: got (%v, %v), want the Workload and no error", finder.Name(), got, err)
		}
	}

	func() {
		defer func() {
			if recover() == nil {
				t.Errorf("jobframework: expected a panic on an undefaulted Job; if upstream has " +
					"fixed this, delete the test and the caveat in workload_finder.go")
			}
		}()
		_, _ = FrameworkFinder{}.Find(ctx, c, job)
	}()
}

// TestFrameworkFinderIgnoresADriftedJob characterises the other way the strategies part company.
// FindMatchingWorkloads answers "is there a Workload equivalent to this Job", not "which Workload
// belongs to this Job": when the Job's pod template no longer matches the recorded podSet, the
// Workload comes back in the to-delete list and the match is nil. A list by owner or by job-uid
// still finds it.
//
// Which behaviour this platform wants is the question. Reporting nothing about a drifted run hides
// it; reporting the stale Workload describes a run against an admission request that no longer
// matches it. Neither is obviously right, which is why this is a Review beat and not a bug.
func TestFrameworkFinderIgnoresADriftedJob(t *testing.T) {
	ctx := context.Background()
	s := testScheme(t)
	job, wl := admittedJobAndWorkload()
	job.Spec.Template.Spec.Containers[0].Image = "drifted:v2"

	idx := &builderIndexer{b: fake.NewClientBuilder().WithScheme(s)}
	if err := jobframework.SetupWorkloadOwnerIndex(ctx, idx, batchJobGVK); err != nil {
		t.Fatalf("registering the owner index: %v", err)
	}
	c := idx.b.WithObjects(job, wl).Build()

	for _, finder := range []WorkloadFinder{OwnerIndexFinder{}, JobUIDLabelFinder{}} {
		got, err := finder.Find(ctx, c, job)
		if err != nil || got == nil {
			t.Errorf("%s: got (%v, %v), want the Workload and no error", finder.Name(), got, err)
		}
	}

	got, err := FrameworkFinder{}.Find(ctx, c, job)
	if err != nil {
		t.Fatalf("jobframework: %v", err)
	}
	if got != nil {
		t.Errorf("jobframework: found %q, want nil for a drifted Job", got.Name)
	}
}

// TestExactlyOneRefusesTwoWorkloads proves the ambiguity is an error rather than a coin flip. Two
// admission requests for one Job is a state no strategy can resolve, and picking either would make
// the reported status depend on list ordering.
func TestExactlyOneRefusesTwoWorkloads(t *testing.T) {
	ctx := context.Background()
	s := testScheme(t)
	job, wl := admittedJobAndWorkload()
	second := wl.DeepCopy()
	second.Name = wl.Name + "-duplicate"
	second.ResourceVersion = ""

	idx := &builderIndexer{b: fake.NewClientBuilder().WithScheme(s)}
	if err := jobframework.SetupWorkloadOwnerIndex(ctx, idx, batchJobGVK); err != nil {
		t.Fatalf("registering the owner index: %v", err)
	}
	c := idx.b.WithObjects(job, wl, second).Build()

	for _, finder := range []WorkloadFinder{OwnerIndexFinder{}, JobUIDLabelFinder{}} {
		if _, err := finder.Find(ctx, c, job); err == nil {
			t.Errorf("%s: want an error for two Workloads, got nil", finder.Name())
		}
	}
}
