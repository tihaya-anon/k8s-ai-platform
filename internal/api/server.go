// Package api is the control-plane HTTP surface. Every platform object is created, inspected, and
// released through here; nothing in the product loop requires kubectl.
//
// The router is Gin. The platform's own mechanics stay on the standard library, but this is the
// surface people poke at by hand, so route grouping, request binding, and a browsable OpenAPI page
// are worth a dependency. The spec in openapi.yaml is hand written and served from the binary, so
// the documentation ships with the thing it documents and works on a disconnected machine.
package api

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"

	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
	"github.com/tihaya-anon/k8s-ai-platform/internal/store"
)

//go:embed openapi.yaml
var openapiSpec []byte

//go:embed docs
var docsFS embed.FS

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

// Handler returns the router with every endpoint registered.
func (s *Server) Handler() http.Handler {
	gin.SetMode(gin.ReleaseMode)
	// A typo in a request body is a bug, not a field to ignore: reject unknown fields instead of
	// silently accepting a call that does not do what the caller meant.
	binding.EnableDecoderDisallowUnknownFields = true

	r := gin.New()
	// Nothing sits in front of this in the lab, so trust no proxy headers.
	_ = r.SetTrustedProxies(nil)
	r.Use(gin.Recovery(), s.observe())

	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok\n") })
	r.GET("/metrics", s.metrics)

	// The API documents itself: the spec and a console to fire requests from.
	r.GET("/openapi.yaml", s.spec)
	r.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/docs") })
	r.GET("/docs", s.docsIndex)
	r.GET("/docs/:file", s.docsAsset)

	v1 := r.Group("/v1")
	{
		v1.POST("/datasets", s.createDataset)
		v1.GET("/datasets", s.listDatasets)

		v1.POST("/training-jobs", s.createTrainingJob)
		v1.GET("/training-jobs", s.listTrainingJobs)
		v1.GET("/training-jobs/:name", s.getTrainingJob)

		v1.GET("/models", s.listModels)
		v1.GET("/models/:ref", s.getModel)

		v1.POST("/evaluations", s.createEvaluation)
		v1.GET("/evaluations", s.listEvaluations)
		v1.GET("/evaluations/:name", s.getEvaluation)

		routes := v1.Group("/routes")
		{
			routes.GET("", s.listRoutes)
			routes.GET("/:model", s.getRoute)
			routes.POST("/:model/canary", s.startCanary)
			routes.POST("/:model/promote", s.promote)
			routes.POST("/:model/rollback", s.rollback)
			routes.POST("/:model/scale", s.scale)
		}

		v1.GET("/deployments", s.listDeployments)
		v1.GET("/events", s.listEvents)
		v1.GET("/status", s.status)
	}

	return r
}

// observe logs every request and counts the ones that fail on our side.
func (s *Server) observe() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		s.requests.Add(1)
		c.Next()
		if c.Writer.Status() >= 500 {
			s.errors.Add(1)
		}
		s.log.Info("request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"ms", time.Since(start).Milliseconds())
	}
}

// --- documentation --------------------------------------------------------

func (s *Server) spec(c *gin.Context) {
	c.Data(http.StatusOK, "application/yaml; charset=utf-8", openapiSpec)
}

func (s *Server) docsIndex(c *gin.Context) {
	b, err := docsFS.ReadFile("docs/index.html")
	if err != nil {
		c.String(http.StatusInternalServerError, "docs are missing from this build")
		return
	}
	c.Data(http.StatusOK, "text/html; charset=utf-8", b)
}

func (s *Server) docsAsset(c *gin.Context) {
	name := filepath.Base(c.Param("file"))
	b, err := docsFS.ReadFile("docs/" + name)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			c.Status(http.StatusNotFound)
			return
		}
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Data(http.StatusOK, contentType(name), b)
}

func contentType(name string) string {
	switch filepath.Ext(name) {
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".html":
		return "text/html; charset=utf-8"
	default:
		return "application/octet-stream"
	}
}

// --- helpers --------------------------------------------------------------

func writeErr(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"error": msg})
}

// bind decodes a JSON body. Endpoints whose body is optional pass allowEmpty, because an operator
// firing a promote from the docs page should not have to send an empty object.
func bind[T any](c *gin.Context, allowEmpty bool) (T, bool) {
	var v T
	err := c.ShouldBindJSON(&v)
	switch {
	case err == nil:
		return v, true
	case allowEmpty && errors.Is(err, io.EOF):
		return v, true
	default:
		writeErr(c, http.StatusBadRequest, "invalid body: "+err.Error())
		return v, false
	}
}

func storeErr(c *gin.Context, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeErr(c, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrConflict):
		writeErr(c, http.StatusConflict, err.Error())
	default:
		writeErr(c, http.StatusInternalServerError, err.Error())
	}
}

// --- datasets -------------------------------------------------------------

