package worker

import (
	"encoding/json"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func quietLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func trainConfig(t *testing.T, dir string) TrainConfig {
	t.Helper()
	return TrainConfig{
		JobName: "ft-1", ModelName: "support", ModelVersion: "v1",
		DatasetRef: "docs:v1", Steps: 10, CheckpointEvery: 2,
		ArtifactDir: dir, StepDuration: 0,
	}
}

func TestTrainWritesCheckpointsAndArtifact(t *testing.T) {
	dir := t.TempDir()
	if err := Train(trainConfig(t, dir), quietLog()); err != nil {
		t.Fatalf("Train: %v", err)
	}
	ckpts, err := os.ReadDir(filepath.Join(dir, "models", "support", "v1", "checkpoints"))
	if err != nil {
		t.Fatalf("read checkpoints: %v", err)
	}
	if len(ckpts) != 5 {
		t.Fatalf("got %d checkpoints, want 5 for 10 steps every 2", len(ckpts))
	}
	var card Card
	b, err := os.ReadFile(filepath.Join(dir, "models", "support", "v1", "model.json"))
	if err != nil {
		t.Fatalf("read model.json: %v", err)
	}
	if err := json.Unmarshal(b, &card); err != nil {
		t.Fatalf("parse model.json: %v", err)
	}
	if card.ModelRef != "support:v1" || card.TrainAccuracy <= 0 {
		t.Fatalf("card = %+v, want a usable artifact", card)
	}
}

// A failed attempt must leave the platform able to resume rather than restart from step zero.
func TestTrainResumesFromLatestCheckpoint(t *testing.T) {
	dir := t.TempDir()
	cfg := trainConfig(t, dir)
	cfg.FailAtStep = 7
	if err := Train(cfg, quietLog()); err == nil {
		t.Fatal("Train should fail at the injected step")
	}
	ck, err := latestCheckpoint(filepath.Join(dir, "models", "support", "v1", "checkpoints"))
	if err != nil {
		t.Fatalf("latestCheckpoint: %v", err)
	}
	if ck.Step != 6 {
		t.Fatalf("checkpoint step = %d, want 6", ck.Step)
	}

	cfg.FailAtStep = 0
	if err := Train(cfg, quietLog()); err != nil {
		t.Fatalf("resumed Train: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "models", "support", "v1", "model.json")); err != nil {
		t.Fatalf("resumed run produced no artifact: %v", err)
	}
}

func TestTrainFailsWithStructuredReasonOnMissingData(t *testing.T) {
	cfg := trainConfig(t, t.TempDir())
	cfg.DatasetURI = filepath.Join(t.TempDir(), "gone.jsonl")
	err := Train(cfg, quietLog())
	if err == nil || !strings.Contains(err.Error(), "data read error") {
		t.Fatalf("err = %v, want a structured data read error", err)
	}
}

func TestTrainQualityIsDeterministicPerVersion(t *testing.T) {
	if quality("support:v1") != quality("support:v1") {
		t.Fatal("the same model version must always train to the same quality")
	}
	if quality("support:v1") == quality("support:v2") {
		t.Fatal("different versions should differ, otherwise evaluation proves nothing")
	}
}

func TestEvaluateWritesReport(t *testing.T) {
	dir := t.TempDir()
	if err := Train(trainConfig(t, dir), quietLog()); err != nil {
		t.Fatalf("Train: %v", err)
	}
	goldenPath := filepath.Join(dir, "datasets", "golden.jsonl")
	if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var rows strings.Builder
	for i := 0; i < 50; i++ {
		rows.WriteString(`{"id":"row-` + string(rune('a'+i%26)) + string(rune('a'+i/26)) + `"}` + "\n")
	}
	if err := os.WriteFile(goldenPath, []byte(rows.String()), 0o644); err != nil {
		t.Fatalf("write golden: %v", err)
	}

	cfg := EvalConfig{
		EvalName: "eval-1", ModelRef: "support:v1",
		ArtifactURI: filepath.Join(dir, "models", "support", "v1"),
		DatasetRef:  "golden:v1", DatasetURI: goldenPath, ArtifactDir: dir,
	}
	if err := Evaluate(cfg, quietLog()); err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "evals", "eval-1", "result.json"))
	if err != nil {
		t.Fatalf("read report: %v", err)
	}
	var rep Report
	if err := json.Unmarshal(b, &rep); err != nil {
		t.Fatalf("parse report: %v", err)
	}
	if rep.Samples != 50 || rep.Accuracy <= 0 || rep.P95LatencyMs <= 0 {
		t.Fatalf("report = %+v, want populated quality, latency and sample count", rep)
	}
}

func TestEvaluateFailsWithoutArtifact(t *testing.T) {
	cfg := EvalConfig{EvalName: "eval-1", ModelRef: "support:v1", ArtifactURI: t.TempDir(), ArtifactDir: t.TempDir()}
	if err := Evaluate(cfg, quietLog()); err == nil {
		t.Fatal("Evaluate should fail when the model artifact is missing")
	}
}
