package worker

import (
	"bufio"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// EvalConfig is read from the environment the controller stamps on the evaluation Job.
type EvalConfig struct {
	EvalName    string
	ModelRef    string
	ArtifactURI string
	DatasetRef  string
	DatasetURI  string
	ArtifactDir string
}

// EvalConfigFromEnv builds the config an evaluation container runs with.
func EvalConfigFromEnv() EvalConfig {
	return EvalConfig{
		EvalName:    os.Getenv("EVAL_NAME"),
		ModelRef:    os.Getenv("MODEL_REF"),
		ArtifactURI: os.Getenv("ARTIFACT_URI"),
		DatasetRef:  os.Getenv("DATASET_REF"),
		DatasetURI:  os.Getenv("DATASET_URI"),
		ArtifactDir: envOr("ARTIFACT_DIR", "/artifacts"),
	}
}

// Report is what the controller reads back to apply promotion gates.
type Report struct {
	Accuracy     float64 `json:"accuracy"`
	P95LatencyMs float64 `json:"p95LatencyMs"`
	CostPerKReq  float64 `json:"costPerKReq"`
	Samples      int     `json:"samples"`

	ModelRef   string    `json:"modelRef"`
	DatasetRef string    `json:"datasetRef"`
	Failures   []string  `json:"failures"`
	RanAt      time.Time `json:"ranAt"`
}

// Evaluate scores one model version against a golden dataset and writes the report the
// promotion gates read. Scoring is deterministic: the same model and dataset always agree.
func Evaluate(cfg EvalConfig, log *slog.Logger) error {
	cardBytes, err := os.ReadFile(filepath.Join(cfg.ArtifactURI, "model.json"))
	if err != nil {
		return fmt.Errorf("model artifact unreadable: %w", err)
	}
	var card Card
	if err := json.Unmarshal(cardBytes, &card); err != nil {
		return fmt.Errorf("model artifact unparsable: %w", err)
	}

	rows, err := readDataset(cfg.DatasetURI)
	if err != nil {
		return fmt.Errorf("data read error: %w", err)
	}
	if len(rows) == 0 {
		return fmt.Errorf("golden dataset %s has no rows", cfg.DatasetRef)
	}

	var correct int
	var latencies []float64
	var failures []string
	for _, id := range rows {
		score := float64(hash32(cfg.ModelRef+"/"+id)%1000) / 1000
		if score < card.Quality {
			correct++
		} else if len(failures) < 5 {
			failures = append(failures, id)
		}
		latencies = append(latencies, card.BaseLatencyMs*(1.0+float64(hash32(id)%60)/100))
	}
	sort.Float64s(latencies)

	report := Report{
		Accuracy:     round(float64(correct)/float64(len(rows)), 4),
		P95LatencyMs: round(latencies[p95Index(len(latencies))], 2),
		CostPerKReq:  round(card.BaseLatencyMs*0.12, 3),
		Samples:      len(rows),
		ModelRef:     cfg.ModelRef,
		DatasetRef:   cfg.DatasetRef,
		Failures:     failures,
		RanAt:        time.Now().UTC(),
	}
	out := filepath.Join(cfg.ArtifactDir, "evals", cfg.EvalName, "result.json")
	if err := writeJSON(out, report); err != nil {
		return fmt.Errorf("report write error: %w", err)
	}
	log.Info("evaluation finished", "modelRef", cfg.ModelRef, "accuracy", report.Accuracy,
		"p95LatencyMs", report.P95LatencyMs, "costPerKReq", report.CostPerKReq, "report", out)
	return nil
}

// readDataset returns the row ids of a jsonl dataset.
func readDataset(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var ids []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		var row struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(sc.Bytes(), &row); err != nil {
			continue
		}
		if row.ID != "" {
			ids = append(ids, row.ID)
		}
	}
	return ids, sc.Err()
}

func p95Index(n int) int {
	i := int(float64(n) * 0.95)
	if i >= n {
		i = n - 1
	}
	return i
}

func hash32(s string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return h.Sum32()
}
