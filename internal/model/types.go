// Package model holds the platform's logical objects. Users of the platform interact with these,
// never with raw Kubernetes objects (see docs/draft.md, "Required Platform Behavior").
package model

import "time"

// Phase is the status of any long running platform object.
type Phase string

const (
	PhasePending   Phase = "Pending"
	PhaseQueued    Phase = "Queued"
	PhaseRunning   Phase = "Running"
	PhaseSucceeded Phase = "Succeeded"
	PhaseFailed    Phase = "Failed"
)

// ModelStatus tracks where a model version sits in the release funnel.
type ModelStatus string

const (
	ModelRegistered       ModelStatus = "Registered"
	ModelEvaluating       ModelStatus = "Evaluating"
	ModelPromotionBlocked ModelStatus = "PromotionBlocked"
	ModelPromotionReady   ModelStatus = "PromotionReady"
	ModelCanary           ModelStatus = "Canary"
	ModelStable           ModelStatus = "Stable"
	ModelRetired          ModelStatus = "Retired"
)

// Dataset is a versioned, immutable input to training, evaluation, or batch work.
type Dataset struct {
	Name      string    `json:"name"`
	Version   string    `json:"version"`
	Owner     string    `json:"owner"`
	URI       string    `json:"uri"`
	Rows      int       `json:"rows"`
	Golden    bool      `json:"golden"`
	CreatedAt time.Time `json:"createdAt"`
}

// Ref is "name:version", the platform's addressing form for datasets and models.
func (d Dataset) Ref() string { return d.Name + ":" + d.Version }

// TrainingJob is a fine-tuning run that produces a model artifact.
type TrainingJob struct {
	Name       string `json:"name"`
	Owner      string `json:"owner"`
	BaseModel  string `json:"baseModel"`
	DatasetRef string `json:"datasetRef"`
	ModelName  string `json:"modelName"`
	Steps      int    `json:"steps"`
	GPUs       int    `json:"gpus"`
	Priority   int    `json:"priority"`
	// CheckpointEvery is measured in steps; 0 disables checkpointing.
	CheckpointEvery int `json:"checkpointEvery"`

	Phase       Phase      `json:"phase"`
	Reason      string     `json:"reason,omitempty"`
	K8sJob      string     `json:"k8sJob,omitempty"`
	ArtifactURI string     `json:"artifactUri,omitempty"`
	ModelRef    string     `json:"modelRef,omitempty"`
	Attempt     int        `json:"attempt"`
	CreatedAt   time.Time  `json:"createdAt"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
}

// ModelVersion is a registered artifact. It cannot serve traffic until evaluation gates pass.
type ModelVersion struct {
	Name        string      `json:"name"`
	Version     string      `json:"version"`
	Owner       string      `json:"owner"`
	ArtifactURI string      `json:"artifactUri"`
	SourceJob   string      `json:"sourceJob,omitempty"`
	DatasetRef  string      `json:"datasetRef,omitempty"`
	Status      ModelStatus `json:"status"`
	Reason      string      `json:"reason,omitempty"`
	Metrics     Metrics     `json:"metrics"`
	CreatedAt   time.Time   `json:"createdAt"`
}

func (m ModelVersion) Ref() string { return m.Name + ":" + m.Version }

// Metrics are the quality, latency, and cost numbers promotion gates read.
type Metrics struct {
	Accuracy     float64 `json:"accuracy"`
	P95LatencyMs float64 `json:"p95LatencyMs"`
	CostPerKReq  float64 `json:"costPerKReq"`
	Samples      int     `json:"samples"`
}

// Gates are the thresholds a model version must clear before promotion.
type Gates struct {
	MinAccuracy     float64 `json:"minAccuracy"`
	MaxP95LatencyMs float64 `json:"maxP95LatencyMs"`
	MaxCostPerKReq  float64 `json:"maxCostPerKReq"`
}

// DefaultGates are used when an evaluation job does not carry its own.
func DefaultGates() Gates {
	return Gates{MinAccuracy: 0.80, MaxP95LatencyMs: 250, MaxCostPerKReq: 5.0}
}

// Check reports whether metrics pass, and why not when they do not.
func (g Gates) Check(m Metrics) (bool, string) {
	switch {
	case m.Accuracy < g.MinAccuracy:
		return false, "accuracy below gate"
	case g.MaxP95LatencyMs > 0 && m.P95LatencyMs > g.MaxP95LatencyMs:
		return false, "p95 latency above gate"
	case g.MaxCostPerKReq > 0 && m.CostPerKReq > g.MaxCostPerKReq:
		return false, "cost above gate"
	}
	return true, ""
}

// EvaluationJob scores a model version against a golden dataset.
type EvaluationJob struct {
	Name       string     `json:"name"`
	Owner      string     `json:"owner"`
	ModelRef   string     `json:"modelRef"`
	DatasetRef string     `json:"datasetRef"`
	Gates      Gates      `json:"gates"`
	Phase      Phase      `json:"phase"`
	Reason     string     `json:"reason,omitempty"`
	K8sJob     string     `json:"k8sJob,omitempty"`
	Metrics    *Metrics   `json:"metrics,omitempty"`
	Passed     *bool      `json:"passed,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
}

// Route is the logical endpoint application developers call. Application code addresses
// Route.Model and never a concrete version.
type Route struct {
	Model         string    `json:"model"`
	StableVersion string    `json:"stableVersion,omitempty"`
	CanaryVersion string    `json:"canaryVersion,omitempty"`
	CanaryPercent int       `json:"canaryPercent"`
	Replicas      int       `json:"replicas"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// RouteView is a route plus how many replicas each lane actually has ready. The gateway needs
// this: a lane that has been declared but has no ready pods must not receive traffic, or a canary
// would fail requests for the seconds between the route change and the first ready replica.
type RouteView struct {
	Route
	StableReady int `json:"stableReady"`
	CanaryReady int `json:"canaryReady"`
}

// Deployment is the platform's record of one model server the controller keeps running.
type Deployment struct {
	ModelRef  string    `json:"modelRef"`
	Name      string    `json:"name"`
	Replicas  int       `json:"replicas"`
	Ready     int       `json:"ready"`
	Service   string    `json:"service"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Event is the append-only audit trail an incident responder replays.
type Event struct {
	Time    time.Time `json:"time"`
	Kind    string    `json:"kind"`
	Name    string    `json:"name"`
	Type    string    `json:"type"`
	Message string    `json:"message"`
}
