// Package api is the control-plane HTTP surface. Every platform object is created, inspected, and
// released through here; nothing in the product loop requires kubectl.
//
// The router is Huma over net/http. Huma derives the OpenAPI document from the same Go types the
// handlers are declared with, so the description of an endpoint and its implementation cannot
// disagree: there is no second place to edit. openapi.yaml next to this file is a committed
// snapshot of that generated document, refreshed by `make openapi` and guarded by
// TestSpecSnapshotIsCurrent, so a change to the contract shows up as a diff in review.
//
// The platform's own mechanics stay on the standard library. This is the one surface people poke
// at by hand, so declarative validation and a browsable spec are worth the single dependency.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
	"github.com/tihaya-anon/k8s-ai-platform/internal/store"
)

const (
	specTitle   = "AI Platform control plane"
	specVersion = "1.0.0"

	specDescription = `The control plane for an internal AI model operations platform. Every platform object is
created, inspected, and released through this API; nothing in the product loop requires
kubectl.

The loop is: register a dataset version, submit a training job, let it produce a model
artifact, evaluate that artifact against a golden dataset, and only then route traffic to it.

**The release path is gated.** A model version moves
` + "`Registered -> Evaluating -> PromotionReady -> Canary -> Stable`" + `, and
` + "`POST /v1/routes/{model}/canary`" + ` refuses any version that is not ` + "`PromotionReady`" + `.
That refusal is the difference between a platform and a deployment script, so it is worth
trying on purpose.`
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

// Handler returns the router with every endpoint registered.
func (s *Server) Handler() http.Handler {
	mux, _ := s.build()
	// Huma serves the documentation page itself at /docs; this is only the front door.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs", http.StatusFound)
	})
	return s.observe(mux)
}

// Spec returns the OpenAPI document Huma derives from the registered operations. The snapshot in
// openapi.yaml is written from here, so nothing else has to describe the API.
func (s *Server) Spec() ([]byte, error) {
	_, api := s.build()
	return api.OpenAPI().YAML()
}

// build registers every operation on a fresh mux. Handler and Spec share it so the served routes
// and the generated document are produced by the same code path, never two drifting ones.
func (s *Server) build() (*http.ServeMux, huma.API) {
	mux := http.NewServeMux()
	api := humago.New(mux, s.config())
	s.register(api)
	return mux, api
}

func (s *Server) config() huma.Config {
	cfg := huma.DefaultConfig(specTitle, specVersion)
	// The default config installs a transformer that adds a "$schema" property to every response
	// body and a Link header beside it. The gateway, the controller, and ctl decode these bodies
	// into the structs in internal/model, so the wire format stays exactly those structs.
	cfg.CreateHooks = nil
	cfg.SchemasPath = ""
	cfg.OpenAPIPath = "/openapi"
	cfg.DocsPath = "/docs"
	cfg.DocsRenderer = huma.DocsRendererSwaggerUI
	cfg.DocsRendererConfig = map[string]any{
		"deepLinking":              true,
		"tryItOutEnabled":          true,
		"displayRequestDuration":   true,
		"docExpansion":             "list",
		"defaultModelsExpandDepth": 1,
	}
	cfg.Info.Description = specDescription
	cfg.Servers = []*huma.Server{{
		URL:         "http://localhost:8080",
		Description: "kind cluster, via the NodePort mapped by deploy/kind-cluster.yaml",
	}}
	cfg.Tags = []*huma.Tag{
		{Name: "datasets", Description: "Versioned, immutable inputs to training, evaluation, and batch work."},
		{Name: "training", Description: "Jobs that produce model artifacts."},
		{Name: "models", Description: "Registered model versions and their gate results."},
		{Name: "evaluation", Description: "Scoring a model version against a golden dataset."},
		{Name: "release", Description: "Routes, canaries, promotion, rollback, and replica counts."},
		{Name: "observability", Description: "Status, events, deployments, and metrics."},
	}
	return cfg
}

// --- middleware -----------------------------------------------------------

// observe logs every request and counts the ones that fail on our side.
func (s *Server) observe(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		s.requests.Add(1)
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.code() >= 500 {
			s.errors.Add(1)
		}
		s.log.Info("request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.code(),
			"ms", time.Since(start).Milliseconds())
	})
}

// statusRecorder remembers the status code so the log line and the error counter can see it.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(b)
}

