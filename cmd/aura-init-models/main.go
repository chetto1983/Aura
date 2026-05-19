// Command aura-init-models is the one-shot init container that ensures all
// large model artifacts (embedding GGUFs today, OCR weights tomorrow) are
// present at the canonical paths before the dependent sidecars start.
//
// The container is intentionally tiny (distroless static base, ~10 MiB).
// It mounts ./data into /models and either:
//   - exits 0 immediately if every required file is already present with a
//     verified SHA-256 (warm cache, ~1 s for a 265 MB scan), or
//   - downloads each missing/corrupt file from its pinned upstream URL,
//     verifies, atomically renames into place, and exits 0.
//
// On unrecoverable error (SHA mismatch from upstream, max retries exhausted)
// the process exits 1. The Compose orchestrator's `condition:
// service_completed_successfully` keeps aura-llama-embed from starting until
// this binary returns 0, eliminating the cold-start race where llama-embed
// crashes because the model file isn't on disk yet.
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/aura/aura/internal/install"
)

const defaultDataDir = "/models"

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))

	dataDir := envOrDefault("AURA_MODELS_DIR", defaultDataDir)
	logger.Info("aura-init-models starting", "data_dir", dataDir)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	embStart := time.Now()
	embProgress := install.LogProgressFn(logger.With("model", install.EmbeddingModelFilename), 5*time.Second)
	if err := install.EnsureEmbeddingModel(ctx, dataDir, embProgress); err != nil {
		logger.Error("embedding model fetch failed",
			"data_dir", dataDir,
			"url", install.EmbeddingModelURL,
			"sha256", install.EmbeddingModelSHA256,
			"err", err.Error(),
		)
		os.Exit(1)
	}
	logger.Info("embedding model ready",
		"filename", install.EmbeddingModelFilename,
		"elapsed_sec", int(time.Since(embStart).Seconds()),
	)

	whisperStart := time.Now()
	whisperProgress := install.LogProgressFn(logger.With("model", install.WhisperModelFilename), 5*time.Second)
	if err := install.EnsureWhisperModel(ctx, dataDir, whisperProgress); err != nil {
		logger.Error("whisper model fetch failed",
			"data_dir", dataDir,
			"url", install.WhisperModelURL,
			"sha256", install.WhisperModelSHA256,
			"err", err.Error(),
		)
		os.Exit(1)
	}
	logger.Info("whisper model ready",
		"filename", install.WhisperModelFilename,
		"elapsed_sec", int(time.Since(whisperStart).Seconds()),
	)

	piperStart := time.Now()
	piperProgress := install.LogProgressFn(logger.With("model", install.PiperVoiceFilename), 5*time.Second)
	if err := install.EnsurePiperVoice(ctx, dataDir, piperProgress); err != nil {
		logger.Error("piper voice fetch failed",
			"data_dir", dataDir,
			"voice_url", install.PiperVoiceURL,
			"voice_sha256", install.PiperVoiceSHA256,
			"cfg_url", install.PiperVoiceConfigURL,
			"cfg_sha256", install.PiperVoiceConfigSHA256,
			"err", err.Error(),
		)
		os.Exit(1)
	}
	logger.Info("piper voice ready",
		"filename", install.PiperVoiceFilename,
		"elapsed_sec", int(time.Since(piperStart).Seconds()),
	)
	os.Exit(0)
}

func envOrDefault(key, fallback string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return fallback
	}
	return v
}
