package api

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
	"github.com/tihaya-anon/k8s-ai-platform/internal/store"
)

func newServer(t *testing.T) (*httptest.Server, *store.Store) {
	t.Helper()
	dir := t.TempDir()
	st, err := store.Open(filepath.Join(dir, "state.json"))
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	s := New(st, dir, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(srv.Close)
	return srv, st
}

func post(t *testing.T, srv *httptest.Server, path string, body any) (int, []byte) {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(srv.URL+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, raw
}

func TestCreateDatasetRequiresOwner(t *testing.T) {
	srv, _ := newServer(t)
	code, body := post(t, srv, "/v1/datasets", map[string]any{"name": "docs", "version": "v1"})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400: every object needs an owner", code, body)
	}
}

func TestCreateDatasetGeneratesRows(t *testing.T) {
	srv, _ := newServer(t)
	code, body := post(t, srv, "/v1/datasets", map[string]any{
		"name": "docs", "version": "v1", "owner": "ml-team", "rows": 5, "generate": true,
	})
	if code != http.StatusCreated {
		t.Fatalf("status = %d (%s), want 201", code, body)
	}
	var d model.Dataset
	if err := json.Unmarshal(body, &d); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if _, err := io.ReadAll(mustOpen(t, d.URI)); err != nil {
		t.Fatalf("generated dataset unreadable: %v", err)
	}
}

func TestTrainingJobRejectsUnknownDataset(t *testing.T) {
	srv, _ := newServer(t)
	code, body := post(t, srv, "/v1/training-jobs", map[string]any{
		"name": "ft-1", "owner": "ml-team", "modelName": "support", "datasetRef": "nope:v1",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400 for an unregistered dataset", code, body)
	}
}

func TestEvaluationRequiresGoldenDataset(t *testing.T) {
	srv, st := newServer(t)
	mustCreateModel(t, st, model.ModelRegistered)
	if _, err := st.CreateDataset(model.Dataset{Name: "docs", Version: "v1", Owner: "ml"}); err != nil {
		t.Fatalf("CreateDataset: %v", err)
	}
	code, body := post(t, srv, "/v1/evaluations", map[string]any{
		"name": "eval-1", "modelRef": "support:v1", "datasetRef": "docs:v1",
	})
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400: evaluation needs a golden dataset", code, body)
	}
}

// The gate is the whole point of the release path: an unevaluated model must not reach traffic.
func TestCanaryRejectsUngatedModel(t *testing.T) {
	srv, st := newServer(t)
	mustCreateModel(t, st, model.ModelRegistered)
	code, body := post(t, srv, "/v1/routes/support/canary", map[string]any{"version": "v1", "percent": 10})
	if code != http.StatusConflict {
		t.Fatalf("status = %d (%s), want 409 for a model that has not passed evaluation", code, body)
	}
}

func TestCanaryPromoteRollbackLifecycle(t *testing.T) {
	srv, st := newServer(t)
	mustCreateModel(t, st, model.ModelPromotionReady)

	if code, body := post(t, srv, "/v1/routes/support/canary", map[string]any{"version": "v1", "percent": 25}); code != http.StatusOK {
		t.Fatalf("canary status = %d (%s), want 200", code, body)
	}
	if m, _ := st.GetModel("support:v1"); m.Status != model.ModelCanary {
		t.Fatalf("model status = %q, want Canary", m.Status)
	}

	if code, body := post(t, srv, "/v1/routes/support/promote", map[string]any{}); code != http.StatusOK {
		t.Fatalf("promote status = %d (%s), want 200", code, body)
	}
	rt, err := st.GetRoute("support")
	if err != nil || rt.StableVersion != "v1" || rt.CanaryVersion != "" {
		t.Fatalf("route = %+v, %v; want v1 stable and no canary", rt, err)
	}

	// A second version canaries on top of the stable one, then rolls back without touching it.
	if _, err := st.CreateModel(model.ModelVersion{Name: "support", Version: "v2", Status: model.ModelPromotionReady}); err != nil {
		t.Fatalf("CreateModel v2: %v", err)
	}
	if code, body := post(t, srv, "/v1/routes/support/canary", map[string]any{"version": "v2", "percent": 50}); code != http.StatusOK {
		t.Fatalf("canary v2 status = %d (%s), want 200", code, body)
	}
	if code, body := post(t, srv, "/v1/routes/support/rollback", map[string]any{"reason": "latency regression"}); code != http.StatusOK {
		t.Fatalf("rollback status = %d (%s), want 200", code, body)
	}
	rt, _ = st.GetRoute("support")
	if rt.StableVersion != "v1" || rt.CanaryVersion != "" {
		t.Fatalf("after rollback route = %+v, want stable v1 and no canary", rt)
	}
	m, _ := st.GetModel("support:v2")
	if m.Status != model.ModelPromotionBlocked || m.Reason == "" {
		t.Fatalf("rolled back model = %+v, want PromotionBlocked with a reason", m)
	}
}

func TestPromoteWithoutCanaryConflicts(t *testing.T) {
	srv, st := newServer(t)
	if _, err := st.PutRoute(model.Route{Model: "support", StableVersion: "v1"}, "seed"); err != nil {
		t.Fatalf("PutRoute: %v", err)
	}
	if code, body := post(t, srv, "/v1/routes/support/promote", map[string]any{}); code != http.StatusConflict {
		t.Fatalf("status = %d (%s), want 409", code, body)
	}
}

func TestScaleRefusesZeroReplicas(t *testing.T) {
	srv, st := newServer(t)
	if _, err := st.PutRoute(model.Route{Model: "support", StableVersion: "v1"}, "seed"); err != nil {
		t.Fatalf("PutRoute: %v", err)
	}
	if code, body := post(t, srv, "/v1/routes/support/scale", map[string]any{"replicas": 0}); code != http.StatusBadRequest {
		t.Fatalf("status = %d (%s), want 400: online inference keeps a replica floor", code, body)
	}
}

func TestMetricsExposeJobPhases(t *testing.T) {
	srv, st := newServer(t)
	if _, err := st.CreateTrainingJob(model.TrainingJob{Name: "ft-1"}); err != nil {
		t.Fatalf("CreateTrainingJob: %v", err)
	}
	resp, err := http.Get(srv.URL + "/metrics")
	if err != nil {
		t.Fatalf("GET /metrics: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !bytes.Contains(body, []byte(`aiplatform_training_jobs{phase="Pending"} 1`)) {
		t.Fatalf("metrics missing pending job gauge:\n%s", body)
	}
}

func mustCreateModel(t *testing.T, st *store.Store, status model.ModelStatus) {
	t.Helper()
	if _, err := st.CreateModel(model.ModelVersion{Name: "support", Version: "v1", Status: status}); err != nil {
		t.Fatalf("CreateModel: %v", err)
	}
}

func mustOpen(t *testing.T, path string) io.Reader {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	t.Cleanup(func() { f.Close() })
	return f
}
