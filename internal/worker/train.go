// Package worker holds the job entrypoints the controller schedules: training and evaluation.
// The compute is simulated, but the operational behaviour that matters -- checkpointing, resume,
// structured failure, and a written artifact -- is real.
package worker

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// TrainConfig is read from the environment the controller stamps on the Job.
type TrainConfig struct {
	JobName         string
	ModelName       string
	ModelVersion    string
	BaseModel       string
	DatasetRef      string
	DatasetURI      string
	Steps           int
	CheckpointEvery int
	ArtifactDir     string
	StepDuration    time.Duration
	// FailAtStep injects a mid-run failure, which is how the resume path gets exercised.
	FailAtStep int
}

// TrainConfigFromEnv builds the config a training container runs with.
func TrainConfigFromEnv() TrainConfig {
	return TrainConfig{
		JobName:         os.Getenv("JOB_NAME"),
		ModelName:       os.Getenv("MODEL_NAME"),
		ModelVersion:    os.Getenv("MODEL_VERSION"),
		BaseModel:       os.Getenv("BASE_MODEL"),
		DatasetRef:      os.Getenv("DATASET_REF"),
		DatasetURI:      os.Getenv("DATASET_URI"),
		Steps:           envInt("STEPS", 20),
		CheckpointEvery: envInt("CHECKPOINT_EVERY", 5),
		ArtifactDir:     envOr("ARTIFACT_DIR", "/artifacts"),
		StepDuration:    time.Duration(envInt("STEP_MS", 200)) * time.Millisecond,
		FailAtStep:      envInt("FAIL_AT_STEP", 0),
	}
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func envInt(k string, def int) int {
	if v := os.Getenv(k); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

// Checkpoint is what a restarted attempt reads to avoid repeating finished work.
type Checkpoint struct {
	Step      int       `json:"step"`
	Loss      float64   `json:"loss"`
	Accuracy  float64   `json:"accuracy"`
	WrittenAt time.Time `json:"writtenAt"`
}

// Card is the model artifact a successful run leaves behind.
type Card struct {
	ModelRef      string    `json:"modelRef"`
	BaseModel     string    `json:"baseModel"`
	DatasetRef    string    `json:"datasetRef"`
	SourceJob     string    `json:"sourceJob"`
	Steps         int       `json:"steps"`
	TrainAccuracy float64   `json:"trainAccuracy"`
	Quality       float64   `json:"quality"`
	BaseLatencyMs float64   `json:"baseLatencyMs"`
	FinishedAt    time.Time `json:"finishedAt"`
}

// Train runs one training attempt, resuming from the newest checkpoint if there is one.
func Train(cfg TrainConfig, log *slog.Logger) error {
	if cfg.ModelName == "" || cfg.ModelVersion == "" {
		return fmt.Errorf("MODEL_NAME and MODEL_VERSION are required")
	}
	if _, err := os.Stat(cfg.DatasetURI); cfg.DatasetURI != "" && err != nil {
		return fmt.Errorf("data read error: %w", err)
	}

	outDir := filepath.Join(cfg.ArtifactDir, "models", cfg.ModelName, cfg.ModelVersion)
	ckptDir := filepath.Join(outDir, "checkpoints")
	if err := os.MkdirAll(ckptDir, 0o755); err != nil {
		return fmt.Errorf("checkpoint write error: %w", err)
	}

	start := 0
	if ck, err := latestCheckpoint(ckptDir); err == nil {
		start = ck.Step
		log.Info("resuming from checkpoint", "step", start, "loss", ck.Loss)
	}

	// Quality is deterministic per model version, so the same version always evaluates the same.
	target := quality(cfg.ModelName + ":" + cfg.ModelVersion)
	var acc, loss float64
	for step := start + 1; step <= cfg.Steps; step++ {
		time.Sleep(cfg.StepDuration)
		progress := float64(step) / float64(cfg.Steps)
		acc = target * (0.55 + 0.45*progress)
		loss = 2.0 * (1 - progress) * (1.2 - target)
		if cfg.FailAtStep > 0 && step == cfg.FailAtStep {
			return fmt.Errorf("user code error: injected failure at step %d", step)
		}
		if cfg.CheckpointEvery > 0 && step%cfg.CheckpointEvery == 0 {
			ck := Checkpoint{Step: step, Loss: round(loss, 4), Accuracy: round(acc, 4), WrittenAt: time.Now().UTC()}
			if err := writeJSON(filepath.Join(ckptDir, fmt.Sprintf("step-%04d.json", step)), ck); err != nil {
				return fmt.Errorf("checkpoint write error: %w", err)
			}
			log.Info("checkpoint written", "step", step, "loss", ck.Loss, "accuracy", ck.Accuracy)
		}
		log.Info("step", "step", step, "of", cfg.Steps, "loss", round(loss, 4), "accuracy", round(acc, 4))
	}

	card := Card{
		ModelRef:      cfg.ModelName + ":" + cfg.ModelVersion,
		BaseModel:     cfg.BaseModel,
		DatasetRef:    cfg.DatasetRef,
		SourceJob:     cfg.JobName,
		Steps:         cfg.Steps,
		TrainAccuracy: round(acc, 4),
		Quality:       round(target, 4),
		BaseLatencyMs: round(30-20*target, 2),
		FinishedAt:    time.Now().UTC(),
	}
	if err := writeJSON(filepath.Join(outDir, "model.json"), card); err != nil {
		return fmt.Errorf("artifact write error: %w", err)
	}
	log.Info("training finished", "modelRef", card.ModelRef, "trainAccuracy", card.TrainAccuracy)
	return nil
}

// latestCheckpoint returns the newest committed checkpoint, ignoring partial writes.
func latestCheckpoint(dir string) (Checkpoint, error) {
	var best Checkpoint
	entries, err := os.ReadDir(dir)
	if err != nil {
		return best, err
	}
	found := false
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		var ck Checkpoint
		if err := json.Unmarshal(b, &ck); err != nil {
			continue // a torn checkpoint is skipped, not fatal
		}
		if ck.Step > best.Step {
			best, found = ck, true
		}
	}
	if !found {
		return best, os.ErrNotExist
	}
	return best, nil
}

// quality derives a stable pseudo-random skill level in [0.62, 0.95] for a model version.
func quality(ref string) float64 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(ref))
	return 0.62 + float64(h.Sum32()%34)/100
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func round(v float64, places int) float64 {
	p := 1.0
	for i := 0; i < places; i++ {
		p *= 10
	}
	return float64(int(v*p+0.5)) / p
}
