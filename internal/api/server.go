// Package api is the control-plane HTTP surface. Every platform object is created, inspected, and
// released through here; nothing in the product loop requires kubectl.
package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
	"github.com/tihaya-anon/k8s-ai-platform/internal/store"
)

// Server serves the control-plane API.
type Server struct {
	st          *store.Store
	log         *slog.Logger
	artifactDir string

	requests atomic.Int64
	errors   atomic.Int64
}

// New builds the API server. artifactDir is the shared volume where datasets and model artifacts
// live, so the API can materialise synthetic datasets for the demo loop.
func New(st *store.Store, artifactDir string, log *slog.Logger) *Server {
	return &Server{st: st, artifactDir: artifactDir, log: log}
}

// Routes returns the mux with every endpoint registered.
func (s *Server) Routes() *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /metrics", s.metrics)

	mux.HandleFunc("POST /v1/datasets", s.createDataset)
	mux.HandleFunc("GET /v1/datasets", s.listDatasets)

	mux.HandleFunc("POST /v1/training-jobs", s.createTrainingJob)
	mux.HandleFunc("GET /v1/training-jobs", s.listTrainingJobs)
	mux.HandleFunc("GET /v1/training-jobs/{name}", s.getTrainingJob)

	mux.HandleFunc("GET /v1/models", s.listModels)
	mux.HandleFunc("GET /v1/models/{ref}", s.getModel)

	mux.HandleFunc("POST /v1/evaluations", s.createEvaluation)
	mux.HandleFunc("GET /v1/evaluations", s.listEvaluations)
	mux.HandleFunc("GET /v1/evaluations/{name}", s.getEvaluation)

	mux.HandleFunc("GET /v1/routes", s.listRoutes)
	mux.HandleFunc("GET /v1/routes/{model}", s.getRoute)
	mux.HandleFunc("POST /v1/routes/{model}/canary", s.startCanary)
	mux.HandleFunc("POST /v1/routes/{model}/promote", s.promote)
	mux.HandleFunc("POST /v1/routes/{model}/rollback", s.rollback)
	mux.HandleFunc("POST /v1/routes/{model}/scale", s.scale)

	mux.HandleFunc("GET /v1/deployments", s.listDeployments)
	mux.HandleFunc("GET /v1/events", s.listEvents)
	mux.HandleFunc("GET /v1/status", s.status)

	return mux
}

// Handler wraps the mux with request logging and counters.
func (s *Server) Handler() http.Handler {
	mux := s.Routes()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		s.requests.Add(1)
		rec := &statusRecorder{ResponseWriter: w, code: 200}
		mux.ServeHTTP(rec, r)
		if rec.code >= 500 {
			s.errors.Add(1)
		}
		s.log.Info("request", "method", r.Method, "path", r.URL.Path,
			"status", rec.code, "ms", time.Since(start).Milliseconds())
	})
}

type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(c int) {
	r.code = c
	r.ResponseWriter.WriteHeader(c)
}

// --- helpers --------------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func decode[T any](w http.ResponseWriter, r *http.Request) (T, bool) {
	var v T
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&v); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid body: "+err.Error())
		return v, false
	}
	return v, true
}

func storeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeErr(w, http.StatusConflict, err.Error())
	default:
		writeErr(w, http.StatusInternalServerError, err.Error())
	}
}

// --- datasets -------------------------------------------------------------

type datasetRequest struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Owner    string `json:"owner"`
	Rows     int    `json:"rows"`
	Golden   bool   `json:"golden"`
	URI      string `json:"uri"`
	Generate bool   `json:"generate"`
}

func (s *Server) createDataset(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[datasetRequest](w, r)
	if !ok {
		return
	}
	if req.Name == "" || req.Version == "" {
		writeErr(w, http.StatusBadRequest, "name and version are required")
		return
	}
	if req.Owner == "" {
		writeErr(w, http.StatusBadRequest, "owner is required: every object has an owner")
		return
	}
	if req.Rows <= 0 {
		req.Rows = 200
	}
	uri := req.URI
	if uri == "" {
		uri = filepath.Join(s.artifactDir, "datasets", req.Name+"-"+req.Version+".jsonl")
	}
	if req.Generate {
		if err := generateDataset(uri, req.Name+":"+req.Version, req.Rows); err != nil {
			writeErr(w, http.StatusInternalServerError, "generate dataset: "+err.Error())
			return
		}
	}
	d, err := s.st.CreateDataset(model.Dataset{
		Name: req.Name, Version: req.Version, Owner: req.Owner,
		URI: uri, Rows: req.Rows, Golden: req.Golden,
	})
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, d)
}

