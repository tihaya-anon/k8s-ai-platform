package controller

import (
	"io"
	"log/slog"
	"testing"

	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
	"github.com/tihaya-anon/k8s-ai-platform/internal/store"
)

func lookup(models map[string]model.ModelVersion) func(string) (model.ModelVersion, error) {
	return func(ref string) (model.ModelVersion, error) {
		m, ok := models[ref]
		if !ok {
			return m, store.ErrNotFound
		}
		return m, nil
	}
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// Routes store bare versions while the registry is keyed by "name:version". Getting this wrong
// silently serves nothing, so it is pinned here.
func TestDesiredServersResolvesFullModelRefs(t *testing.T) {
	models := map[string]model.ModelVersion{
		"support:v1": {Name: "support", Version: "v1"},
		"support:v2": {Name: "support", Version: "v2"},
	}
	routes := []model.Route{{
		Model: "support", StableVersion: "v1", CanaryVersion: "v2", CanaryPercent: 20, Replicas: 3,
	}}

	wanted, replicas := desiredServers(routes, lookup(models), quiet())
	if len(wanted) != 2 {
		t.Fatalf("wanted %d servers, want both the stable and the canary version: %v", len(wanted), wanted)
	}
	if _, ok := wanted["support:v1"]; !ok {
		t.Fatal("stable version missing from the desired set")
	}
	if replicas["support:v2"] != 3 {
		t.Fatalf("canary replicas = %d, want the route's 3", replicas["support:v2"])
	}
}

func TestDesiredServersSkipsUnknownVersions(t *testing.T) {
	routes := []model.Route{{Model: "support", StableVersion: "v9"}}
	wanted, _ := desiredServers(routes, lookup(map[string]model.ModelVersion{}), quiet())
	if len(wanted) != 0 {
		t.Fatalf("wanted = %v, want nothing for a route pointing at a missing version", wanted)
	}
}

func TestDesiredServersTakesTheHighestReplicaCount(t *testing.T) {
	models := map[string]model.ModelVersion{"support:v1": {Name: "support", Version: "v1"}}
	routes := []model.Route{
		{Model: "support", StableVersion: "v1", Replicas: 2},
		{Model: "support", StableVersion: "v1", Replicas: 5},
	}
	_, replicas := desiredServers(routes, lookup(models), quiet())
	if replicas["support:v1"] != 5 {
		t.Fatalf("replicas = %d, want 5", replicas["support:v1"])
	}
}
