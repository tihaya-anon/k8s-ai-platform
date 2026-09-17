package store

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "state.json"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	return s
}

func TestDatasetRoundTripAndConflict(t *testing.T) {
	s := newStore(t)
	d := model.Dataset{Name: "docs", Version: "v1", Owner: "ml-team"}
	if _, err := s.CreateDataset(d); err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	if _, err := s.CreateDataset(d); !errors.Is(err, ErrConflict) {
		t.Fatalf("second create = %v, want ErrConflict", err)
	}
	got, err := s.GetDataset("docs:v1")
	if err != nil || got.Owner != "ml-team" {
		t.Fatalf("GetDataset = %+v, %v", got, err)
	}
	if _, err := s.GetDataset("docs:v2"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing dataset = %v, want ErrNotFound", err)
	}
}

func TestStatePersistsAcrossRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	s, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, err := s.CreateTrainingJob(model.TrainingJob{Name: "ft-1", Owner: "ml", ModelName: "m"}); err != nil {
		t.Fatalf("CreateTrainingJob: %v", err)
	}

	reopened, err := Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	j, err := reopened.GetTrainingJob("ft-1")
	if err != nil {
		t.Fatalf("GetTrainingJob after restart: %v", err)
	}
	if j.Phase != model.PhasePending {
		t.Fatalf("phase = %q, want Pending", j.Phase)
	}
}

func TestPhaseChangeEmitsEvent(t *testing.T) {
	s := newStore(t)
	if _, err := s.CreateTrainingJob(model.TrainingJob{Name: "ft-1"}); err != nil {
		t.Fatalf("CreateTrainingJob: %v", err)
	}
	if _, err := s.UpdateTrainingJob("ft-1", func(j *model.TrainingJob) {
		j.Phase, j.Reason = model.PhaseFailed, "image pull failure"
	}); err != nil {
		t.Fatalf("UpdateTrainingJob: %v", err)
	}
	events := s.Events("ft-1", 0)
	if len(events) != 2 {
		t.Fatalf("got %d events, want create plus phase change", len(events))
	}
	last := events[len(events)-1]
	if last.Type != string(model.PhaseFailed) || last.Message == "" {
		t.Fatalf("last event = %+v, want a Failed event carrying the reason", last)
	}
}

func TestUpdateWithoutPhaseChangeEmitsNothing(t *testing.T) {
	s := newStore(t)
	if _, err := s.CreateTrainingJob(model.TrainingJob{Name: "ft-1"}); err != nil {
		t.Fatalf("CreateTrainingJob: %v", err)
	}
	if _, err := s.UpdateTrainingJob("ft-1", func(j *model.TrainingJob) { j.K8sJob = "train-ft-1" }); err != nil {
		t.Fatalf("UpdateTrainingJob: %v", err)
	}
	if got := len(s.Events("ft-1", 0)); got != 1 {
		t.Fatalf("got %d events, want only the create event", got)
	}
}

func TestPutRouteDefaultsReplicas(t *testing.T) {
	s := newStore(t)
	r, err := s.PutRoute(model.Route{Model: "support"}, "first route")
	if err != nil {
		t.Fatalf("PutRoute: %v", err)
	}
	if r.Replicas != 1 {
		t.Fatalf("replicas = %d, want a floor of 1", r.Replicas)
	}
}