// generateDataset writes a deterministic synthetic dataset so the loop has real bytes on disk
// with real lineage, without shipping a corpus in the repository.
func generateDataset(path, ref string, rows int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for i := 0; i < rows; i++ {
		rec := map[string]any{
			"id":     fmt.Sprintf("%s-%04d", ref, i),
			"prompt": fmt.Sprintf("question %d about %s", i, ref),
			"label":  i % 4,
		}
		if err := enc.Encode(rec); err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) listDatasets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.st.ListDatasets())
}

// --- training -------------------------------------------------------------

type trainingRequest struct {
	Name            string `json:"name"`
	Owner           string `json:"owner"`
	BaseModel       string `json:"baseModel"`
	DatasetRef      string `json:"datasetRef"`
	ModelName       string `json:"modelName"`
	Steps           int    `json:"steps"`
	GPUs            int    `json:"gpus"`
	Priority        int    `json:"priority"`
	CheckpointEvery int    `json:"checkpointEvery"`
}

func (s *Server) createTrainingJob(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[trainingRequest](w, r)
	if !ok {
		return
	}
	switch {
	case req.Name == "":
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	case req.Owner == "":
		writeErr(w, http.StatusBadRequest, "owner is required")
		return
	case req.ModelName == "":
		writeErr(w, http.StatusBadRequest, "modelName is required")
		return
	}
	if _, err := s.st.GetDataset(req.DatasetRef); err != nil {
		writeErr(w, http.StatusBadRequest, "datasetRef "+req.DatasetRef+" is not registered")
		return
	}
	if req.Steps <= 0 {
		req.Steps = 20
	}
	if req.CheckpointEvery <= 0 {
		req.CheckpointEvery = 5
	}
	if req.GPUs <= 0 {
		req.GPUs = 1
	}
	j, err := s.st.CreateTrainingJob(model.TrainingJob{
		Name: req.Name, Owner: req.Owner, BaseModel: req.BaseModel,
		DatasetRef: req.DatasetRef, ModelName: req.ModelName, Steps: req.Steps,
		GPUs: req.GPUs, Priority: req.Priority, CheckpointEvery: req.CheckpointEvery,
	})
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, j)
}

func (s *Server) listTrainingJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.st.ListTrainingJobs())
}

func (s *Server) getTrainingJob(w http.ResponseWriter, r *http.Request) {
	j, err := s.st.GetTrainingJob(r.PathValue("name"))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, j)
}

// --- models and evaluation -----------------------------------------------

func (s *Server) listModels(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.st.ListModels())
}

func (s *Server) getModel(w http.ResponseWriter, r *http.Request) {
	m, err := s.st.GetModel(r.PathValue("ref"))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, m)
}

type evaluationRequest struct {
	Name       string       `json:"name"`
	Owner      string       `json:"owner"`
	ModelRef   string       `json:"modelRef"`
	DatasetRef string       `json:"datasetRef"`
	Gates      *model.Gates `json:"gates,omitempty"`
}

func (s *Server) createEvaluation(w http.ResponseWriter, r *http.Request) {
	req, ok := decode[evaluationRequest](w, r)
	if !ok {
		return
	}
	if req.Name == "" || req.ModelRef == "" || req.DatasetRef == "" {
		writeErr(w, http.StatusBadRequest, "name, modelRef and datasetRef are required")
		return
	}
	if _, err := s.st.GetModel(req.ModelRef); err != nil {
		writeErr(w, http.StatusBadRequest, "model "+req.ModelRef+" is not registered")
		return
	}
	ds, err := s.st.GetDataset(req.DatasetRef)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "dataset "+req.DatasetRef+" is not registered")
		return
	}
	if !ds.Golden {
		writeErr(w, http.StatusBadRequest, "evaluation must run against a golden dataset")
		return
	}
	gates := model.DefaultGates()
	if req.Gates != nil {
		gates = *req.Gates
	}
	e, err := s.st.CreateEvaluation(model.EvaluationJob{
		Name: req.Name, Owner: req.Owner, ModelRef: req.ModelRef,
		DatasetRef: req.DatasetRef, Gates: gates,
	})
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) listEvaluations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.st.ListEvaluations())
}

