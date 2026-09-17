// Package serve is the model server. It loads one model version's artifact and answers inference
// requests. The "model" is simulated, but everything around it -- readiness, version reporting,
// per-request metadata, latency, and metrics -- behaves the way a real model server must.
package serve

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Card is the artifact a training run writes next to its weights.
type Card struct {
	ModelRef      string  `json:"modelRef"`
	TrainAccuracy float64 `json:"trainAccuracy"`
	Quality       float64 `json:"quality"`
	Steps         int     `json:"steps"`
	BaseLatencyMs float64 `json:"baseLatencyMs"`
}

// Server answers /infer for exactly one model version.
type Server struct {
	card     Card
	modelRef string
	log      *slog.Logger

	requests atomic.Int64
	errors   atomic.Int64
	latency  atomic.Int64 // cumulative microseconds
}

// New loads the model card from the artifact directory.
func New(modelRef, artifactURI string, log *slog.Logger) (*Server, error) {
	b, err := os.ReadFile(filepath.Join(artifactURI, "model.json"))
	if err != nil {
		return nil, fmt.Errorf("load model artifact for %s: %w", modelRef, err)
	}
	var c Card
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("parse model artifact for %s: %w", modelRef, err)
	}
	if c.BaseLatencyMs <= 0 {
		c.BaseLatencyMs = 15
	}
	return &Server{card: c, modelRef: modelRef, log: log}, nil
}

// Request is the inference request body.
type Request struct {
	Input string `json:"input"`
}

// Response carries the prediction plus the metadata a caller needs to debug a bad rollout.
type Response struct {
	Output     string  `json:"output"`
	ModelRef   string  `json:"modelRef"`
	Confidence float64 `json:"confidence"`
	Tokens     int     `json:"tokens"`
	LatencyMs  float64 `json:"latencyMs"`
}

// Handler returns the model server's HTTP handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "ok %s\n", s.modelRef)
	})
	mux.HandleFunc("GET /metrics", s.metrics)
	mux.HandleFunc("POST /infer", s.infer)
	return mux
}

func (s *Server) infer(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.requests.Add(1)
	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.errors.Add(1)
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}
	if req.Input == "" {
		s.errors.Add(1)
		http.Error(w, "input is required", http.StatusBadRequest)
		return
	}

	// Simulated work: a better-trained model answers faster and more confidently.
	jitter := float64(hash(req.Input)%20) / 10.0
	delay := time.Duration((s.card.BaseLatencyMs*(1.4-s.card.Quality) + jitter) * float64(time.Millisecond))
	time.Sleep(delay)

	resp := Response{
		Output:     fmt.Sprintf("[%s] answer to %q", s.modelRef, req.Input),
		ModelRef:   s.modelRef,
		Confidence: round(s.card.Quality*0.9+float64(hash(req.Input)%10)/100, 3),
		Tokens:     len(req.Input)/4 + 8,
		LatencyMs:  round(float64(time.Since(start).Microseconds())/1000, 2),
	}
	s.latency.Add(time.Since(start).Microseconds())
	w.Header().Set("X-Model-Version", s.modelRef)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# TYPE aiplatform_model_requests_total counter\n")
	fmt.Fprintf(w, "aiplatform_model_requests_total{model=%q} %d\n", s.modelRef, s.requests.Load())
	fmt.Fprintf(w, "# TYPE aiplatform_model_errors_total counter\n")
	fmt.Fprintf(w, "aiplatform_model_errors_total{model=%q} %d\n", s.modelRef, s.errors.Load())
	fmt.Fprintf(w, "# TYPE aiplatform_model_latency_seconds_total counter\n")
	fmt.Fprintf(w, "aiplatform_model_latency_seconds_total{model=%q} %.6f\n",
		s.modelRef, float64(s.latency.Load())/1e6)
}

func hash(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}

func round(v float64, places int) float64 {
	p := 1.0
	for i := 0; i < places; i++ {
		p *= 10
	}
	return float64(int(v*p+0.5)) / p
}
