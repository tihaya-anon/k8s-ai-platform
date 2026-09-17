package controller

import (
	"strings"
	"testing"

	"github.com/tihaya-anon/k8s-ai-platform/internal/k8s"
	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
)

func testConfig() Config {
	return Config{
		Namespace: "ai-platform", Image: "ai-platform:dev", ImagePullPolicy: "IfNotPresent",
		ServiceAccount: "ai-platform", ArtifactHost: "/artifacts", ArtifactMount: "/artifacts",
	}
}

func TestTrainingJobCarriesResumeAndLineageEnv(t *testing.T) {
	j := model.TrainingJob{Name: "ft-support-1", ModelName: "support", DatasetRef: "docs:v1", Steps: 12, CheckpointEvery: 4}
	ds := model.Dataset{Name: "docs", Version: "v1", URI: "/artifacts/datasets/docs-v1.jsonl"}
	name, obj := testConfig().TrainingJob(j, ds, "v3")

	if name != "train-ft-support-1" {
		t.Fatalf("job name = %q", name)
	}
	env := containerEnv(t, obj, "spec", "template", "spec")
	for k, want := range map[string]string{
		"MODEL_NAME": "support", "MODEL_VERSION": "v3", "STEPS": "12",
		"CHECKPOINT_EVERY": "4", "DATASET_URI": ds.URI,
	} {
		if env[k] != want {
			t.Fatalf("env %s = %q, want %q", k, env[k], want)
		}
	}
	lm := objectAt(t, obj, "metadata", "labels")
	if lm[LabelManagedBy] != ManagedBy || lm[LabelJob] != "ft-support-1" {
		t.Fatalf("labels = %v, want the platform's ownership labels", lm)
	}
}

func TestModelServerNameIsStableAndDNSSafe(t *testing.T) {
	m := model.ModelVersion{Name: "Support_Assistant", Version: "v12"}
	name := ServerName(m)
	if name != ServerNameFor("Support_Assistant", "v12") {
		t.Fatal("the gateway and the controller must derive the same service name")
	}
	if strings.ContainsAny(name, "_:. ") || strings.ToLower(name) != name {
		t.Fatalf("name %q is not DNS-1123 safe", name)
	}
}

func TestModelServerAndServiceShareASelector(t *testing.T) {
	m := model.ModelVersion{Name: "support", Version: "v1", ArtifactURI: "/artifacts/models/support/v1"}
	cfg := testConfig()
	_, dep := cfg.ModelServer(m, 3)
	_, svc := cfg.ModelService(m)

	dl := objectAt(t, dep, "spec", "template", "metadata", "labels")
	ss := objectAt(t, svc, "spec", "selector")
	if dl["platform.ai/server"] != ss["platform.ai/server"] || ss["platform.ai/server"] == nil {
		t.Fatalf("service selector %v does not match pod labels %v", ss, dl)
	}
	if got := k8s.NestedInt(dep, "spec", "replicas"); got != 3 {
		t.Fatalf("replicas = %d, want 3", got)
	}
	env := containerEnv(t, dep, "spec", "template", "spec")
	if env["MODEL_REF"] != "support:v1" || env["ARTIFACT_URI"] != m.ArtifactURI {
		t.Fatalf("model server env = %v, want the version it serves", env)
	}
}

func TestSplitRef(t *testing.T) {
	name, version := splitRef("support-assistant:v7")
	if name != "support-assistant" || version != "v7" {
		t.Fatalf("splitRef = %q, %q", name, version)
	}
}

// objectAt reads a nested Object out of a rendered manifest.
func objectAt(t *testing.T, obj k8s.Object, path ...string) k8s.Object {
	t.Helper()
	v, ok := k8s.Nested(obj, path...)
	if !ok {
		t.Fatalf("no object at %v", path)
	}
	o, ok := v.(k8s.Object)
	if !ok {
		t.Fatalf("value at %v is %T, want k8s.Object", path, v)
	}
	return o
}

// containerEnv flattens the first container's env into a map.
func containerEnv(t *testing.T, obj k8s.Object, path ...string) map[string]string {
	t.Helper()
	sm := objectAt(t, obj, path...)
	containers, _ := sm["containers"].([]k8s.Object)
	if len(containers) == 0 {
		t.Fatal("pod spec has no containers")
	}
	env, _ := containers[0]["env"].([]k8s.Object)
	out := map[string]string{}
	for _, e := range env {
		k, _ := e["name"].(string)
		v, _ := e["value"].(string)
		out[k] = v
	}
	return out
}

// An unstable pod template makes every reconcile look like a change, which would roll the model
// server's pods forever.
func TestRenderedManifestsAreStableAcrossCalls(t *testing.T) {
	cfg := testConfig()
	m := model.ModelVersion{Name: "support", Version: "v1", ArtifactURI: "/artifacts/models/support/v1"}
	first := renderEnvList(t, cfg, m)
	for i := 0; i < 20; i++ {
		if got := renderEnvList(t, cfg, m); got != first {
			t.Fatalf("env order changed between renders:\n%s\n%s", first, got)
		}
	}
}

func renderEnvList(t *testing.T, cfg Config, m model.ModelVersion) string {
	t.Helper()
	_, dep := cfg.ModelServer(m, 1)
	sm := objectAt(t, dep, "spec", "template", "spec")
	containers, _ := sm["containers"].([]k8s.Object)
	envs, _ := containers[0]["env"].([]k8s.Object)
	var b strings.Builder
	for _, e := range envs {
		b.WriteString(e["name"].(string) + "=" + e["value"].(string) + ";")
	}
	return b.String()
}