func (w *statusRecorder) code() int {
	if w.status == 0 {
		return http.StatusOK
	}
	return w.status
}

// Unwrap lets http.ResponseController reach the real writer, which Huma uses to set read
// deadlines on request bodies.
func (w *statusRecorder) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// --- helpers --------------------------------------------------------------

// storeErr maps a store failure onto the status code a caller can act on. Huma renders these as
// RFC 7807 problem documents, so the CLI and the docs page read the same shape.
func storeErr(err error) error {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, store.ErrConflict):
		return huma.Error409Conflict(err.Error())
	default:
		return huma.Error500InternalServerError(err.Error())
	}
}

// textResponse carries a body Huma writes verbatim, for the two endpoints that are read by
// machines expecting plain text rather than JSON.
type textResponse struct {
	ContentType string `header:"Content-Type"`
	Body        []byte
}

func plainTextResponses(description string) map[string]*huma.Response {
	return map[string]*huma.Response{
		"200": {
			Description: description,
			Content: map[string]*huma.MediaType{
				"text/plain": {Schema: &huma.Schema{Type: "string"}},
			},
		},
	}
}

// --- operations -----------------------------------------------------------

// register declares every endpoint. Each one names its input and output types, and Huma turns
// those types into the request validation, the response encoding, and the OpenAPI document.
func (s *Server) register(api huma.API) {
	s.registerObservability(api)
	s.registerDatasets(api)
	s.registerTraining(api)
	s.registerModels(api)
	s.registerEvaluation(api)
	s.registerRelease(api)
}

// --- datasets -------------------------------------------------------------

type datasetInput struct {
	Body struct {
		Name     string `json:"name" minLength:"1" doc:"Logical dataset name."`
		Version  string `json:"version" minLength:"1" doc:"Dataset version; a name and version together are immutable."`
		Owner    string `json:"owner" minLength:"1" doc:"Owning team or person. Every platform object has an owner."`
		Rows     int    `json:"rows,omitempty" minimum:"1" default:"200" doc:"Number of rows the dataset holds."`
		Golden   bool   `json:"golden,omitempty" doc:"Golden datasets are the only ones evaluation will score against."`
		URI      string `json:"uri,omitempty" doc:"Where the rows live. Defaults to a path on the shared artifact volume."`
		Generate bool   `json:"generate,omitempty" doc:"Materialise deterministic synthetic rows at the URI."`
	}
}

type datasetOutput struct {
	Body model.Dataset
}

type datasetListOutput struct {
	Body []model.Dataset
}

func (s *Server) registerDatasets(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-dataset",
		Method:        http.MethodPost,
		Path:          "/v1/datasets",
		Tags:          []string{"datasets"},
		Summary:       "Register a dataset version",
		Description:   "Datasets are immutable: re-registering the same name and version is a conflict.",
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusConflict},
	}, s.createDataset)

	huma.Register(api, huma.Operation{
		OperationID: "list-datasets",
		Method:      http.MethodGet,
		Path:        "/v1/datasets",
		Tags:        []string{"datasets"},
		Summary:     "List every registered dataset version",
	}, func(ctx context.Context, _ *struct{}) (*datasetListOutput, error) {
		return &datasetListOutput{Body: s.st.ListDatasets()}, nil
	})
}