func (s *Server) getEvaluation(w http.ResponseWriter, r *http.Request) {
	e, err := s.st.GetEvaluation(r.PathValue("name"))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

// --- routes and release ---------------------------------------------------

// listRoutes is what the gateway polls, so it carries per-lane readiness alongside the weights.
func (s *Server) listRoutes(w http.ResponseWriter, r *http.Request) {
	ready := map[string]int{}
	for _, d := range s.st.ListDeployments() {
		ready[d.ModelRef] = d.Ready
	}
	routes := s.st.ListRoutes()
	views := make([]model.RouteView, 0, len(routes))
	for _, rt := range routes {
		views = append(views, model.RouteView{
			Route:       rt,
			StableReady: ready[rt.Model+":"+rt.StableVersion],
			CanaryReady: ready[rt.Model+":"+rt.CanaryVersion],
		})
	}
	writeJSON(w, http.StatusOK, views)
}

func (s *Server) getRoute(w http.ResponseWriter, r *http.Request) {
	rt, err := s.st.GetRoute(r.PathValue("model"))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

type canaryRequest struct {
	Version  string `json:"version"`
	Percent  int    `json:"percent"`
	Replicas int    `json:"replicas"`
}

// startCanary is the only way new model code reaches production traffic, and it refuses any
// version that has not passed its evaluation gates.
func (s *Server) startCanary(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("model")
	req, ok := decode[canaryRequest](w, r)
	if !ok {
		return
	}
	if req.Version == "" {
		writeErr(w, http.StatusBadRequest, "version is required")
		return
	}
	ref := name + ":" + req.Version
	m, err := s.st.GetModel(ref)
	if err != nil {
		storeErr(w, err)
		return
	}
	if m.Status != model.ModelPromotionReady && m.Status != model.ModelCanary {
		writeErr(w, http.StatusConflict,
			fmt.Sprintf("model %s is %s: promotion requires passing evaluation gates", ref, m.Status))
		return
	}
	if req.Percent <= 0 || req.Percent > 100 {
		req.Percent = 10
	}
	rt, err := s.st.GetRoute(name)
	if errors.Is(err, store.ErrNotFound) {
		rt = model.Route{Model: name, Replicas: 1}
	} else if err != nil {
		storeErr(w, err)
		return
	}
	if req.Replicas > 0 {
		rt.Replicas = req.Replicas
	}
	rt.CanaryVersion, rt.CanaryPercent = req.Version, req.Percent
	rt, err = s.st.PutRoute(rt, fmt.Sprintf("canary %s at %d%%", ref, req.Percent))
	if err != nil {
		storeErr(w, err)
		return
	}
	if _, err := s.st.UpdateModel(ref, func(x *model.ModelVersion) {
		x.Status, x.Reason = model.ModelCanary, ""
	}); err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

// promote makes the canary version the stable one and retires the version it replaced.
func (s *Server) promote(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("model")
	rt, err := s.st.GetRoute(name)
	if err != nil {
		storeErr(w, err)
		return
	}
	if rt.CanaryVersion == "" {
		writeErr(w, http.StatusConflict, "no canary version to promote")
		return
	}
	previous := rt.StableVersion
	newStable := rt.CanaryVersion
	rt.StableVersion, rt.CanaryVersion, rt.CanaryPercent = newStable, "", 0
	rt, err = s.st.PutRoute(rt, fmt.Sprintf("promote %s:%s to stable (was %q)", name, newStable, previous))
	if err != nil {
		storeErr(w, err)
		return
	}
	if _, err := s.st.UpdateModel(name+":"+newStable, func(x *model.ModelVersion) {
		x.Status = model.ModelStable
	}); err != nil {
		storeErr(w, err)
		return
	}
	if previous != "" && previous != newStable {
		if _, err := s.st.UpdateModel(name+":"+previous, func(x *model.ModelVersion) {
			x.Status, x.Reason = model.ModelRetired, "replaced by "+newStable
		}); err != nil {
			storeErr(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, rt)
}

type rollbackRequest struct {
	Reason string `json:"reason"`
}

// rollback drops the canary immediately. Application code never changes; only the route does.
func (s *Server) rollback(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("model")
	req, ok := decode[rollbackRequest](w, r)
	if !ok {
		return
	}
	rt, err := s.st.GetRoute(name)
	if err != nil {
		storeErr(w, err)
		return
	}
	if rt.CanaryVersion == "" {
		writeErr(w, http.StatusConflict, "no canary version to roll back")
		return
	}
	bad := rt.CanaryVersion
	reason := req.Reason
	if reason == "" {
		reason = "operator rollback"
	}
	rt.CanaryVersion, rt.CanaryPercent = "", 0
	rt, err = s.st.PutRoute(rt, fmt.Sprintf("rollback %s:%s (%s)", name, bad, reason))
	if err != nil {
		storeErr(w, err)
		return
	}
	if _, err := s.st.UpdateModel(name+":"+bad, func(x *model.ModelVersion) {
		x.Status, x.Reason = model.ModelPromotionBlocked, "rolled back: "+reason
	}); err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

type scaleRequest struct {
	Replicas int `json:"replicas"`
}

func (s *Server) scale(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("model")
	req, ok := decode[scaleRequest](w, r)
	if !ok {
		return
	}
	if req.Replicas < 1 {
		writeErr(w, http.StatusBadRequest, "replicas must be at least 1: online inference keeps a floor")
		return
	}
	rt, err := s.st.GetRoute(name)
	if err != nil {
		storeErr(w, err)
		return
	}
	rt.Replicas = req.Replicas
	rt, err = s.st.PutRoute(rt, fmt.Sprintf("scale %s to %d replicas", name, req.Replicas))
	if err != nil {
		storeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rt)
}

// --- observability --------------------------------------------------------

func (s *Server) listDeployments(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.st.ListDeployments())
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 50
	}
	writeJSON(w, http.StatusOK, s.st.Events(r.URL.Query().Get("name"), limit))
}

// status is the single page an operator opens first: what is running, what is blocked, what is
// serving traffic.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"datasets":     s.st.ListDatasets(),
		"trainingJobs": s.st.ListTrainingJobs(),
		"models":       s.st.ListModels(),
		"evaluations":  s.st.ListEvaluations(),
		"routes":       s.st.ListRoutes(),
		"deployments":  s.st.ListDeployments(),
		"events":       s.st.Events("", 20),
	})
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP aiplatform_api_requests_total Control plane API requests.\n")
	fmt.Fprintf(w, "# TYPE aiplatform_api_requests_total counter\n")
	fmt.Fprintf(w, "aiplatform_api_requests_total %d\n", s.requests.Load())
	fmt.Fprintf(w, "# HELP aiplatform_api_errors_total Control plane API 5xx responses.\n")
	fmt.Fprintf(w, "# TYPE aiplatform_api_errors_total counter\n")
	fmt.Fprintf(w, "aiplatform_api_errors_total %d\n", s.errors.Load())

	byPhase := map[model.Phase]int{}
	for _, j := range s.st.ListTrainingJobs() {
		byPhase[j.Phase]++
	}
	fmt.Fprintf(w, "# HELP aiplatform_training_jobs Training jobs by phase.\n")
	fmt.Fprintf(w, "# TYPE aiplatform_training_jobs gauge\n")
	for _, p := range []model.Phase{model.PhasePending, model.PhaseQueued, model.PhaseRunning, model.PhaseSucceeded, model.PhaseFailed} {
		fmt.Fprintf(w, "aiplatform_training_jobs{phase=%q} %d\n", p, byPhase[p])
	}

	fmt.Fprintf(w, "# HELP aiplatform_model_versions Model versions by status.\n")
	fmt.Fprintf(w, "# TYPE aiplatform_model_versions gauge\n")
	byStatus := map[model.ModelStatus]int{}
	for _, m := range s.st.ListModels() {
		byStatus[m.Status]++
	}
	for st, n := range byStatus {
		fmt.Fprintf(w, "aiplatform_model_versions{status=%q} %d\n", st, n)
	}
}
