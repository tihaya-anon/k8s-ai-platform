// Package store keeps all platform state in one JSON file, guarded by a mutex and written
// atomically. This is deliberately the smallest thing that works; swapping it for PostgreSQL is
// one of the maintainer exercises in docs/training-tasks.md.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
)

// ErrNotFound is returned for any lookup that misses.
var ErrNotFound = errors.New("not found")

// ErrConflict is returned when an object with the same identity already exists.
var ErrConflict = errors.New("already exists")

const maxEvents = 2000

type state struct {
	Datasets    map[string]model.Dataset       `json:"datasets"`
	TrainJobs   map[string]model.TrainingJob   `json:"trainingJobs"`
	Models      map[string]model.ModelVersion  `json:"models"`
	Evaluations map[string]model.EvaluationJob `json:"evaluations"`
	Routes      map[string]model.Route         `json:"routes"`
	Deployments map[string]model.Deployment    `json:"deployments"`
	Events      []model.Event                  `json:"events"`
}

func newState() *state {
	return &state{
		Datasets:    map[string]model.Dataset{},
		TrainJobs:   map[string]model.TrainingJob{},
		Models:      map[string]model.ModelVersion{},
		Evaluations: map[string]model.EvaluationJob{},
		Routes:      map[string]model.Route{},
		Deployments: map[string]model.Deployment{},
	}
}

// Store is the platform's metadata database.
type Store struct {
	mu   sync.RWMutex
	path string
	st   *state
	// Now is overridable in tests.
	Now func() time.Time
}

// Open loads the state file, creating an empty one if it does not exist.
func Open(path string) (*Store, error) {
	s := &Store{path: path, st: newState(), Now: time.Now}
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(b, s.st); err != nil {
			return nil, fmt.Errorf("parse state %s: %w", path, err)
		}
		// Older state files may miss maps added later.
		fill(s.st)
	case os.IsNotExist(err):
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		if err := s.flush(); err != nil {
			return nil, err
		}
	default:
		return nil, err
	}
	return s, nil
}

func fill(st *state) {
	if st.Datasets == nil {
		st.Datasets = map[string]model.Dataset{}
	}
	if st.TrainJobs == nil {
		st.TrainJobs = map[string]model.TrainingJob{}
	}
	if st.Models == nil {
		st.Models = map[string]model.ModelVersion{}
	}
	if st.Evaluations == nil {
		st.Evaluations = map[string]model.EvaluationJob{}
	}
	if st.Routes == nil {
		st.Routes = map[string]model.Route{}
	}
	if st.Deployments == nil {
		st.Deployments = map[string]model.Deployment{}
	}
}

