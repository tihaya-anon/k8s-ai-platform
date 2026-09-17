package gateway

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
)

func newGateway(t *testing.T, routes ...model.RouteView) *Gateway {
	t.Helper()
	g := New("", "ai-platform", slog.New(slog.NewTextHandler(io.Discard, nil)))
	for _, r := range routes {
		g.routes[r.Model] = r
	}
	return g
}

func TestPickSplitsTrafficByWeight(t *testing.T) {
	g := newGateway(t)
	route := model.RouteView{
		Route:       model.Route{Model: "support", StableVersion: "v1", CanaryVersion: "v2", CanaryPercent: 30},
		StableReady: 1, CanaryReady: 1,
	}
	counts := map[string]int{}
	for i := 0; i < 10000; i++ {
		v, lane := g.pick(route, "")
		counts[v]++
		if (v == "v2") != (lane == "canary") {
			t.Fatalf("version %q reported as lane %q", v, lane)
		}
	}
	share := float64(counts["v2"]) / 10000
	if share < 0.27 || share > 0.33 {
		t.Fatalf("canary share = %.3f, want roughly 0.30", share)
	}
}

func TestPickWithoutCanaryAlwaysStable(t *testing.T) {
	g := newGateway(t)
	route := model.RouteView{Route: model.Route{Model: "support", StableVersion: "v1"}, StableReady: 2}
	for i := 0; i < 100; i++ {
		if v, lane := g.pick(route, ""); v != "v1" || lane != "stable" {
			t.Fatalf("pick = %q/%q, want v1/stable", v, lane)
		}
	}
}

// A route with only a canary lane still has to answer: this is the first version of a new model.
func TestPickFallsBackToCanaryWhenNoStable(t *testing.T) {
	g := newGateway(t)
	route := model.RouteView{Route: model.Route{Model: "support", CanaryVersion: "v1"}, CanaryReady: 1}
	if v, lane := g.pick(route, ""); v != "v1" || lane != "canary" {
		t.Fatalf("pick = %q/%q, want v1/canary", v, lane)
	}
}

func TestPinnedVersionMustBeOnTheRoute(t *testing.T) {
	g := newGateway(t)
	route := model.RouteView{
		Route:       model.Route{Model: "support", StableVersion: "v1", CanaryVersion: "v2", CanaryPercent: 50},
		StableReady: 1, CanaryReady: 1,
	}
	if v, _ := g.pick(route, "v2"); v != "v2" {
		t.Fatalf("pinned canary = %q, want v2", v)
	}
	if v, _ := g.pick(route, "v9"); v != "" {
		t.Fatalf("pinned unknown version = %q, want no version", v)
	}
}

// A canary gets a Service before it has endpoints. Until a replica is ready, stable must absorb
// the traffic, otherwise starting a canary fails requests for no reason the caller can act on.
func TestPickSkipsCanaryWithNoReadyReplicas(t *testing.T) {
	g := newGateway(t)
	route := model.RouteView{
		Route:       model.Route{Model: "support", StableVersion: "v1", CanaryVersion: "v2", CanaryPercent: 100},
		StableReady: 1, CanaryReady: 0,
	}
	for i := 0; i < 100; i++ {
		if v, lane := g.pick(route, ""); v != "v1" || lane != "stable" {
			t.Fatalf("pick = %q/%q, want the ready stable lane", v, lane)
		}
	}
}

// With nothing ready anywhere the gateway must say so rather than dial a dead Service.
func TestPickReturnsNothingWhenNoLaneIsReady(t *testing.T) {
	g := newGateway(t)
	route := model.RouteView{Route: model.Route{Model: "support", StableVersion: "v1", CanaryVersion: "v2", CanaryPercent: 50}}
	if v, _ := g.pick(route, ""); v != "" {
		t.Fatalf("pick = %q, want no version", v)
	}
}

func TestInferWithoutRouteIsNotFound(t *testing.T) {
	g := newGateway(t)
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/infer", "application/json",
		strings.NewReader(`{"model":"missing","input":"hi"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// A caller must be able to tell whose fault a failure is without reading platform logs.
	if body["errorFrom"] != "gateway" || body["requestId"] == "" {
		t.Fatalf("error body = %v, want a gateway-attributed error with a request id", body)
	}
}

func TestInferRejectsEmptyInput(t *testing.T) {
	g := newGateway(t, model.RouteView{Route: model.Route{Model: "support", StableVersion: "v1"}, StableReady: 1})
	srv := httptest.NewServer(g.Handler())
	defer srv.Close()

	resp, err := http.Post(srv.URL+"/v1/infer", "application/json", strings.NewReader(`{"model":"support"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

func TestMetricsReportPerVersionTraffic(t *testing.T) {
	g := newGateway(t)
	g.record("support", "v1", 1000, false)
	g.record("support", "v2", 2000, true)

	rec := httptest.NewRecorder()
	g.metrics(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	for _, want := range []string{
		`aiplatform_gateway_requests_total{model_version="support:v1"} 1`,
		`aiplatform_gateway_errors_total{model_version="support:v2"} 1`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics missing %q:\n%s", want, body)
		}
	}
}