type datasetRequest struct {
	Name     string `json:"name" binding:"required"`
	Version  string `json:"version" binding:"required"`
	Owner    string `json:"owner"`
	Rows     int    `json:"rows"`
	Golden   bool   `json:"golden"`
	URI      string `json:"uri"`
	Generate bool   `json:"generate"`
}

func (s *Server) createDataset(c *gin.Context) {
	req, ok := bind[datasetRequest](c, false)
	if !ok {
		return
	}
	if req.Owner == "" {
		writeErr(c, http.StatusBadRequest, "owner is required: every object has an owner")
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
			writeErr(c, http.StatusInternalServerError, "generate dataset: "+err.Error())
			return
		}
	}
	d, err := s.st.CreateDataset(model.Dataset{
		Name: req.Name, Version: req.Version, Owner: req.Owner,
		URI: uri, Rows: req.Rows, Golden: req.Golden,
	})
	if err != nil {
		storeErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, d)
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

func (s *Server) listDatasets(c *gin.Context) {
	c.JSON(http.StatusOK, s.st.ListDatasets())
}

// --- training -------------------------------------------------------------

type trainingRequest struct {
	Name            string `json:"name" binding:"required"`
	Owner           string `json:"owner" binding:"required"`
	BaseModel       string `json:"baseModel"`
	DatasetRef      string `json:"datasetRef" binding:"required"`
	ModelName       string `json:"modelName" binding:"required"`
	Steps           int    `json:"steps"`
	GPUs            int    `json:"gpus"`
	Priority        int    `json:"priority"`
	CheckpointEvery int    `json:"checkpointEvery"`
}

func (s *Server) createTrainingJob(c *gin.Context) {
	req, ok := bind[trainingRequest](c, false)
	if !ok {
		return
	}
	if _, err := s.st.GetDataset(req.DatasetRef); err != nil {
		writeErr(c, http.StatusBadRequest, "datasetRef "+req.DatasetRef+" is not registered")
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
		storeErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, j)
}

func (s *Server) listTrainingJobs(c *gin.Context) {
	c.JSON(http.StatusOK, s.st.ListTrainingJobs())
}

func (s *Server) getTrainingJob(c *gin.Context) {
	j, err := s.st.GetTrainingJob(c.Param("name"))
	if err != nil {
		storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, j)
}

// --- models and evaluation -----------------------------------------------

func (s *Server) listModels(c *gin.Context) {
	c.JSON(http.StatusOK, s.st.ListModels())
}

func (s *Server) getModel(c *gin.Context) {
	m, err := s.st.GetModel(c.Param("ref"))
	if err != nil {
		storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, m)
}

type evaluationRequest struct {
	Name       string       `json:"name" binding:"required"`
	Owner      string       `json:"owner"`
	ModelRef   string       `json:"modelRef" binding:"required"`
	DatasetRef string       `json:"datasetRef" binding:"required"`
	Gates      *model.Gates `json:"gates,omitempty"`
}

func (s *Server) createEvaluation(c *gin.Context) {
	req, ok := bind[evaluationRequest](c, false)
	if !ok {
		return
	}
	if _, err := s.st.GetModel(req.ModelRef); err != nil {
		writeErr(c, http.StatusBadRequest, "model "+req.ModelRef+" is not registered")
		return
	}
	ds, err := s.st.GetDataset(req.DatasetRef)
	if err != nil {
		writeErr(c, http.StatusBadRequest, "dataset "+req.DatasetRef+" is not registered")
		return
	}
	if !ds.Golden {
		writeErr(c, http.StatusBadRequest, "evaluation must run against a golden dataset")
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
		storeErr(c, err)
		return
	}
	c.JSON(http.StatusCreated, e)
}

func (s *Server) listEvaluations(c *gin.Context) {
	c.JSON(http.StatusOK, s.st.ListEvaluations())
}

func (s *Server) getEvaluation(c *gin.Context) {
	e, err := s.st.GetEvaluation(c.Param("name"))
	if err != nil {
		storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, e)
}

// --- routes and release ---------------------------------------------------

// listRoutes is what the gateway polls, so it carries per-lane readiness alongside the weights.
func (s *Server) listRoutes(c *gin.Context) {
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
	c.JSON(http.StatusOK, views)
}

func (s *Server) getRoute(c *gin.Context) {
	rt, err := s.st.GetRoute(c.Param("model"))
	if err != nil {
		storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, rt)
}

type canaryRequest struct {
	Version  string `json:"version" binding:"required"`
	Percent  int    `json:"percent"`
	Replicas int    `json:"replicas"`
}

