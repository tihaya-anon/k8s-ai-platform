// Package ctl is the operator and engineer CLI. It is a thin client over the control-plane API:
// anything the CLI can do, a script or another service can do with plain HTTP.
package ctl

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/tihaya-anon/k8s-ai-platform/internal/model"
)

// Run dispatches one CLI invocation. args excludes the program name and the "ctl" verb.
func Run(args []string) error {
	api := envOr("PLATFORM_API", "http://localhost:8080")
	gw := envOr("PLATFORM_GATEWAY", "http://localhost:8081")
	if len(args) == 0 {
		usage()
		return fmt.Errorf("a command is required")
	}
	c := &client{api: api, gateway: gw, http: &http.Client{Timeout: 30 * time.Second}}
	switch args[0] {
	case "status":
		return c.status()
	case "dataset":
		return c.dataset(args[1:])
	case "train":
		return c.train(args[1:])
	case "model":
		return c.models(args[1:])
	case "eval":
		return c.eval(args[1:])
	case "route":
		return c.route(args[1:])
	case "canary":
		return c.canary(args[1:])
	case "promote":
		return c.promote(args[1:])
	case "rollback":
		return c.rollback(args[1:])
	case "scale":
		return c.scale(args[1:])
	case "events":
		return c.events(args[1:])
	case "infer":
		return c.infer(args[1:])
	case "wait":
		return c.wait(args[1:])
	case "help", "-h", "--help":
		usage()
		return nil
	default:
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `platform ctl <command>

  status                       one page view of the platform
  dataset list
  dataset create               -name -version -owner [-rows] [-golden] [-generate]
  train submit                 -name -owner -model -dataset [-base] [-steps] [-checkpoint-every]
  train list | train get       -name
  model list
  eval run                     -name -model -dataset [-min-accuracy] [-max-p95] [-max-cost]
  eval list
  route list
  canary                       -model -version [-percent] [-replicas]
  promote                      -model
  rollback                     -model [-reason]
  scale                        -model -replicas
  events                       [-name] [-limit]
  infer                        -model -input [-n] [-version]
  wait                         -kind training|eval -name [-timeout]

Environment: PLATFORM_API (default http://localhost:8080), PLATFORM_GATEWAY (default http://localhost:8081)
`)
}