func (s *Server) createDataset(ctx context.Context, in *datasetInput) (*datasetOutput, error) {
	req := in.Body
	uri := req.URI
	if uri == "" {
		uri = filepath.Join(s.artifactDir, "datasets", req.Name+"-"+req.Version+".jsonl")
	}
	if req.Generate {
		if err := generateDataset(uri, req.Name+":"+req.Version, req.Rows); err != nil {
			return nil, huma.Error500InternalServerError("generate dataset: " + err.Error())
		}
	}
	d, err := s.st.CreateDataset(model.Dataset{
		Name: req.Name, Version: req.Version, Owner: req.Owner,
		URI: uri, Rows: req.Rows, Golden: req.Golden,
	})
	if err != nil {
		return nil, storeErr(err)
	}
	return &datasetOutput{Body: d}, nil
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

// --- training -------------------------------------------------------------

type trainingInput struct {
	Body struct {
		Name            string `json:"name" minLength:"1" doc:"Job name, unique across training jobs."`
		Owner           string `json:"owner" minLength:"1" doc:"Owning team or person."`
		BaseModel       string `json:"baseModel,omitempty" doc:"Base model the run fine-tunes."`
		DatasetRef      string `json:"datasetRef" minLength:"1" doc:"Training dataset, as name:version. It must already be registered."`
		ModelName       string `json:"modelName" minLength:"1" doc:"Logical model name the produced artifact registers under."`
		Steps           int    `json:"steps,omitempty" minimum:"1" default:"20" doc:"Training steps to run."`
		GPUs            int    `json:"gpus,omitempty" minimum:"1" default:"1" doc:"GPUs requested for the pod."`
		Priority        int    `json:"priority,omitempty" doc:"Scheduling priority; higher runs first when the queue is contended."`
		CheckpointEvery int    `json:"checkpointEvery,omitempty" minimum:"1" default:"5" doc:"Checkpoint interval in steps. A restarted job resumes from the last checkpoint."`
	}
}

type trainingOutput struct {
	Body model.TrainingJob
}

type trainingListOutput struct {
	Body []model.TrainingJob
}

type nameInput struct {
	Name string `path:"name" doc:"Object name."`
}

func (s *Server) registerTraining(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-training-job",
		Method:        http.MethodPost,
		Path:          "/v1/training-jobs",
		Tags:          []string{"training"},
		Summary:       "Submit a training job",
		Description:   "The job is queued immediately; the controller turns it into a Kubernetes Job and reports progress back through this object.",
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
	}, s.createTrainingJob)

	huma.Register(api, huma.Operation{
		OperationID: "list-training-jobs",
		Method:      http.MethodGet,
		Path:        "/v1/training-jobs",
		Tags:        []string{"training"},
		Summary:     "List every training job",
	}, func(ctx context.Context, _ *struct{}) (*trainingListOutput, error) {
		return &trainingListOutput{Body: s.st.ListTrainingJobs()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-training-job",
		Method:      http.MethodGet,
		Path:        "/v1/training-jobs/{name}",
		Tags:        []string{"training"},
		Summary:     "Read one training job",
		Errors:      []int{http.StatusNotFound},
	}, func(ctx context.Context, in *nameInput) (*trainingOutput, error) {
		j, err := s.st.GetTrainingJob(in.Name)
		if err != nil {
			return nil, storeErr(err)
		}
		return &trainingOutput{Body: j}, nil
	})
}

func (s *Server) createTrainingJob(ctx context.Context, in *trainingInput) (*trainingOutput, error) {
	req := in.Body
	// Lineage is the point of the platform: a job that cannot name a registered dataset cannot
	// later explain which data produced the model it registers.
	if _, err := s.st.GetDataset(req.DatasetRef); err != nil {
		return nil, huma.Error400BadRequest("datasetRef " + req.DatasetRef + " is not registered")
	}
	j, err := s.st.CreateTrainingJob(model.TrainingJob{
		Name: req.Name, Owner: req.Owner, BaseModel: req.BaseModel,
		DatasetRef: req.DatasetRef, ModelName: req.ModelName, Steps: req.Steps,
		GPUs: req.GPUs, Priority: req.Priority, CheckpointEvery: req.CheckpointEvery,
	})
	if err != nil {
		return nil, storeErr(err)
	}
	return &trainingOutput{Body: j}, nil
}

// --- models ---------------------------------------------------------------

type modelOutput struct {
	Body model.ModelVersion
}

type modelListOutput struct {
	Body []model.ModelVersion
}

func (s *Server) registerModels(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-models",
		Method:      http.MethodGet,
		Path:        "/v1/models",
		Tags:        []string{"models"},
		Summary:     "List every registered model version",
	}, func(ctx context.Context, _ *struct{}) (*modelListOutput, error) {
		return &modelListOutput{Body: s.st.ListModels()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-model",
		Method:      http.MethodGet,
		Path:        "/v1/models/{ref}",
		Tags:        []string{"models"},
		Summary:     "Read one model version",
		Errors:      []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		Ref string `path:"ref" doc:"Model reference, as name:version."`
	}) (*modelOutput, error) {
		m, err := s.st.GetModel(in.Ref)
		if err != nil {
			return nil, storeErr(err)
		}
		return &modelOutput{Body: m}, nil
	})
}

// --- evaluation -----------------------------------------------------------

