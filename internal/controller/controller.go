// Package controller reconciles platform objects into Kubernetes objects. It runs one loop, has no
// leader election, and reads desired state straight from the store: the smallest shape that still
// shows the real control-plane pattern of "observe, diff, act, record".
package controller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/tihaya-anon/k8s-ai-platform/internal/k8s"
	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
	"github.com/tihaya-anon/k8s-ai-platform/internal/store"
)

// Controller reconciles the store against the cluster.
type Controller struct {
	st   *store.Store
	kube *k8s.Client
	cfg  Config
	log  *slog.Logger
}

// New builds a controller.
func New(st *store.Store, kube *k8s.Client, cfg Config, log *slog.Logger) *Controller {
	return &Controller{st: st, kube: kube, cfg: cfg, log: log}
}

// Run reconciles every interval until the context is cancelled.
func (c *Controller) Run(ctx context.Context, interval time.Duration) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := c.reconcile(ctx); err != nil {
				c.log.Error("reconcile failed", "err", err)
			}
		}
	}
}

func (c *Controller) reconcile(ctx context.Context) error {
	var errs []error
	if err := c.reconcileTraining(ctx); err != nil {
		errs = append(errs, fmt.Errorf("training: %w", err))
	}
	if err := c.reconcileEvaluation(ctx); err != nil {
		errs = append(errs, fmt.Errorf("evaluation: %w", err))
	}
	if err := c.reconcileServing(ctx); err != nil {
		errs = append(errs, fmt.Errorf("serving: %w", err))
	}
	return errors.Join(errs...)
}

// --- training -------------------------------------------------------------