type client struct {
	api     string
	gateway string
	http    *http.Client
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func (c *client) do(method, url string, body any, out any) error {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("%s: %s", resp.Status, problemDetail(raw))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(raw, out)
}

// problemDetail renders the control plane's RFC 7807 error body as one line. Field errors are
// included because a rejected request is usually rejected over one field, and naming it saves a
// round trip through the docs page.
func problemDetail(raw []byte) string {
	var p struct {
		Title  string `json:"title"`
		Detail string `json:"detail"`
		Errors []struct {
			Message  string `json:"message"`
			Location string `json:"location"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &p); err != nil || (p.Detail == "" && p.Title == "") {
		return strings.TrimSpace(string(raw))
	}
	msg := p.Detail
	if msg == "" {
		msg = p.Title
	}
	for _, e := range p.Errors {
		msg += "\n  " + e.Location + ": " + e.Message
	}
	return msg
}

func tw() *tabwriter.Writer { return tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0) }

// --- commands -------------------------------------------------------------

func (c *client) status() error {
	var s struct {
		Datasets     []model.Dataset       `json:"datasets"`
		TrainingJobs []model.TrainingJob   `json:"trainingJobs"`
		Models       []model.ModelVersion  `json:"models"`
		Evaluations  []model.EvaluationJob `json:"evaluations"`
		Routes       []model.Route         `json:"routes"`
		Deployments  []model.Deployment    `json:"deployments"`
		Events       []model.Event         `json:"events"`
	}
	if err := c.do(http.MethodGet, c.api+"/v1/status", nil, &s); err != nil {
		return err
	}
	w := tw()
	fmt.Fprintln(w, "DATASETS\tOWNER\tROWS\tGOLDEN")
	for _, d := range s.Datasets {
		fmt.Fprintf(w, "%s\t%s\t%d\t%t\n", d.Ref(), d.Owner, d.Rows, d.Golden)
	}
	fmt.Fprintln(w, "\nTRAINING JOB\tPHASE\tMODEL\tATTEMPT\tREASON")
	for _, j := range s.TrainingJobs {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n", j.Name, j.Phase, j.ModelRef, j.Attempt, j.Reason)
	}
	fmt.Fprintln(w, "\nMODEL\tSTATUS\tACCURACY\tP95 MS\tCOST/1K\tREASON")
	for _, m := range s.Models {
		fmt.Fprintf(w, "%s\t%s\t%.4f\t%.1f\t%.3f\t%s\n",
			m.Ref(), m.Status, m.Metrics.Accuracy, m.Metrics.P95LatencyMs, m.Metrics.CostPerKReq, m.Reason)
	}
	fmt.Fprintln(w, "\nEVALUATION\tPHASE\tMODEL\tPASSED\tREASON")
	for _, e := range s.Evaluations {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", e.Name, e.Phase, e.ModelRef, boolPtr(e.Passed), e.Reason)
	}
	fmt.Fprintln(w, "\nROUTE\tSTABLE\tCANARY\tCANARY %\tREPLICAS")
	for _, r := range s.Routes {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\n", r.Model, or(r.StableVersion, "-"), or(r.CanaryVersion, "-"), r.CanaryPercent, r.Replicas)
	}
	fmt.Fprintln(w, "\nMODEL SERVER\tREADY/DESIRED")
	for _, d := range s.Deployments {
		fmt.Fprintf(w, "%s\t%d/%d\n", d.ModelRef, d.Ready, d.Replicas)
	}
	fmt.Fprintln(w, "\nRECENT EVENTS")
	for _, e := range s.Events {
		fmt.Fprintf(w, "%s\t%s/%s\t%s\t%s\n", e.Time.Format("15:04:05"), e.Kind, e.Name, e.Type, e.Message)
	}
	return w.Flush()
}

func (c *client) dataset(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("dataset list|create")
	}
	switch args[0] {
	case "list":
		var out []model.Dataset
		if err := c.do(http.MethodGet, c.api+"/v1/datasets", nil, &out); err != nil {
			return err
		}
		w := tw()
		fmt.Fprintln(w, "REF\tOWNER\tROWS\tGOLDEN\tURI")
		for _, d := range out {
			fmt.Fprintf(w, "%s\t%s\t%d\t%t\t%s\n", d.Ref(), d.Owner, d.Rows, d.Golden, d.URI)
		}
		return w.Flush()
	case "create":
		fs := flag.NewFlagSet("dataset create", flag.ExitOnError)
		name := fs.String("name", "", "dataset name")
		version := fs.String("version", "", "dataset version")
		owner := fs.String("owner", "", "owning team or person")
		rows := fs.Int("rows", 200, "number of rows")
		golden := fs.Bool("golden", false, "is this an evaluation golden set")
		generate := fs.Bool("generate", true, "materialise synthetic rows on the shared volume")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		var out model.Dataset
		err := c.do(http.MethodPost, c.api+"/v1/datasets", map[string]any{
			"name": *name, "version": *version, "owner": *owner,
			"rows": *rows, "golden": *golden, "generate": *generate,
		}, &out)
		if err != nil {
			return err
		}
		fmt.Printf("dataset %s registered (%d rows) at %s\n", out.Ref(), out.Rows, out.URI)
		return nil
	}
	return fmt.Errorf("unknown dataset command %q", args[0])
}

func (c *client) train(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("train submit|list|get")
	}
	switch args[0] {
	case "submit":
		fs := flag.NewFlagSet("train submit", flag.ExitOnError)
		name := fs.String("name", "", "job name")
		owner := fs.String("owner", "", "owning team or person")
		modelName := fs.String("model", "", "logical model name the job produces")
		dataset := fs.String("dataset", "", "training dataset ref, name:version")
		base := fs.String("base", "base-small", "base model")
		steps := fs.Int("steps", 20, "training steps")
		ckpt := fs.Int("checkpoint-every", 5, "checkpoint interval in steps")
		gpus := fs.Int("gpus", 1, "requested GPUs")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		var out model.TrainingJob
		err := c.do(http.MethodPost, c.api+"/v1/training-jobs", map[string]any{
			"name": *name, "owner": *owner, "modelName": *modelName, "datasetRef": *dataset,
			"baseModel": *base, "steps": *steps, "checkpointEvery": *ckpt, "gpus": *gpus,
		}, &out)
		if err != nil {
			return err
		}
		fmt.Printf("training job %s submitted (%s)\n", out.Name, out.Phase)
		return nil
	case "list":
		var out []model.TrainingJob
		if err := c.do(http.MethodGet, c.api+"/v1/training-jobs", nil, &out); err != nil {
			return err
		}
		w := tw()
		fmt.Fprintln(w, "NAME\tPHASE\tOWNER\tDATASET\tMODEL\tK8S JOB\tREASON")
		for _, j := range out {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
				j.Name, j.Phase, j.Owner, j.DatasetRef, j.ModelRef, j.K8sJob, j.Reason)
		}
		return w.Flush()
	case "get":
		fs := flag.NewFlagSet("train get", flag.ExitOnError)
		name := fs.String("name", "", "job name")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		var out model.TrainingJob
		if err := c.do(http.MethodGet, c.api+"/v1/training-jobs/"+*name, nil, &out); err != nil {
			return err
		}
		return printJSON(out)
	}
	return fmt.Errorf("unknown train command %q", args[0])
}

func (c *client) models(args []string) error {
	var out []model.ModelVersion
	if err := c.do(http.MethodGet, c.api+"/v1/models", nil, &out); err != nil {
		return err
	}
	w := tw()
	fmt.Fprintln(w, "REF\tSTATUS\tACCURACY\tP95 MS\tCOST/1K\tSOURCE JOB\tREASON")
	for _, m := range out {
		fmt.Fprintf(w, "%s\t%s\t%.4f\t%.1f\t%.3f\t%s\t%s\n", m.Ref(), m.Status,
			m.Metrics.Accuracy, m.Metrics.P95LatencyMs, m.Metrics.CostPerKReq, m.SourceJob, m.Reason)
	}
	return w.Flush()
}

func (c *client) eval(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("eval run|list")
	}
	switch args[0] {
	case "run":
		fs := flag.NewFlagSet("eval run", flag.ExitOnError)
		name := fs.String("name", "", "evaluation name")
		owner := fs.String("owner", "", "owning team or person")
		modelRef := fs.String("model", "", "model ref, name:version")
		dataset := fs.String("dataset", "", "golden dataset ref, name:version")
		minAcc := fs.Float64("min-accuracy", 0.80, "promotion gate: minimum accuracy")
		maxP95 := fs.Float64("max-p95", 250, "promotion gate: maximum p95 latency in ms")
		maxCost := fs.Float64("max-cost", 5.0, "promotion gate: maximum cost per 1k requests")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		var out model.EvaluationJob
		err := c.do(http.MethodPost, c.api+"/v1/evaluations", map[string]any{
			"name": *name, "owner": *owner, "modelRef": *modelRef, "datasetRef": *dataset,
			"gates": map[string]any{
				"minAccuracy": *minAcc, "maxP95LatencyMs": *maxP95, "maxCostPerKReq": *maxCost,
			},
		}, &out)
		if err != nil {
			return err
		}
		fmt.Printf("evaluation %s submitted for %s\n", out.Name, out.ModelRef)
		return nil
	case "list":
		var out []model.EvaluationJob
		if err := c.do(http.MethodGet, c.api+"/v1/evaluations", nil, &out); err != nil {
			return err
		}
		w := tw()
		fmt.Fprintln(w, "NAME\tPHASE\tMODEL\tACCURACY\tP95 MS\tPASSED\tREASON")
		for _, e := range out {
			acc, p95 := 0.0, 0.0
			if e.Metrics != nil {
				acc, p95 = e.Metrics.Accuracy, e.Metrics.P95LatencyMs
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%.4f\t%.1f\t%s\t%s\n", e.Name, e.Phase, e.ModelRef, acc, p95, boolPtr(e.Passed), e.Reason)
		}
		return w.Flush()
	}
	return fmt.Errorf("unknown eval command %q", args[0])
}

func (c *client) route(args []string) error {
	var out []model.Route
	if err := c.do(http.MethodGet, c.api+"/v1/routes", nil, &out); err != nil {
		return err
	}
	w := tw()
	fmt.Fprintln(w, "MODEL\tSTABLE\tCANARY\tCANARY %\tREPLICAS\tUPDATED")
	for _, r := range out {
		fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n", r.Model, or(r.StableVersion, "-"),
			or(r.CanaryVersion, "-"), r.CanaryPercent, r.Replicas, r.UpdatedAt.Format(time.RFC3339))
	}
	return w.Flush()
}

func (c *client) canary(args []string) error {
	fs := flag.NewFlagSet("canary", flag.ExitOnError)
	m := fs.String("model", "", "logical model name")
	v := fs.String("version", "", "model version to canary")
	pct := fs.Int("percent", 10, "percent of traffic sent to the canary")
	replicas := fs.Int("replicas", 0, "replicas per served version, 0 keeps the current value")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var out model.Route
	err := c.do(http.MethodPost, c.api+"/v1/routes/"+*m+"/canary",
		map[string]any{"version": *v, "percent": *pct, "replicas": *replicas}, &out)
	if err != nil {
		return err
	}
	fmt.Printf("canary %s:%s at %d%% (stable %s)\n", out.Model, out.CanaryVersion, out.CanaryPercent, or(out.StableVersion, "none"))
	return nil
}

func (c *client) promote(args []string) error {
	fs := flag.NewFlagSet("promote", flag.ExitOnError)
	m := fs.String("model", "", "logical model name")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var out model.Route
	if err := c.do(http.MethodPost, c.api+"/v1/routes/"+*m+"/promote", map[string]any{}, &out); err != nil {
		return err
	}
	fmt.Printf("promoted %s:%s to stable\n", out.Model, out.StableVersion)
	return nil
}

func (c *client) rollback(args []string) error {
	fs := flag.NewFlagSet("rollback", flag.ExitOnError)
	m := fs.String("model", "", "logical model name")
	reason := fs.String("reason", "", "why the canary is being rolled back")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var out model.Route
	if err := c.do(http.MethodPost, c.api+"/v1/routes/"+*m+"/rollback", map[string]any{"reason": *reason}, &out); err != nil {
		return err
	}
	fmt.Printf("rolled back canary on %s, stable stays %s\n", out.Model, or(out.StableVersion, "none"))
	return nil
}

func (c *client) scale(args []string) error {
	fs := flag.NewFlagSet("scale", flag.ExitOnError)
	m := fs.String("model", "", "logical model name")
	replicas := fs.Int("replicas", 1, "replicas per served version")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var out model.Route
	if err := c.do(http.MethodPost, c.api+"/v1/routes/"+*m+"/scale", map[string]any{"replicas": *replicas}, &out); err != nil {
		return err
	}
	fmt.Printf("%s now runs %d replicas per served version\n", out.Model, out.Replicas)
	return nil
}

func (c *client) events(args []string) error {
	fs := flag.NewFlagSet("events", flag.ExitOnError)
	name := fs.String("name", "", "filter by object name")
	limit := fs.Int("limit", 50, "number of events")
	if err := fs.Parse(args); err != nil {
		return err
	}
	var out []model.Event
	url := fmt.Sprintf("%s/v1/events?name=%s&limit=%d", c.api, *name, *limit)
	if err := c.do(http.MethodGet, url, nil, &out); err != nil {
		return err
	}
	w := tw()
	fmt.Fprintln(w, "TIME\tOBJECT\tTYPE\tMESSAGE")
	for _, e := range out {
		fmt.Fprintf(w, "%s\t%s/%s\t%s\t%s\n", e.Time.Format(time.RFC3339), e.Kind, e.Name, e.Type, e.Message)
	}
	return w.Flush()
}

// infer sends traffic through the gateway and summarises which version answered, which is how a
// canary is judged.
func (c *client) infer(args []string) error {
	fs := flag.NewFlagSet("infer", flag.ExitOnError)
	m := fs.String("model", "", "logical model name")
	input := fs.String("input", "hello", "request input")
	n := fs.Int("n", 1, "number of requests")
	version := fs.String("version", "", "pin one version instead of following the route")
	if err := fs.Parse(args); err != nil {
		return err
	}
	counts := map[string]int{}
	var totalMs float64
	var failures int
	for i := 0; i < *n; i++ {
		var out struct {
			Version   string  `json:"version"`
			Lane      string  `json:"lane"`
			Output    string  `json:"output"`
			LatencyMs float64 `json:"latencyMs"`
		}
		body := map[string]any{"model": *m, "input": fmt.Sprintf("%s #%d", *input, i), "version": *version}
		if err := c.do(http.MethodPost, c.gateway+"/v1/infer", body, &out); err != nil {
			failures++
			if failures == 1 {
				fmt.Printf("request failed: %v\n", err)
			}
			continue
		}
		counts[out.Version+" ("+out.Lane+")"]++
		totalMs += out.LatencyMs
		if *n == 1 {
			fmt.Println(out.Output)
		}
	}
	ok := *n - failures
	w := tw()
	fmt.Fprintln(w, "\nVERSION\tREQUESTS\tSHARE")
	for k, v := range counts {
		fmt.Fprintf(w, "%s\t%d\t%.0f%%\n", k, v, float64(v)/float64(max(ok, 1))*100)
	}
	fmt.Fprintf(w, "failed\t%d\t\n", failures)
	if ok > 0 {
		fmt.Fprintf(w, "mean latency\t%.1f ms\t\n", totalMs/float64(ok))
	}
	return w.Flush()
}

// wait blocks until a job reaches a terminal phase, which keeps demo scripts honest.
func (c *client) wait(args []string) error {
	fs := flag.NewFlagSet("wait", flag.ExitOnError)
	kind := fs.String("kind", "training", "training or eval")
	name := fs.String("name", "", "object name")
	timeout := fs.Duration("timeout", 5*time.Minute, "how long to wait")
	if err := fs.Parse(args); err != nil {
		return err
	}
	path := "/v1/training-jobs/"
	if *kind == "eval" {
		path = "/v1/evaluations/"
	}
	deadline := time.Now().Add(*timeout)
	last := ""
	for time.Now().Before(deadline) {
		var out struct {
			Phase  model.Phase `json:"phase"`
			Reason string      `json:"reason"`
		}
		if err := c.do(http.MethodGet, c.api+path+*name, nil, &out); err != nil {
			return err
		}
		if string(out.Phase) != last {
			fmt.Printf("%s %s: %s %s\n", *kind, *name, out.Phase, out.Reason)
			last = string(out.Phase)
		}
		switch out.Phase {
		case model.PhaseSucceeded:
			return nil
		case model.PhaseFailed:
			return fmt.Errorf("%s %s failed: %s", *kind, *name, out.Reason)
		}
		time.Sleep(2 * time.Second)
	}
	return fmt.Errorf("timed out waiting for %s %s", *kind, *name)
}

func printJSON(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return nil
}

func boolPtr(b *bool) string {
	if b == nil {
		return "-"
	}
	if *b {
		return "yes"
	}
	return "no"
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
