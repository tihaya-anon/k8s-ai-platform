package model

import "testing"

func TestGatesCheck(t *testing.T) {
	g := Gates{MinAccuracy: 0.8, MaxP95LatencyMs: 200, MaxCostPerKReq: 3}
	tests := []struct {
		name string
		m    Metrics
		want bool
	}{
		{"all pass", Metrics{Accuracy: 0.9, P95LatencyMs: 100, CostPerKReq: 1}, true},
		{"accuracy fails", Metrics{Accuracy: 0.7, P95LatencyMs: 100, CostPerKReq: 1}, false},
		{"latency fails", Metrics{Accuracy: 0.9, P95LatencyMs: 300, CostPerKReq: 1}, false},
		{"cost fails", Metrics{Accuracy: 0.9, P95LatencyMs: 100, CostPerKReq: 9}, false},
		{"exactly at gate passes", Metrics{Accuracy: 0.8, P95LatencyMs: 200, CostPerKReq: 3}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, why := g.Check(tc.m)
			if got != tc.want {
				t.Fatalf("Check() = %v (%s), want %v", got, why, tc.want)
			}
			if !got && why == "" {
				t.Fatal("a failing gate must explain itself")
			}
		})
	}
}

func TestUnsetGatesIgnoreLatencyAndCost(t *testing.T) {
	g := Gates{MinAccuracy: 0.5}
	if ok, why := g.Check(Metrics{Accuracy: 0.6, P95LatencyMs: 9999, CostPerKReq: 9999}); !ok {
		t.Fatalf("unset latency and cost gates must not block: %s", why)
	}
}
