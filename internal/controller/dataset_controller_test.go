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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/tihaya-anon/k8s-ai-platform/internal/artifact"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	mlv1alpha1 "github.com/tihaya-anon/k8s-ai-platform/api/v1alpha1"
)

var _ = Describe("Dataset condition vocabulary", func() {
	It("keeps the strings external consumers match on", func() {
		Expect(conditionResolvedDataset).To(Equal("Resolved"))
		Expect(reasonArtifactResolved).To(Equal("ArtifactResolved"))
		Expect(reasonArtifactEmpty).To(Equal("ArtifactEmpty"))
		Expect(reasonArtifactUnreachable).To(Equal("ArtifactUnreachable"))
		Expect(reasonDriftDetected).To(Equal("DriftDetected"))
	})
})

var _ = Describe("Dataset Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"

		ctx := context.Background()

		typeNamespacedName := types.NamespacedName{
			Name:      resourceName,
			Namespace: "default",
		}
		dataset := &mlv1alpha1.Dataset{}

		// Everything the reconciler cannot reach through the API server is injected,
		// so a whole pass is described by the metadata the store hands back. The
		// recorder is a parameter rather than a local: a transition is only
		// observable against the phase the previous pass left behind, and a fresh
		// recorder per pass could never tell "emitted once" from "emitted again".
		reconcileWith := func(recorder events.EventRecorder, found artifact.Metadata) {
			GinkgoHelper()
			controllerReconciler := &DatasetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: recorder,
				Resolver: &artifact.FakeResolver{Entries: map[string]artifact.Metadata{
					"sample.uri": found,
				}},
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Dataset")
			err := k8sClient.Get(ctx, typeNamespacedName, dataset)
			if err != nil && errors.IsNotFound(err) {
				resource := &mlv1alpha1.Dataset{
					ObjectMeta: metav1.ObjectMeta{
						Name:      resourceName,
						Namespace: "default",
					},
					Spec: ValidSpec(),
				}
				Expect(k8sClient.Create(ctx, resource)).To(Succeed())
			}
		})

		AfterEach(func() {
			resource := &mlv1alpha1.Dataset{}
			err := k8sClient.Get(ctx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Dataset")
			Expect(k8sClient.Delete(ctx, resource)).To(Succeed())
		})

		It("should record resolved metadata and go Ready", func() {
			By("Reconciling the created resource")
			controllerReconciler := &DatasetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: events.NewFakeRecorder(10),
				Resolver: &artifact.FakeResolver{Entries: map[string]artifact.Metadata{
					"sample.uri": {Rows: 1, Checksum: "placeholder"},
				}},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			resolved := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, resolved)).To(Succeed())
			Expect(resolved.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseReady))
			Expect(resolved.Status.ObservedRows).To(HaveValue(Equal(1)))
			Expect(resolved.Status.LastResolvedTime.IsZero()).To(BeFalse())
			Expect(apimeta.IsStatusConditionTrue(resolved.Status.Conditions, conditionResolvedDataset)).To(BeTrue())
		})

		It("should go Failed when the artifact store cannot resolve the uri", func() {
			controllerReconciler := &DatasetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: events.NewFakeRecorder(10),
				Resolver: &artifact.FakeResolver{Err: errors.NewServiceUnavailable("store down")},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())

			failed := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, failed)).To(Succeed())
			Expect(failed.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseFailed))
			condition := apimeta.FindStatusCondition(failed.Status.Conditions, conditionResolvedDataset)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Reason).To(Equal(reasonArtifactUnreachable))
			Expect(condition.ObservedGeneration).To(Equal(failed.Generation))
		})

		It("should go Failed when the artifact resolves to no rows", func() {
			By("resolving an artifact the store can read but nothing can train on")
			controllerReconciler := &DatasetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: events.NewFakeRecorder(10),
				Resolver: &artifact.FakeResolver{Entries: map[string]artifact.Metadata{
					"sample.uri": {Rows: 0, Checksum: "placeholder"},
				}},
			}

			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred(), "an empty artifact is a reportable state, not a reconcile failure")

			empty := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, empty)).To(Succeed())
			Expect(empty.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseFailed))

			condition := apimeta.FindStatusCondition(empty.Status.Conditions, conditionResolvedDataset)
			Expect(condition).NotTo(BeNil())
			By("naming the artifact as the layer at fault, not the registrant's claim")
			// Matched exactly, not as a substring: the claimed row count disagrees
			// with an empty artifact too, so dropping the guard that skips drift
			// detection on this branch turns the reason into DriftDetected and
			// blames the registrant for a store that returned nothing.
			Expect(condition.Reason).To(Equal(reasonArtifactEmpty))

			By("recording no row count, which Minimum=1 would have rejected")
			Expect(empty.Status.ObservedRows).To(BeNil())
			Expect(empty.Status.ObservedChecksum).To(HaveValue(Equal("placeholder")))
		})

		It("should clear a row count that an emptied artifact has invalidated", func() {
			recorder := events.NewFakeRecorder(10)

			By("observing a usable artifact first")
			reconcileWith(recorder, artifact.Metadata{Rows: 1, Checksum: "placeholder"})
			ready := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, ready)).To(Succeed())
			Expect(ready.Status.ObservedRows).To(HaveValue(Equal(1)))

			By("resolving the same uri after the artifact was emptied")
			reconcileWith(recorder, artifact.Metadata{Rows: 0, Checksum: "placeholder"})
			emptied := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, emptied)).To(Succeed())
			Expect(emptied.Status.ObservedRows).To(BeNil(), "a stale count would claim an observation this pass contradicts")
			Expect(emptied.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseFailed))
		})

		DescribeTable("should go Failed when the artifact no longer matches the claim", func(
			found artifact.Metadata, wantMessages ...string) {
			recorder := events.NewFakeRecorder(10)
			reconcileWith(recorder, found)

			drifted := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, drifted)).To(Succeed())
			Expect(drifted.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseFailed))

			condition := apimeta.FindStatusCondition(drifted.Status.Conditions, conditionResolvedDataset)
			Expect(condition).NotTo(BeNil())
			By("naming the claim as the layer at fault, not the store that answered")
			Expect(condition.Reason).To(Equal(reasonDriftDetected))
			Expect(condition.ObservedGeneration).To(Equal(drifted.Generation))

			// The whole clause, not just the field path: a message that printed the
			// observed and the claimed value the wrong way round would still contain
			// "spec.rows", and would tell the reader the artifact holds the number
			// the registrant typed.
			for _, want := range wantMessages {
				Expect(condition.Message).To(ContainSubstring(want))
			}

			By("still recording what the store returned, since the drift is only legible against it")
			Expect(drifted.Status.ObservedChecksum).To(HaveValue(Equal(found.Checksum)))

			By("warning a human about a transition nobody asked for")
			Expect(recorder.Events).To(Receive(SatisfyAll(
				ContainSubstring("Warning"),
				ContainSubstring("Dataset Failed"),
			)))
		},
			Entry("rows drifted", artifact.Metadata{Rows: 2, Checksum: "placeholder"},
				"spec.rows", "artifact resolved to 2 rows, spec claims 1"),
			Entry("checksum drifted", artifact.Metadata{Rows: 1, Checksum: "drifted"},
				"spec.checksum", "artifact resolved to checksum drifted, spec claims placeholder"),
			Entry("both drifted", artifact.Metadata{Rows: 2, Checksum: "drifted"},
				"spec.rows", "artifact resolved to 2 rows, spec claims 1",
				"spec.checksum", "artifact resolved to checksum drifted, spec claims placeholder"),
		)

		It("should fail a Ready Dataset whose artifact was rewritten under it", func() {
			recorder := events.NewFakeRecorder(10)

			By("resolving the artifact the registrant actually registered")
			reconcileWith(recorder, artifact.Metadata{Rows: 1, Checksum: "placeholder"})
			ready := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, ready)).To(Succeed())
			Expect(ready.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseReady))
			Expect(recorder.Events).To(Receive(ContainSubstring("Normal")))

			By("resyncing after the artifact was replaced, with nobody touching the Dataset")
			reconcileWith(recorder, artifact.Metadata{Rows: 1, Checksum: "rewritten"})
			rewritten := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, typeNamespacedName, rewritten)).To(Succeed())
			Expect(rewritten.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseFailed))
			condition := apimeta.FindStatusCondition(rewritten.Status.Conditions, conditionResolvedDataset)
			Expect(condition).NotTo(BeNil())
			Expect(condition.Reason).To(Equal(reasonDriftDetected))

			// generation moves on a spec write and never on a status write, so an
			// unchanged generation is the proof that the world moved, not the user.
			By("proving no user edit stands between the two passes")
			Expect(rewritten.Generation).To(Equal(ready.Generation))
			Expect(recorder.Events).To(Receive(ContainSubstring("Warning")))

			By("staying quiet on a resync that changed nothing, so the transition is not buried")
			reconcileWith(recorder, artifact.Metadata{Rows: 1, Checksum: "rewritten"})
			Expect(recorder.Events).NotTo(Receive())
		})

		It("should stay Ready when the artifact moves in a field nobody claimed", func() {
			By("registering a Dataset that makes no claim about rows or checksum")
			spec := ValidSpec()
			spec.Rows = nil
			spec.Checksum = nil
			unclaimed := &mlv1alpha1.Dataset{
				ObjectMeta: metav1.ObjectMeta{
					GenerateName: "unclaimed-",
					Namespace:    "default",
				},
				Spec: spec,
			}
			Expect(k8sClient.Create(ctx, unclaimed)).To(Succeed())
			// The outer AfterEach only removes the shared fixture, so cleanup for
			// this object has to travel with the object.
			DeferCleanup(func() {
				Expect(k8sClient.Delete(ctx, unclaimed)).To(Succeed())
			})
			unclaimedName := types.NamespacedName{Name: unclaimed.Name, Namespace: unclaimed.Namespace}

			controllerReconciler := &DatasetReconciler{
				Client:   k8sClient,
				Scheme:   k8sClient.Scheme(),
				Recorder: events.NewFakeRecorder(10),
				Resolver: &artifact.FakeResolver{Entries: map[string]artifact.Metadata{
					"sample.uri": {Rows: 50, Checksum: "whatever-the-store-holds"},
				}},
			}
			_, err := controllerReconciler.Reconcile(ctx, reconcile.Request{
				NamespacedName: unclaimedName,
			})
			Expect(err).NotTo(HaveOccurred())

			resolved := &mlv1alpha1.Dataset{}
			Expect(k8sClient.Get(ctx, unclaimedName, resolved)).To(Succeed())
			By("an absent claim has nothing to disagree with")
			Expect(resolved.Status.Phase).To(Equal(mlv1alpha1.DatasetPhaseReady))
			Expect(resolved.Status.ObservedRows).To(HaveValue(Equal(50)))
			Expect(resolved.Status.ObservedChecksum).To(HaveValue(Equal("whatever-the-store-holds")))
		})
	})
})