func (c *Controller) reconcileTraining(ctx context.Context) error {
	var errs []error
	for _, j := range c.st.ListTrainingJobs() {
		switch j.Phase {
		case model.PhasePending:
			if err := c.startTraining(ctx, j); err != nil {
				errs = append(errs, err)
			}
		case model.PhaseQueued, model.PhaseRunning:
			if err := c.syncTraining(ctx, j); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (c *Controller) startTraining(ctx context.Context, j model.TrainingJob) error {
	ds, err := c.st.GetDataset(j.DatasetRef)
	if err != nil {
		_, uerr := c.st.UpdateTrainingJob(j.Name, func(t *model.TrainingJob) {
			t.Phase, t.Reason = model.PhaseFailed, "dataset "+j.DatasetRef+" not found"
		})
		return uerr
	}
	version := c.nextVersion(j.ModelName)
	name, obj := c.cfg.TrainingJob(j, ds, version)
	if err := c.kube.Apply(ctx, c.kube.JobsPath(), name, obj); err != nil {
		return fmt.Errorf("apply training job %s: %w", name, err)
	}
	artifact := filepath.Join(c.cfg.ArtifactMount, "models", j.ModelName, version)
	_, err = c.st.UpdateTrainingJob(j.Name, func(t *model.TrainingJob) {
		t.Phase = model.PhaseQueued
		t.K8sJob = name
		t.ModelRef = j.ModelName + ":" + version
		t.ArtifactURI = artifact
		t.Attempt++
	})
	return err
}

// nextVersion allocates v1, v2, ... per logical model name.
func (c *Controller) nextVersion(name string) string {
	max := 0
	for _, m := range c.st.ListModels() {
		if m.Name != name {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(m.Version, "v%d", &n); err == nil && n > max {
			max = n
		}
	}
	for _, j := range c.st.ListTrainingJobs() {
		if j.ModelName != name || j.ModelRef == "" {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(j.ModelRef, name+":v%d", &n); err == nil && n > max {
			max = n
		}
	}
	return fmt.Sprintf("v%d", max+1)
}

func (c *Controller) syncTraining(ctx context.Context, j model.TrainingJob) error {
	job, err := c.kube.Get(ctx, c.kube.JobsPath(), j.K8sJob)
	if errors.Is(err, k8s.ErrNotFound) {
		_, uerr := c.st.UpdateTrainingJob(j.Name, func(t *model.TrainingJob) {
			t.Phase, t.Reason = model.PhaseFailed, "kubernetes job disappeared"
		})
		return uerr
	}
	if err != nil {
		return err
	}

	active := k8s.NestedInt(job, "status", "active")
	succeeded := k8s.NestedInt(job, "status", "succeeded")
	failed := k8s.NestedInt(job, "status", "failed")
	backoff := k8s.NestedInt(job, "spec", "backoffLimit")

	switch {
	case succeeded > 0:
		return c.finishTraining(ctx, j)
	case failed > backoff:
		reason := c.failureReason(ctx, j.K8sJob)
		_, err := c.st.UpdateTrainingJob(j.Name, func(t *model.TrainingJob) {
			t.Phase, t.Reason = model.PhaseFailed, reason
			now := time.Now().UTC()
			t.FinishedAt = &now
		})
		return err
	case active > 0 && j.Phase != model.PhaseRunning:
		_, err := c.st.UpdateTrainingJob(j.Name, func(t *model.TrainingJob) {
			t.Phase = model.PhaseRunning
			now := time.Now().UTC()
			t.StartedAt = &now
		})
		return err
	}
	return nil
}

// finishTraining registers the produced artifact as a model version. The version is Registered,
// not promotable: only evaluation gates can move it forward.
func (c *Controller) finishTraining(ctx context.Context, j model.TrainingJob) error {
	card, err := readModelCard(filepath.Join(j.ArtifactURI, "model.json"))
	if err != nil {
		_, uerr := c.st.UpdateTrainingJob(j.Name, func(t *model.TrainingJob) {
			t.Phase, t.Reason = model.PhaseFailed, "artifact missing: "+err.Error()
		})
		return uerr
	}
	name, version := splitRef(j.ModelRef)
	_, err = c.st.CreateModel(model.ModelVersion{
		Name:        name,
		Version:     version,
		Owner:       j.Owner,
		ArtifactURI: j.ArtifactURI,
		SourceJob:   j.Name,
		DatasetRef:  j.DatasetRef,
		Status:      model.ModelRegistered,
		Metrics:     model.Metrics{Accuracy: card.TrainAccuracy},
	})
	if err != nil && !errors.Is(err, store.ErrConflict) {
		return err
	}
	_, err = c.st.UpdateTrainingJob(j.Name, func(t *model.TrainingJob) {
		t.Phase = model.PhaseSucceeded
		now := time.Now().UTC()
		t.FinishedAt = &now
	})
	return err
}

// failureReason turns a failed Job into a sentence an on-call engineer can act on.
func (c *Controller) failureReason(ctx context.Context, jobName string) string {
	pods, err := c.kube.List(ctx, c.kube.PodsPath(), "job-name="+jobName)
	if err != nil || len(pods) == 0 {
		return "job failed, no pods found"
	}
	for _, p := range pods {
		statuses, _ := k8s.Nested(p, "status", "containerStatuses")
		list, _ := statuses.([]any)
		for _, s := range list {
			cs, _ := s.(map[string]any)
			term, _ := k8s.Nested(cs, "state", "terminated")
			if t, ok := term.(map[string]any); ok {
				reason, _ := t["reason"].(string)
				code, _ := t["exitCode"].(float64)
				if reason != "" || code != 0 {
					return fmt.Sprintf("%s (exit %d) in pod %s", reason, int(code), k8s.NestedString(p, "metadata", "name"))
				}
			}
			wait, _ := k8s.Nested(cs, "state", "waiting")
			if w, ok := wait.(map[string]any); ok {
				if reason, _ := w["reason"].(string); reason != "" {
					return reason + " in pod " + k8s.NestedString(p, "metadata", "name")
				}
			}
		}
	}
	return "job failed without a container status"
}

// --- evaluation -----------------------------------------------------------

func (c *Controller) reconcileEvaluation(ctx context.Context) error {
	var errs []error
	for _, e := range c.st.ListEvaluations() {
		switch e.Phase {
		case model.PhasePending:
			if err := c.startEvaluation(ctx, e); err != nil {
				errs = append(errs, err)
			}
		case model.PhaseQueued, model.PhaseRunning:
			if err := c.syncEvaluation(ctx, e); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (c *Controller) startEvaluation(ctx context.Context, e model.EvaluationJob) error {
	m, err := c.st.GetModel(e.ModelRef)
	if err != nil {
		_, uerr := c.st.UpdateEvaluation(e.Name, func(x *model.EvaluationJob) {
			x.Phase, x.Reason = model.PhaseFailed, "model "+e.ModelRef+" not found"
		})
		return uerr
	}
	ds, err := c.st.GetDataset(e.DatasetRef)
	if err != nil {
		_, uerr := c.st.UpdateEvaluation(e.Name, func(x *model.EvaluationJob) {
			x.Phase, x.Reason = model.PhaseFailed, "dataset "+e.DatasetRef+" not found"
		})
		return uerr
	}
	name, obj := c.cfg.EvaluationJob(e, m, ds)
	if err := c.kube.Apply(ctx, c.kube.JobsPath(), name, obj); err != nil {
		return fmt.Errorf("apply evaluation job %s: %w", name, err)
	}
	if _, err := c.st.UpdateModel(m.Ref(), func(x *model.ModelVersion) {
		x.Status = model.ModelEvaluating
	}); err != nil {
		return err
	}
	_, err = c.st.UpdateEvaluation(e.Name, func(x *model.EvaluationJob) {
		x.Phase, x.K8sJob = model.PhaseQueued, name
	})
	return err
}

func (c *Controller) syncEvaluation(ctx context.Context, e model.EvaluationJob) error {
	job, err := c.kube.Get(ctx, c.kube.JobsPath(), e.K8sJob)
	if errors.Is(err, k8s.ErrNotFound) {
		_, uerr := c.st.UpdateEvaluation(e.Name, func(x *model.EvaluationJob) {
			x.Phase, x.Reason = model.PhaseFailed, "kubernetes job disappeared"
		})
		return uerr
	}
	if err != nil {
		return err
	}
	active := k8s.NestedInt(job, "status", "active")
	succeeded := k8s.NestedInt(job, "status", "succeeded")
	failed := k8s.NestedInt(job, "status", "failed")
	backoff := k8s.NestedInt(job, "spec", "backoffLimit")

	switch {
	case succeeded > 0:
		return c.finishEvaluation(e)
	case failed > backoff:
		reason := c.failureReason(ctx, e.K8sJob)
		_, err := c.st.UpdateEvaluation(e.Name, func(x *model.EvaluationJob) {
			x.Phase, x.Reason = model.PhaseFailed, reason
		})
		if err != nil {
			return err
		}
		_, err = c.st.UpdateModel(e.ModelRef, func(x *model.ModelVersion) {
			x.Status, x.Reason = model.ModelPromotionBlocked, "evaluation job failed"
		})
		return err
	case active > 0 && e.Phase != model.PhaseRunning:
		_, err := c.st.UpdateEvaluation(e.Name, func(x *model.EvaluationJob) {
			x.Phase = model.PhaseRunning
		})
		return err
	}
	return nil
}

// finishEvaluation reads the report the eval worker wrote and applies the promotion gates.
func (c *Controller) finishEvaluation(e model.EvaluationJob) error {
	path := filepath.Join(c.cfg.ArtifactMount, "evals", e.Name, "result.json")
	metrics, err := readMetrics(path)
	if err != nil {
		_, uerr := c.st.UpdateEvaluation(e.Name, func(x *model.EvaluationJob) {
			x.Phase, x.Reason = model.PhaseFailed, "evaluation report missing: "+err.Error()
		})
		return uerr
	}
	gates := e.Gates
	if gates == (model.Gates{}) {
		gates = model.DefaultGates()
	}
	passed, why := gates.Check(metrics)
	now := time.Now().UTC()
	if _, err := c.st.UpdateEvaluation(e.Name, func(x *model.EvaluationJob) {
		x.Phase = model.PhaseSucceeded
		x.Metrics = &metrics
		x.Passed = &passed
		x.Reason = why
		x.FinishedAt = &now
	}); err != nil {
		return err
	}
	_, err = c.st.UpdateModel(e.ModelRef, func(x *model.ModelVersion) {
		x.Metrics = metrics
		if passed {
			x.Status, x.Reason = model.ModelPromotionReady, ""
		} else {
			x.Status, x.Reason = model.ModelPromotionBlocked, why
		}
	})
	return err
}

// --- serving --------------------------------------------------------------

// reconcileServing makes the cluster match the routes: every version a route references gets a
// Deployment and Service, and every server no route references is removed.
func (c *Controller) reconcileServing(ctx context.Context) error {
	wanted, replicas := desiredServers(c.st.ListRoutes(), c.st.GetModel, c.log)

	var errs []error
	for ref, m := range wanted {
		name, dep := c.cfg.ModelServer(m, max(1, replicas[ref]))
		if err := c.kube.Apply(ctx, c.kube.DeploymentsPath(), name, dep); err != nil {
			errs = append(errs, fmt.Errorf("apply deployment %s: %w", name, err))
			continue
		}
		svcName, svc := c.cfg.ModelService(m)
		if err := c.kube.Apply(ctx, c.kube.ServicesPath(), svcName, svc); err != nil {
			errs = append(errs, fmt.Errorf("apply service %s: %w", svcName, err))
			continue
		}
		ready := 0
		if live, err := c.kube.Get(ctx, c.kube.DeploymentsPath(), name); err == nil {
			ready = k8s.NestedInt(live, "status", "readyReplicas")
		}
		if err := c.st.PutDeployment(model.Deployment{
			ModelRef: ref, Name: name, Service: svcName,
			Replicas: max(1, replicas[ref]), Ready: ready,
		}); err != nil {
			errs = append(errs, err)
		}
	}

	for _, d := range c.st.ListDeployments() {
		if _, ok := wanted[d.ModelRef]; ok {
			continue
		}
		if err := c.kube.Delete(ctx, c.kube.DeploymentsPath(), d.Name); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := c.kube.Delete(ctx, c.kube.ServicesPath(), d.Service); err != nil {
			errs = append(errs, err)
			continue
		}
		if err := c.st.DeleteDeployment(d.ModelRef); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// desiredServers expands the routes into the set of model versions that must be running, and the
// replica count each one needs. A route stores bare versions, so the full "name:version" ref has
// to be rebuilt here before any model lookup.
func desiredServers(routes []model.Route, get func(string) (model.ModelVersion, error), log *slog.Logger) (map[string]model.ModelVersion, map[string]int) {
	wanted := map[string]model.ModelVersion{}
	replicas := map[string]int{}
	for _, r := range routes {
		for _, version := range []string{r.StableVersion, r.CanaryVersion} {
			if version == "" {
				continue
			}
			ref := r.Model + ":" + version
			m, err := get(ref)
			if err != nil {
				log.Warn("route references an unknown model version", "route", r.Model, "ref", ref)
				continue
			}
			wanted[ref] = m
			if r.Replicas > replicas[ref] {
				replicas[ref] = r.Replicas
			}
		}
	}
	return wanted, replicas
}

// --- artifact readers -----------------------------------------------------

type modelCard struct {
	ModelRef      string  `json:"modelRef"`
	TrainAccuracy float64 `json:"trainAccuracy"`
	Quality       float64 `json:"quality"`
	Steps         int     `json:"steps"`
}

func readModelCard(path string) (modelCard, error) {
	var c modelCard
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(b, &c)
}

func readMetrics(path string) (model.Metrics, error) {
	var m model.Metrics
	b, err := os.ReadFile(path)
	if err != nil {
		return m, err
	}
	return m, json.Unmarshal(b, &m)
}

func splitRef(ref string) (string, string) {
	for i := len(ref) - 1; i >= 0; i-- {
		if ref[i] == ':' {
			return ref[:i], ref[i+1:]
		}
	}
	return ref, ""
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