// gatesBody is the request shape of model.Gates. A zero threshold means "do not gate on this
// dimension", which is why every field is optional here even though the stored object always
// carries all three.
type gatesBody struct {
	MinAccuracy     float64 `json:"minAccuracy,omitempty" minimum:"0" maximum:"1" doc:"Minimum accuracy the model must reach."`
	MaxP95LatencyMs float64 `json:"maxP95LatencyMs,omitempty" minimum:"0" doc:"Maximum p95 latency in milliseconds; 0 disables the latency gate."`
	MaxCostPerKReq  float64 `json:"maxCostPerKReq,omitempty" minimum:"0" doc:"Maximum cost per 1000 requests; 0 disables the cost gate."`
}

type evaluationInput struct {
	Body struct {
		Name       string     `json:"name" minLength:"1" doc:"Evaluation name, unique across evaluations."`
		Owner      string     `json:"owner,omitempty" doc:"Owning team or person."`
		ModelRef   string     `json:"modelRef" minLength:"1" doc:"Model version to score, as name:version."`
		DatasetRef string     `json:"datasetRef" minLength:"1" doc:"Golden dataset to score against, as name:version."`
		Gates      *gatesBody `json:"gates,omitempty" doc:"Promotion thresholds. Omit to use the platform defaults."`
	}
}

type evaluationOutput struct {
	Body model.EvaluationJob
}

type evaluationListOutput struct {
	Body []model.EvaluationJob
}

func (s *Server) registerEvaluation(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "create-evaluation",
		Method:        http.MethodPost,
		Path:          "/v1/evaluations",
		Tags:          []string{"evaluation"},
		Summary:       "Score a model version against a golden dataset",
		Description:   "Passing the gates is what moves a model version to PromotionReady, which is the only status a canary will accept.",
		DefaultStatus: http.StatusCreated,
		Errors:        []int{http.StatusBadRequest, http.StatusConflict},
	}, s.createEvaluation)

	huma.Register(api, huma.Operation{
		OperationID: "list-evaluations",
		Method:      http.MethodGet,
		Path:        "/v1/evaluations",
		Tags:        []string{"evaluation"},
		Summary:     "List every evaluation",
	}, func(ctx context.Context, _ *struct{}) (*evaluationListOutput, error) {
		return &evaluationListOutput{Body: s.st.ListEvaluations()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-evaluation",
		Method:      http.MethodGet,
		Path:        "/v1/evaluations/{name}",
		Tags:        []string{"evaluation"},
		Summary:     "Read one evaluation",
		Errors:      []int{http.StatusNotFound},
	}, func(ctx context.Context, in *nameInput) (*evaluationOutput, error) {
		e, err := s.st.GetEvaluation(in.Name)
		if err != nil {
			return nil, storeErr(err)
		}
		return &evaluationOutput{Body: e}, nil
	})
}

func (s *Server) createEvaluation(ctx context.Context, in *evaluationInput) (*evaluationOutput, error) {
	req := in.Body
	if _, err := s.st.GetModel(req.ModelRef); err != nil {
		return nil, huma.Error400BadRequest("model " + req.ModelRef + " is not registered")
	}
	ds, err := s.st.GetDataset(req.DatasetRef)
	if err != nil {
		return nil, huma.Error400BadRequest("dataset " + req.DatasetRef + " is not registered")
	}
	// Scoring against the same data a model trained on would pass every gate and mean nothing.
	if !ds.Golden {
		return nil, huma.Error400BadRequest("evaluation must run against a golden dataset")
	}
	gates := model.DefaultGates()
	if req.Gates != nil {
		gates = model.Gates{
			MinAccuracy:     req.Gates.MinAccuracy,
			MaxP95LatencyMs: req.Gates.MaxP95LatencyMs,
			MaxCostPerKReq:  req.Gates.MaxCostPerKReq,
		}
	}
	e, err := s.st.CreateEvaluation(model.EvaluationJob{
		Name: req.Name, Owner: req.Owner, ModelRef: req.ModelRef,
		DatasetRef: req.DatasetRef, Gates: gates,
	})
	if err != nil {
		return nil, storeErr(err)
	}
	return &evaluationOutput{Body: e}, nil
}

// --- routes and release ---------------------------------------------------

type routeInput struct {
	Model string `path:"model" doc:"Logical model name the route serves."`
}

type routeOutput struct {
	Body model.Route
}

type routeListOutput struct {
	Body []model.RouteView
}