// flush writes the state to a temp file and renames it, so a crash mid-write cannot corrupt state.
func (s *Store) flush() error {
	b, err := json.MarshalIndent(s.st, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// --- events ---------------------------------------------------------------

// Emit records a platform event. Callers already holding the lock use emitLocked.
func (s *Store) Emit(kind, name, typ, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.emitLocked(kind, name, typ, msg)
	_ = s.flush()
}

func (s *Store) emitLocked(kind, name, typ, msg string) {
	s.st.Events = append(s.st.Events, model.Event{
		Time: s.Now().UTC(), Kind: kind, Name: name, Type: typ, Message: msg,
	})
	if len(s.st.Events) > maxEvents {
		s.st.Events = s.st.Events[len(s.st.Events)-maxEvents:]
	}
}

// Events returns the most recent events, newest last, optionally filtered by object name.
func (s *Store) Events(name string, limit int) []model.Event {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := []model.Event{}
	for _, e := range s.st.Events {
		if name == "" || e.Name == name {
			out = append(out, e)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// --- datasets -------------------------------------------------------------

func (s *Store) CreateDataset(d model.Dataset) (model.Dataset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.st.Datasets[d.Ref()]; ok {
		return d, ErrConflict
	}
	d.CreatedAt = s.Now().UTC()
	s.st.Datasets[d.Ref()] = d
	s.emitLocked("Dataset", d.Ref(), "Created", "dataset version registered")
	return d, s.flush()
}

func (s *Store) GetDataset(ref string) (model.Dataset, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	d, ok := s.st.Datasets[ref]
	if !ok {
		return d, ErrNotFound
	}
	return d, nil
}

func (s *Store) ListDatasets() []model.Dataset {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Dataset, 0, len(s.st.Datasets))
	for _, d := range s.st.Datasets {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref() < out[j].Ref() })
	return out
}

// --- training jobs --------------------------------------------------------

func (s *Store) CreateTrainingJob(j model.TrainingJob) (model.TrainingJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.st.TrainJobs[j.Name]; ok {
		return j, ErrConflict
	}
	j.CreatedAt = s.Now().UTC()
	j.Phase = model.PhasePending
	s.st.TrainJobs[j.Name] = j
	s.emitLocked("TrainingJob", j.Name, "Created", "queued for scheduling")
	return j, s.flush()
}

func (s *Store) GetTrainingJob(name string) (model.TrainingJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	j, ok := s.st.TrainJobs[name]
	if !ok {
		return j, ErrNotFound
	}
	return j, nil
}

func (s *Store) ListTrainingJobs() []model.TrainingJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.TrainingJob, 0, len(s.st.TrainJobs))
	for _, j := range s.st.TrainJobs {
		out = append(out, j)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// UpdateTrainingJob applies mutate to a stored job and persists the result.
func (s *Store) UpdateTrainingJob(name string, mutate func(*model.TrainingJob)) (model.TrainingJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.st.TrainJobs[name]
	if !ok {
		return j, ErrNotFound
	}
	before := j.Phase
	mutate(&j)
	s.st.TrainJobs[name] = j
	if j.Phase != before {
		s.emitLocked("TrainingJob", name, string(j.Phase), phaseMessage(j.Phase, j.Reason))
	}
	return j, s.flush()
}

func phaseMessage(p model.Phase, reason string) string {
	if reason != "" {
		return string(p) + ": " + reason
	}
	return "phase " + string(p)
}

// --- models ---------------------------------------------------------------

func (s *Store) CreateModel(m model.ModelVersion) (model.ModelVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.st.Models[m.Ref()]; ok {
		return m, ErrConflict
	}
	m.CreatedAt = s.Now().UTC()
	if m.Status == "" {
		m.Status = model.ModelRegistered
	}
	s.st.Models[m.Ref()] = m
	s.emitLocked("ModelVersion", m.Ref(), "Registered", "artifact registered")
	return m, s.flush()
}

func (s *Store) GetModel(ref string) (model.ModelVersion, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, ok := s.st.Models[ref]
	if !ok {
		return m, ErrNotFound
	}
	return m, nil
}

func (s *Store) ListModels() []model.ModelVersion {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.ModelVersion, 0, len(s.st.Models))
	for _, m := range s.st.Models {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref() < out[j].Ref() })
	return out
}

func (s *Store) UpdateModel(ref string, mutate func(*model.ModelVersion)) (model.ModelVersion, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	m, ok := s.st.Models[ref]
	if !ok {
		return m, ErrNotFound
	}
	before := m.Status
	mutate(&m)
	s.st.Models[ref] = m
	if m.Status != before {
		s.emitLocked("ModelVersion", ref, string(m.Status), phaseMessageModel(m))
	}
	return m, s.flush()
}

func phaseMessageModel(m model.ModelVersion) string {
	if m.Reason != "" {
		return string(m.Status) + ": " + m.Reason
	}
	return "status " + string(m.Status)
}

// --- evaluations ----------------------------------------------------------

func (s *Store) CreateEvaluation(e model.EvaluationJob) (model.EvaluationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.st.Evaluations[e.Name]; ok {
		return e, ErrConflict
	}
	e.CreatedAt = s.Now().UTC()
	e.Phase = model.PhasePending
	s.st.Evaluations[e.Name] = e
	s.emitLocked("EvaluationJob", e.Name, "Created", "evaluation queued for "+e.ModelRef)
	return e, s.flush()
}

func (s *Store) GetEvaluation(name string) (model.EvaluationJob, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	e, ok := s.st.Evaluations[name]
	if !ok {
		return e, ErrNotFound
	}
	return e, nil
}

func (s *Store) ListEvaluations() []model.EvaluationJob {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.EvaluationJob, 0, len(s.st.Evaluations))
	for _, e := range s.st.Evaluations {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

func (s *Store) UpdateEvaluation(name string, mutate func(*model.EvaluationJob)) (model.EvaluationJob, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.st.Evaluations[name]
	if !ok {
		return e, ErrNotFound
	}
	before := e.Phase
	mutate(&e)
	s.st.Evaluations[name] = e
	if e.Phase != before {
		s.emitLocked("EvaluationJob", name, string(e.Phase), phaseMessage(e.Phase, e.Reason))
	}
	return e, s.flush()
}

// --- routes ---------------------------------------------------------------

func (s *Store) GetRoute(modelName string) (model.Route, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.st.Routes[modelName]
	if !ok {
		return r, ErrNotFound
	}
	return r, nil
}

func (s *Store) ListRoutes() []model.Route {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Route, 0, len(s.st.Routes))
	for _, r := range s.st.Routes {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	return out
}

// PutRoute writes a route and records what changed, which is the audit trail for every
// promotion, canary step, and rollback.
func (s *Store) PutRoute(r model.Route, reason string) (model.Route, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.UpdatedAt = s.Now().UTC()
	if r.Replicas <= 0 {
		r.Replicas = 1
	}
	s.st.Routes[r.Model] = r
	s.emitLocked("Route", r.Model, "Updated", reason)
	return r, s.flush()
}

// --- deployments ----------------------------------------------------------

func (s *Store) ListDeployments() []model.Deployment {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.Deployment, 0, len(s.st.Deployments))
	for _, d := range s.st.Deployments {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ModelRef < out[j].ModelRef })
	return out
}

func (s *Store) PutDeployment(d model.Deployment) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	d.UpdatedAt = s.Now().UTC()
	s.st.Deployments[d.ModelRef] = d
	return s.flush()
}

func (s *Store) DeleteDeployment(modelRef string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.st.Deployments, modelRef)
	s.emitLocked("Deployment", modelRef, "Deleted", "model server removed, no route references it")
	return s.flush()
}
