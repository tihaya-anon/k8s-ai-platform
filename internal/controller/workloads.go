package controller

import (
	"fmt"
	"sort"
	"strings"

	"github.com/tihaya-anon/k8s-ai-platform/internal/k8s"
	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
)

// Label keys the platform stamps on every Kubernetes object it owns. Everything the controller
// creates is selectable by app.kubernetes.io/managed-by=ai-platform, which is what makes cleanup
// and incident triage tractable.
const (
	LabelManagedBy = "app.kubernetes.io/managed-by"
	LabelComponent = "app.kubernetes.io/component"
	LabelJob       = "platform.ai/job"
	LabelModel     = "platform.ai/model"
	LabelVersion   = "platform.ai/model-version"
	ManagedBy      = "ai-platform"
)

// Config is everything the controller needs to turn platform objects into Kubernetes objects.
type Config struct {
	Namespace       string
	Image           string
	ImagePullPolicy string
	ServiceAccount  string
	ArtifactHost    string // path on the node that holds datasets, checkpoints, and artifacts
	ArtifactMount   string // path inside containers
}

// k8sName makes a DNS-1123 safe object name.
func k8sName(parts ...string) string {
	s := strings.ToLower(strings.Join(parts, "-"))
	s = strings.NewReplacer(":", "-", "_", "-", ".", "-", "/", "-").Replace(s)
	if len(s) > 63 {
		s = s[:63]
	}
	return strings.Trim(s, "-")
}

func (c Config) artifactVolume() (k8s.Object, k8s.Object) {
	vol := k8s.Object{
		"name":     "artifacts",
		"hostPath": k8s.Object{"path": c.ArtifactHost, "type": "DirectoryOrCreate"},
	}
	mount := k8s.Object{"name": "artifacts", "mountPath": c.ArtifactMount}
	return vol, mount
}

// env renders environment variables in a stable order. Map iteration order is random, and an
// unstable pod template makes every reconcile look like a spec change: the Deployment would roll
// its pods forever.
func env(kv map[string]string) []k8s.Object {
	keys := make([]string, 0, len(kv))
	for k := range kv {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]k8s.Object, 0, len(keys))
	for _, k := range keys {
		out = append(out, k8s.Object{"name": k, "value": kv[k]})
	}
	return out
}

// TrainingJob renders a batch/v1 Job that runs one training attempt. Restarts resume from the
// latest checkpoint, so a preempted or crashed attempt does not start from step zero.
func (c Config) TrainingJob(j model.TrainingJob, ds model.Dataset, version string) (string, k8s.Object) {
	name := k8sName("train", j.Name)
	vol, mount := c.artifactVolume()
	labels := k8s.Object{
		LabelManagedBy: ManagedBy,
		LabelComponent: "training",
		LabelJob:       j.Name,
		LabelModel:     j.ModelName,
		LabelVersion:   version,
	}
	obj := k8s.Object{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata": k8s.Object{
			"name":      name,
			"namespace": c.Namespace,
			"labels":    labels,
		},
		"spec": k8s.Object{
			"backoffLimit":            2,
			"ttlSecondsAfterFinished": 3600,
			"template": k8s.Object{
				"metadata": k8s.Object{"labels": labels},
				"spec": k8s.Object{
					"restartPolicy":      "Never",
					"serviceAccountName": c.ServiceAccount,
					"volumes":            []k8s.Object{vol},
					"containers": []k8s.Object{{
						"name":            "trainer",
						"image":           c.Image,
						"imagePullPolicy": c.ImagePullPolicy,
						"args":            []string{"train"},
						"volumeMounts":    []k8s.Object{mount},
						"env": env(map[string]string{
							"JOB_NAME":         j.Name,
							"MODEL_NAME":       j.ModelName,
							"MODEL_VERSION":    version,
							"BASE_MODEL":       j.BaseModel,
							"DATASET_REF":      j.DatasetRef,
							"DATASET_URI":      ds.URI,
							"STEPS":            fmt.Sprint(j.Steps),
							"CHECKPOINT_EVERY": fmt.Sprint(j.CheckpointEvery),
							"ARTIFACT_DIR":     c.ArtifactMount,
						}),
						// GPU requests are simulated: the MVP asks for CPU shares so the same
						// spec shape works before real GPU nodes exist.
						"resources": k8s.Object{
							"requests": k8s.Object{"cpu": "100m", "memory": "64Mi"},
							"limits":   k8s.Object{"cpu": "500m", "memory": "256Mi"},
						},
					}},
				},
			},
		},
	}
	return name, obj
}