type canaryInput struct {
	Model string `path:"model" doc:"Logical model name the route serves."`
	Body  struct {
		Version  string `json:"version" minLength:"1" doc:"Model version to put behind the canary lane."`
		Percent  int    `json:"percent,omitempty" minimum:"1" maximum:"100" default:"10" doc:"Percent of traffic the canary lane receives."`
		Replicas int    `json:"replicas,omitempty" minimum:"0" doc:"Replicas per served version; 0 keeps the current value."`
	}
}

type rollbackInput struct {
	Model string `path:"model" doc:"Logical model name the route serves."`
	// A pointer body is optional, so an operator can fire a rollback from the docs page or curl
	// without composing a payload first. Rollbacks happen under pressure.
	Body *struct {
		Reason string `json:"reason,omitempty" doc:"Why the canary is being dropped. Recorded on the event timeline."`
	}
}

type scaleInput struct {
	Model string `path:"model" doc:"Logical model name the route serves."`
	Body  struct {
		Replicas int `json:"replicas" minimum:"1" doc:"Replicas per served version. Online inference keeps a floor of one."`
	}
}

func (s *Server) registerRelease(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "list-routes",
		Method:      http.MethodGet,
		Path:        "/v1/routes",
		Tags:        []string{"release"},
		Summary:     "List every route with per-lane readiness",
		Description: "This is what the gateway polls. A lane that has been declared but has no ready replicas must not receive traffic, so the readiness counts travel with the weights.",
	}, func(ctx context.Context, _ *struct{}) (*routeListOutput, error) {
		return &routeListOutput{Body: s.routeViews()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-route",
		Method:      http.MethodGet,
		Path:        "/v1/routes/{model}",
		Tags:        []string{"release"},
		Summary:     "Read one route",
		Errors:      []int{http.StatusNotFound},
	}, func(ctx context.Context, in *routeInput) (*routeOutput, error) {
		rt, err := s.st.GetRoute(in.Model)
		if err != nil {
			return nil, storeErr(err)
		}
		return &routeOutput{Body: rt}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "start-canary",
		Method:      http.MethodPost,
		Path:        "/v1/routes/{model}/canary",
		Tags:        []string{"release"},
		Summary:     "Send a share of traffic to a new version",
		Description: "The gate lives here: a version that is not PromotionReady is refused with 409. This is the difference between a platform and a deployment script.",
		Errors:      []int{http.StatusNotFound, http.StatusConflict},
	}, s.startCanary)

	huma.Register(api, huma.Operation{
		OperationID: "promote-route",
		Method:      http.MethodPost,
		Path:        "/v1/routes/{model}/promote",
		Tags:        []string{"release"},
		Summary:     "Make the canary version stable",
		Description: "The version it replaces is retired, with the replacement named as the reason.",
		Errors:      []int{http.StatusNotFound, http.StatusConflict},
	}, s.promote)

	huma.Register(api, huma.Operation{
		OperationID: "rollback-route",
		Method:      http.MethodPost,
		Path:        "/v1/routes/{model}/rollback",
		Tags:        []string{"release"},
		Summary:     "Drop the canary immediately",
		Description: "Application code never changes; only the route does. The dropped version moves to PromotionBlocked with the reason recorded.",
		Errors:      []int{http.StatusNotFound, http.StatusConflict},
	}, s.rollback)

	huma.Register(api, huma.Operation{
		OperationID: "scale-route",
		Method:      http.MethodPost,
		Path:        "/v1/routes/{model}/scale",
		Tags:        []string{"release"},
		Summary:     "Set the replica count per served version",
		Errors:      []int{http.StatusNotFound},
	}, s.scale)
}

// routeViews joins routes with how many replicas each lane actually has ready.
func (s *Server) routeViews() []model.RouteView {
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
	return views
}

// startCanary is the only way new model code reaches production traffic, and it refuses any
// version that has not passed its evaluation gates.
func (s *Server) startCanary(ctx context.Context, in *canaryInput) (*routeOutput, error) {
	name, req := in.Model, in.Body
	ref := name + ":" + req.Version
	m, err := s.st.GetModel(ref)
	if err != nil {
		return nil, storeErr(err)
	}
	if m.Status != model.ModelPromotionReady && m.Status != model.ModelCanary {
		return nil, huma.Error409Conflict(
			fmt.Sprintf("model %s is %s: promotion requires passing evaluation gates", ref, m.Status))
	}
	rt, err := s.st.GetRoute(name)
	if errors.Is(err, store.ErrNotFound) {
		rt = model.Route{Model: name, Replicas: 1}
	} else if err != nil {
		return nil, storeErr(err)
	}
	if req.Replicas > 0 {
		rt.Replicas = req.Replicas
	}
	rt.CanaryVersion, rt.CanaryPercent = req.Version, req.Percent
	rt, err = s.st.PutRoute(rt, fmt.Sprintf("canary %s at %d%%", ref, req.Percent))
	if err != nil {
		return nil, storeErr(err)
	}
	if _, err := s.st.UpdateModel(ref, func(x *model.ModelVersion) {
		x.Status, x.Reason = model.ModelCanary, ""
	}); err != nil {
		return nil, storeErr(err)
	}
	return &routeOutput{Body: rt}, nil
}

