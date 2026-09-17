// Package gateway is the front door for online inference. Applications address a logical model
// name; the gateway resolves the current route, splits traffic between stable and canary, and
// returns the version that actually served each request.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/rand"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/tihaya-anon/k8s-ai-platform/internal/controller"
	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
)

// Gateway routes inference requests to model servers.
type Gateway struct {
	controlPlane string
	namespace    string
	log          *slog.Logger
	http         *http.Client
	rand         *rand.Rand
	randMu       sync.Mutex

	mu     sync.RWMutex
	routes map[string]model.RouteView

	statsMu sync.Mutex
	stats   map[string]*versionStats
}

type versionStats struct {
	Requests  int64
	Errors    int64
	LatencyUs int64
}

// New builds a gateway that reads routes from the control plane API.
func New(controlPlane, namespace string, log *slog.Logger) *Gateway {
	return &Gateway{
		controlPlane: controlPlane,
		namespace:    namespace,
		log:          log,
		http:         &http.Client{Timeout: 10 * time.Second},
		rand:         rand.New(rand.NewSource(time.Now().UnixNano())),
		routes:       map[string]model.RouteView{},
		stats:        map[string]*versionStats{},
	}
}

// WatchRoutes refreshes the route table on an interval. A stale table is better than a failed
// request, so refresh errors only log.
func (g *Gateway) WatchRoutes(ctx context.Context, interval time.Duration) {
	g.refresh(ctx)
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			g.refresh(ctx)
		}
	}
}

func (g *Gateway) refresh(ctx context.Context) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.controlPlane+"/v1/routes", nil)
	if err != nil {
		return
	}
	resp, err := g.http.Do(req)
	if err != nil {
		g.log.Warn("route refresh failed, serving previous table", "err", err)
		return
	}
	defer resp.Body.Close()
	var routes []model.RouteView
	if err := json.NewDecoder(resp.Body).Decode(&routes); err != nil {
		g.log.Warn("route refresh decode failed", "err", err)
		return
	}
	next := make(map[string]model.RouteView, len(routes))
	for _, r := range routes {
		next[r.Model] = r
	}
	g.mu.Lock()
	g.routes = next
	g.mu.Unlock()
}

// Handler returns the gateway's HTTP handler.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /metrics", g.metrics)
	mux.HandleFunc("GET /v1/routes", g.showRoutes)
	mux.HandleFunc("POST /v1/infer", g.infer)
	return mux
}

type inferRequest struct {
	Model string `json:"model"`
	Input string `json:"input"`
	// Version pins one version, which is how a developer reproduces a bad canary response.
	Version string `json:"version,omitempty"`
}

type inferResponse struct {
	RequestID  string  `json:"requestId"`
	Model      string  `json:"model"`
	Version    string  `json:"version"`
	Lane       string  `json:"lane"`
	Output     string  `json:"output"`
	Confidence float64 `json:"confidence"`
	Tokens     int     `json:"tokens"`
	LatencyMs  float64 `json:"latencyMs"`
}

func (g *Gateway) infer(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	requestID := fmt.Sprintf("req-%d", time.Now().UnixNano())
	w.Header().Set("X-Request-Id", requestID)

	var req inferRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Model == "" || req.Input == "" {
		writeErr(w, http.StatusBadRequest, requestID, "client", "model and input are required")
		return
	}

	g.mu.RLock()
	route, ok := g.routes[req.Model]
	g.mu.RUnlock()
	if !ok {
		writeErr(w, http.StatusNotFound, requestID, "gateway", "no route for model "+req.Model)
		return
	}

	version, lane := g.pick(route, req.Version)
	if version == "" {
		writeErr(w, http.StatusServiceUnavailable, requestID, "gateway", "route has no servable version")
		return
	}
	w.Header().Set("X-Model-Version", req.Model+":"+version)

	svc := controller.ServerNameFor(req.Model, version)
	url := fmt.Sprintf("http://%s.%s.svc:9000/infer", svc, g.namespace)
	body, _ := json.Marshal(map[string]string{"input": req.Input})
	upstream, err := g.http.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		g.record(req.Model, version, time.Since(start), true)
		writeErr(w, http.StatusBadGateway, requestID, "model-server", err.Error())
		return
	}
	defer upstream.Body.Close()
	raw, _ := io.ReadAll(upstream.Body)
	if upstream.StatusCode >= 300 {
		g.record(req.Model, version, time.Since(start), true)
		writeErr(w, http.StatusBadGateway, requestID, "model-server",
			fmt.Sprintf("upstream %s: %s", upstream.Status, string(raw)))
		return
	}
	var ms struct {
		Output     string  `json:"output"`
		Confidence float64 `json:"confidence"`
		Tokens     int     `json:"tokens"`
	}
	if err := json.Unmarshal(raw, &ms); err != nil {
		g.record(req.Model, version, time.Since(start), true)
		writeErr(w, http.StatusBadGateway, requestID, "model-server", "unreadable upstream response")
		return
	}

	g.record(req.Model, version, time.Since(start), false)
	writeJSON(w, http.StatusOK, inferResponse{
		RequestID: requestID, Model: req.Model, Version: version, Lane: lane,
		Output: ms.Output, Confidence: ms.Confidence, Tokens: ms.Tokens,
		LatencyMs: float64(time.Since(start).Microseconds()) / 1000,
	})
}