// EvaluationJob renders the Job that scores a model version against a golden dataset.
func (c Config) EvaluationJob(e model.EvaluationJob, m model.ModelVersion, ds model.Dataset) (string, k8s.Object) {
	name := k8sName("eval", e.Name)
	vol, mount := c.artifactVolume()
	labels := k8s.Object{
		LabelManagedBy: ManagedBy,
		LabelComponent: "evaluation",
		LabelJob:       e.Name,
		LabelModel:     m.Name,
		LabelVersion:   m.Version,
	}
	obj := k8s.Object{
		"apiVersion": "batch/v1",
		"kind":       "Job",
		"metadata":   k8s.Object{"name": name, "namespace": c.Namespace, "labels": labels},
		"spec": k8s.Object{
			"backoffLimit":            1,
			"ttlSecondsAfterFinished": 3600,
			"template": k8s.Object{
				"metadata": k8s.Object{"labels": labels},
				"spec": k8s.Object{
					"restartPolicy":      "Never",
					"serviceAccountName": c.ServiceAccount,
					"volumes":            []k8s.Object{vol},
					"containers": []k8s.Object{{
						"name":            "evaluator",
						"image":           c.Image,
						"imagePullPolicy": c.ImagePullPolicy,
						"args":            []string{"eval"},
						"volumeMounts":    []k8s.Object{mount},
						"env": env(map[string]string{
							"EVAL_NAME":    e.Name,
							"MODEL_REF":    m.Ref(),
							"ARTIFACT_URI": m.ArtifactURI,
							"DATASET_REF":  ds.Ref(),
							"DATASET_URI":  ds.URI,
							"ARTIFACT_DIR": c.ArtifactMount,
						}),
						"resources": k8s.Object{
							"requests": k8s.Object{"cpu": "100m", "memory": "64Mi"},
							"limits":   k8s.Object{"cpu": "500m", "memory": "256Mi"},
						},
					}},
				},
			},
		},
	}
	return name, obj
}

// ServerName is the Deployment and Service name for one served model version.
func ServerName(m model.ModelVersion) string { return ServerNameFor(m.Name, m.Version) }

// ServerNameFor is the same naming rule addressed by name and version, which is what the gateway
// has when it resolves a route to a Service.
func ServerNameFor(name, version string) string { return k8sName("ms", name, version) }

// ModelServer renders the Deployment that serves one model version. One Deployment per version is
// what makes canary and rollback a routing decision rather than a redeploy.
func (c Config) ModelServer(m model.ModelVersion, replicas int) (string, k8s.Object) {
	name := ServerName(m)
	vol, mount := c.artifactVolume()
	labels := k8s.Object{
		LabelManagedBy: ManagedBy,
		LabelComponent: "model-server",
		LabelModel:     m.Name,
		LabelVersion:   m.Version,
	}
	obj := k8s.Object{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata":   k8s.Object{"name": name, "namespace": c.Namespace, "labels": labels},
		"spec": k8s.Object{
			"replicas": replicas,
			"selector": k8s.Object{"matchLabels": k8s.Object{"platform.ai/server": name}},
			"template": k8s.Object{
				"metadata": k8s.Object{"labels": merge(labels, k8s.Object{"platform.ai/server": name})},
				"spec": k8s.Object{
					"serviceAccountName": c.ServiceAccount,
					"volumes":            []k8s.Object{vol},
					"containers": []k8s.Object{{
						"name":            "model-server",
						"image":           c.Image,
						"imagePullPolicy": c.ImagePullPolicy,
						"args":            []string{"serve"},
						"ports":           []k8s.Object{{"containerPort": 9000, "name": "http"}},
						"volumeMounts":    []k8s.Object{mount},
						"env": env(map[string]string{
							"MODEL_REF":    m.Ref(),
							"ARTIFACT_URI": m.ArtifactURI,
							"ARTIFACT_DIR": c.ArtifactMount,
							"LISTEN_ADDR":  ":9000",
						}),
						"readinessProbe": k8s.Object{
							"httpGet":             k8s.Object{"path": "/healthz", "port": 9000},
							"initialDelaySeconds": 1,
							"periodSeconds":       2,
						},
						"resources": k8s.Object{
							"requests": k8s.Object{"cpu": "50m", "memory": "32Mi"},
							"limits":   k8s.Object{"cpu": "500m", "memory": "128Mi"},
						},
					}},
				},
			},
		},
	}
	return name, obj
}

// ModelService renders the ClusterIP Service the gateway sends requests to.
func (c Config) ModelService(m model.ModelVersion) (string, k8s.Object) {
	name := ServerName(m)
	obj := k8s.Object{
		"apiVersion": "v1",
		"kind":       "Service",
		"metadata": k8s.Object{
			"name":      name,
			"namespace": c.Namespace,
			"labels": k8s.Object{
				LabelManagedBy: ManagedBy,
				LabelComponent: "model-server",
				LabelModel:     m.Name,
				LabelVersion:   m.Version,
			},
		},
		"spec": k8s.Object{
			"selector": k8s.Object{"platform.ai/server": name},
			"ports":    []k8s.Object{{"port": 9000, "targetPort": 9000, "name": "http"}},
		},
	}
	return name, obj
}

func merge(a, b k8s.Object) k8s.Object {
	out := k8s.Object{}
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