// startCanary is the only way new model code reaches production traffic, and it refuses any
// version that has not passed its evaluation gates.
func (s *Server) startCanary(c *gin.Context) {
	name := c.Param("model")
	req, ok := bind[canaryRequest](c, false)
	if !ok {
		return
	}
	ref := name + ":" + req.Version
	m, err := s.st.GetModel(ref)
	if err != nil {
		storeErr(c, err)
		return
	}
	if m.Status != model.ModelPromotionReady && m.Status != model.ModelCanary {
		writeErr(c, http.StatusConflict,
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
		storeErr(c, err)
		return
	}
	if req.Replicas > 0 {
		rt.Replicas = req.Replicas
	}
	rt.CanaryVersion, rt.CanaryPercent = req.Version, req.Percent
	rt, err = s.st.PutRoute(rt, fmt.Sprintf("canary %s at %d%%", ref, req.Percent))
	if err != nil {
		storeErr(c, err)
		return
	}
	if _, err := s.st.UpdateModel(ref, func(x *model.ModelVersion) {
		x.Status, x.Reason = model.ModelCanary, ""
	}); err != nil {
		storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, rt)
}

// promote makes the canary version the stable one and retires the version it replaced.
func (s *Server) promote(c *gin.Context) {
	name := c.Param("model")
	rt, err := s.st.GetRoute(name)
	if err != nil {
		storeErr(c, err)
		return
	}
	if rt.CanaryVersion == "" {
		writeErr(c, http.StatusConflict, "no canary version to promote")
		return
	}
	previous := rt.StableVersion
	newStable := rt.CanaryVersion
	rt.StableVersion, rt.CanaryVersion, rt.CanaryPercent = newStable, "", 0
	rt, err = s.st.PutRoute(rt, fmt.Sprintf("promote %s:%s to stable (was %q)", name, newStable, previous))
	if err != nil {
		storeErr(c, err)
		return
	}
	if _, err := s.st.UpdateModel(name+":"+newStable, func(x *model.ModelVersion) {
		x.Status = model.ModelStable
	}); err != nil {
		storeErr(c, err)
		return
	}
	if previous != "" && previous != newStable {
		if _, err := s.st.UpdateModel(name+":"+previous, func(x *model.ModelVersion) {
			x.Status, x.Reason = model.ModelRetired, "replaced by "+newStable
		}); err != nil {
			storeErr(c, err)
			return
		}
	}
	c.JSON(http.StatusOK, rt)
}

type rollbackRequest struct {
	Reason string `json:"reason"`
}

// rollback drops the canary immediately. Application code never changes; only the route does.
func (s *Server) rollback(c *gin.Context) {
	name := c.Param("model")
	req, ok := bind[rollbackRequest](c, true)
	if !ok {
		return
	}
	rt, err := s.st.GetRoute(name)
	if err != nil {
		storeErr(c, err)
		return
	}
	if rt.CanaryVersion == "" {
		writeErr(c, http.StatusConflict, "no canary version to roll back")
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
		storeErr(c, err)
		return
	}
	if _, err := s.st.UpdateModel(name+":"+bad, func(x *model.ModelVersion) {
		x.Status, x.Reason = model.ModelPromotionBlocked, "rolled back: "+reason
	}); err != nil {
		storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, rt)
}

type scaleRequest struct {
	Replicas int `json:"replicas"`
}

func (s *Server) scale(c *gin.Context) {
	name := c.Param("model")
	req, ok := bind[scaleRequest](c, false)
	if !ok {
		return
	}
	if req.Replicas < 1 {
		writeErr(c, http.StatusBadRequest, "replicas must be at least 1: online inference keeps a floor")
		return
	}
	rt, err := s.st.GetRoute(name)
	if err != nil {
		storeErr(c, err)
		return
	}
	rt.Replicas = req.Replicas
	rt, err = s.st.PutRoute(rt, fmt.Sprintf("scale %s to %d replicas", name, req.Replicas))
	if err != nil {
		storeErr(c, err)
		return
	}
	c.JSON(http.StatusOK, rt)
}

// --- observability --------------------------------------------------------

func (s *Server) listDeployments(c *gin.Context) {
	c.JSON(http.StatusOK, s.st.ListDeployments())
}

func (s *Server) listEvents(c *gin.Context) {
	limit, _ := strconv.Atoi(c.Query("limit"))
	if limit == 0 {
		limit = 50
	}
	c.JSON(http.StatusOK, s.st.Events(c.Query("name"), limit))
}

// status is the single page an operator opens first: what is running, what is blocked, what is
// serving traffic.
func (s *Server) status(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{
		"datasets":     s.st.ListDatasets(),
		"trainingJobs": s.st.ListTrainingJobs(),
		"models":       s.st.ListModels(),
		"evaluations":  s.st.ListEvaluations(),
		"routes":       s.st.ListRoutes(),
		"deployments":  s.st.ListDeployments(),
		"events":       s.st.Events("", 20),
	})
}

func (s *Server) metrics(c *gin.Context) {
	w := c.Writer
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