// pick chooses stable or canary for one request. A lane with no ready replicas is skipped: a
// freshly declared canary has a Service before it has endpoints, and those requests would fail for
// no reason the caller could act on. A pinned version wins if the route serves it, because pinning
// is how a developer reproduces a specific version's behaviour.
func (g *Gateway) pick(r model.RouteView, pinned string) (string, string) {
	if pinned != "" {
		switch pinned {
		case r.StableVersion:
			return pinned, "stable"
		case r.CanaryVersion:
			return pinned, "canary"
		default:
			return "", ""
		}
	}
	canaryUp := r.CanaryVersion != "" && r.CanaryReady > 0
	stableUp := r.StableVersion != "" && r.StableReady > 0

	if canaryUp && r.CanaryPercent > 0 {
		g.randMu.Lock()
		n := g.rand.Intn(100)
		g.randMu.Unlock()
		if n < r.CanaryPercent || !stableUp {
			return r.CanaryVersion, "canary"
		}
	}
	if stableUp {
		return r.StableVersion, "stable"
	}
	if canaryUp {
		// A brand new model with only a canary lane still has to answer.
		return r.CanaryVersion, "canary"
	}
	return "", ""
}

func (g *Gateway) record(modelName, version string, d time.Duration, failed bool) {
	key := modelName + ":" + version
	g.statsMu.Lock()
	defer g.statsMu.Unlock()
	s, ok := g.stats[key]
	if !ok {
		s = &versionStats{}
		g.stats[key] = s
	}
	s.Requests++
	s.LatencyUs += d.Microseconds()
	if failed {
		s.Errors++
	}
}

func (g *Gateway) showRoutes(w http.ResponseWriter, r *http.Request) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]model.RouteView, 0, len(g.routes))
	for _, r := range g.routes {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Model < out[j].Model })
	writeJSON(w, http.StatusOK, out)
}

// metrics exposes the per-version split that makes a canary decision possible.
func (g *Gateway) metrics(w http.ResponseWriter, r *http.Request) {
	g.statsMu.Lock()
	defer g.statsMu.Unlock()
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# TYPE aiplatform_gateway_requests_total counter\n")
	for k, s := range g.stats {
		fmt.Fprintf(w, "aiplatform_gateway_requests_total{model_version=%q} %d\n", k, s.Requests)
	}
	fmt.Fprintf(w, "# TYPE aiplatform_gateway_errors_total counter\n")
	for k, s := range g.stats {
		fmt.Fprintf(w, "aiplatform_gateway_errors_total{model_version=%q} %d\n", k, s.Errors)
	}
	fmt.Fprintf(w, "# TYPE aiplatform_gateway_latency_seconds_total counter\n")
	for k, s := range g.stats {
		fmt.Fprintf(w, "aiplatform_gateway_latency_seconds_total{model_version=%q} %.6f\n",
			k, float64(s.LatencyUs)/1e6)
	}
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr always names the failing layer, so a caller can tell a bad request from a bad model
// server from a bad route without reading platform logs.
func writeErr(w http.ResponseWriter, code int, requestID, source, msg string) {
	writeJSON(w, code, map[string]string{
		"requestId": requestID,
		"errorFrom": source,
		"error":     msg,
	})
}