// promote makes the canary version the stable one and retires the version it replaced.
func (s *Server) promote(ctx context.Context, in *routeInput) (*routeOutput, error) {
	name := in.Model
	rt, err := s.st.GetRoute(name)
	if err != nil {
		return nil, storeErr(err)
	}
	if rt.CanaryVersion == "" {
		return nil, huma.Error409Conflict("no canary version to promote")
	}
	previous := rt.StableVersion
	newStable := rt.CanaryVersion
	rt.StableVersion, rt.CanaryVersion, rt.CanaryPercent = newStable, "", 0
	rt, err = s.st.PutRoute(rt, fmt.Sprintf("promote %s:%s to stable (was %q)", name, newStable, previous))
	if err != nil {
		return nil, storeErr(err)
	}
	if _, err := s.st.UpdateModel(name+":"+newStable, func(x *model.ModelVersion) {
		x.Status = model.ModelStable
	}); err != nil {
		return nil, storeErr(err)
	}
	if previous != "" && previous != newStable {
		if _, err := s.st.UpdateModel(name+":"+previous, func(x *model.ModelVersion) {
			x.Status, x.Reason = model.ModelRetired, "replaced by "+newStable
		}); err != nil {
			return nil, storeErr(err)
		}
	}
	return &routeOutput{Body: rt}, nil
}

// rollback drops the canary immediately. Application code never changes; only the route does.
func (s *Server) rollback(ctx context.Context, in *rollbackInput) (*routeOutput, error) {
	name := in.Model
	rt, err := s.st.GetRoute(name)
	if err != nil {
		return nil, storeErr(err)
	}
	if rt.CanaryVersion == "" {
		return nil, huma.Error409Conflict("no canary version to roll back")
	}
	bad := rt.CanaryVersion
	reason := "operator rollback"
	if in.Body != nil && in.Body.Reason != "" {
		reason = in.Body.Reason
	}
	rt.CanaryVersion, rt.CanaryPercent = "", 0
	rt, err = s.st.PutRoute(rt, fmt.Sprintf("rollback %s:%s (%s)", name, bad, reason))
	if err != nil {
		return nil, storeErr(err)
	}
	if _, err := s.st.UpdateModel(name+":"+bad, func(x *model.ModelVersion) {
		x.Status, x.Reason = model.ModelPromotionBlocked, "rolled back: "+reason
	}); err != nil {
		return nil, storeErr(err)
	}
	return &routeOutput{Body: rt}, nil
}

func (s *Server) scale(ctx context.Context, in *scaleInput) (*routeOutput, error) {
	rt, err := s.st.GetRoute(in.Model)
	if err != nil {
		return nil, storeErr(err)
	}
	rt.Replicas = in.Body.Replicas
	rt, err = s.st.PutRoute(rt, fmt.Sprintf("scale %s to %d replicas", in.Model, in.Body.Replicas))
	if err != nil {
		return nil, storeErr(err)
	}
	return &routeOutput{Body: rt}, nil
}

// --- observability --------------------------------------------------------

type deploymentListOutput struct {
	Body []model.Deployment
}

type eventsInput struct {
	Name  string `query:"name" doc:"Only events for this object name."`
	Limit int    `query:"limit" minimum:"1" maximum:"1000" default:"50" doc:"Most recent events to return."`
}

type eventListOutput struct {
	Body []model.Event
}

// statusReport is the single page an operator opens first: what is running, what is blocked,
// what is serving traffic.
type statusReport struct {
	Datasets     []model.Dataset       `json:"datasets"`
	TrainingJobs []model.TrainingJob   `json:"trainingJobs"`
	Models       []model.ModelVersion  `json:"models"`
	Evaluations  []model.EvaluationJob `json:"evaluations"`
	Routes       []model.Route         `json:"routes"`
	Deployments  []model.Deployment    `json:"deployments"`
	Events       []model.Event         `json:"events"`
}

type statusOutput struct {
	Body statusReport
}

func (s *Server) registerObservability(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "healthz",
		Method:      http.MethodGet,
		Path:        "/healthz",
		Tags:        []string{"observability"},
		Summary:     "Liveness probe",
		Responses:   plainTextResponses("The control plane is serving."),
	}, func(ctx context.Context, _ *struct{}) (*textResponse, error) {
		return &textResponse{ContentType: "text/plain; charset=utf-8", Body: []byte("ok\n")}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "metrics",
		Method:      http.MethodGet,
		Path:        "/metrics",
		Tags:        []string{"observability"},
		Summary:     "Prometheus metrics",
		Description: "API counters plus gauges for training job phases and model version statuses.",
		Responses:   plainTextResponses("Metrics in the Prometheus text exposition format."),
	}, func(ctx context.Context, _ *struct{}) (*textResponse, error) {
		return &textResponse{ContentType: "text/plain; version=0.0.4", Body: s.metrics()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-deployments",
		Method:      http.MethodGet,
		Path:        "/v1/deployments",
		Tags:        []string{"observability"},
		Summary:     "List the model servers the controller keeps running",
	}, func(ctx context.Context, _ *struct{}) (*deploymentListOutput, error) {
		return &deploymentListOutput{Body: s.st.ListDeployments()}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-events",
		Method:      http.MethodGet,
		Path:        "/v1/events",
		Tags:        []string{"observability"},
		Summary:     "Read the event log",
		Description: "Every state transition emits an event. This log is the incident timeline.",
	}, func(ctx context.Context, in *eventsInput) (*eventListOutput, error) {
		return &eventListOutput{Body: s.st.Events(in.Name, in.Limit)}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "get-status",
		Method:      http.MethodGet,
		Path:        "/v1/status",
		Tags:        []string{"observability"},
		Summary:     "One page view of the whole platform",
	}, func(ctx context.Context, _ *struct{}) (*statusOutput, error) {
		return &statusOutput{Body: statusReport{
			Datasets:     s.st.ListDatasets(),
			TrainingJobs: s.st.ListTrainingJobs(),
			Models:       s.st.ListModels(),
			Evaluations:  s.st.ListEvaluations(),
			Routes:       s.st.ListRoutes(),
			Deployments:  s.st.ListDeployments(),
			Events:       s.st.Events("", 20),
		}}, nil
	})
}

func (s *Server) metrics() []byte {
	var b []byte
	add := func(format string, args ...any) { b = append(b, fmt.Sprintf(format, args...)...) }

	add("# HELP aiplatform_api_requests_total Control plane API requests.\n")
	add("# TYPE aiplatform_api_requests_total counter\n")
	add("aiplatform_api_requests_total %d\n", s.requests.Load())
	add("# HELP aiplatform_api_errors_total Control plane API 5xx responses.\n")
	add("# TYPE aiplatform_api_errors_total counter\n")
	add("aiplatform_api_errors_total %d\n", s.errors.Load())

	byPhase := map[model.Phase]int{}
	for _, j := range s.st.ListTrainingJobs() {
		byPhase[j.Phase]++
	}
	add("# HELP aiplatform_training_jobs Training jobs by phase.\n")
	add("# TYPE aiplatform_training_jobs gauge\n")
	for _, p := range []model.Phase{model.PhasePending, model.PhaseQueued, model.PhaseRunning, model.PhaseSucceeded, model.PhaseFailed} {
		add("aiplatform_training_jobs{phase=%q} %d\n", p, byPhase[p])
	}

	byStatus := map[model.ModelStatus]int{}
	for _, m := range s.st.ListModels() {
		byStatus[m.Status]++
	}
	add("# HELP aiplatform_model_versions Model versions by status.\n")
	add("# TYPE aiplatform_model_versions gauge\n")
	// Iterated in funnel order so the output is stable between scrapes; a map range is not.
	for _, st := range []model.ModelStatus{
		model.ModelRegistered, model.ModelEvaluating, model.ModelPromotionBlocked,
		model.ModelPromotionReady, model.ModelCanary, model.ModelStable, model.ModelRetired,
	} {
		if n, ok := byStatus[st]; ok {
			add("aiplatform_model_versions{status=%q} %d\n", st, n)
		}
	}
	return b
}
