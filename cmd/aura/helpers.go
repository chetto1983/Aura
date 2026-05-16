package main

import (
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/storage/freshness"
	"github.com/aura/aura/internal/storage/search"
)

func shouldBootstrapPromptOverlayDefaults(cfg *config.Config) bool {
	if cfg == nil {
		return false
	}
	overlay := strings.TrimSpace(cfg.PromptOverlayPath)
	if overlay == "" || overlay == "." {
		return false
	}
	overlayClean := filepath.Clean(overlay)
	workspaceClean := filepath.Clean(strings.TrimSpace(cfg.WorkspaceRoot))
	runtimeClean := filepath.Clean(strings.TrimSpace(cfg.RuntimeWorkspacePath))
	return overlayClean == workspaceClean || overlayClean == runtimeClean || overlayClean == filepath.Clean("/workspace")
}

func skillSearchRoots(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	skillsPath := strings.TrimSpace(cfg.SkillsPath)
	if skillsPath == "" {
		skillsPath = "./skills"
	}
	installRoot := strings.TrimSpace(cfg.SkillsInstallProjectDir)
	if installRoot == "" {
		installRoot = "."
	}
	return []string{
		skillsPath,
		".agents/skills",
		".claude/skills",
		filepath.Join(skillsPath, ".agents", "skills"),
		filepath.Join(skillsPath, ".claude", "skills"),
		filepath.Join(installRoot, ".agents", "skills"),
		filepath.Join(installRoot, ".claude", "skills"),
	}
}

func createLLMClient(cfg *config.Config, logger *slog.Logger) llm.Client {
	_ = logger
	openaiClient := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  cfg.LLMAPIKey,
		BaseURL: cfg.LLMBaseURL,
		Model:   cfg.LLMModel,
	})
	return llm.NewRetryClient(openaiClient, llm.RetryConfig{
		MaxRetries:          cfg.LLMMaxRetries,
		BaseDelay:           time.Second,
		MaxDelay:            30 * time.Second,
		MaxContentRetries:   3,
		ContentTemperatures: []float64{0.0, 0.3, 0.7},
		JitterRatio:         0.5,
	})
}

func createEmbeddingFunc(cfg *config.Config) search.EmbeddingFunc {
	return search.NewOpenAICompatEmbeddingFunction(cfg.EmbeddingBaseURL, cfg.EmbeddingAPIKey, cfg.EmbeddingModel, cfg.EmbeddingOutputDim, true, nil)
}

func setupSandboxRuntime(cfg *config.Config, logger *slog.Logger) (*sandbox.Manager, api.SandboxHealth) {
	if logger == nil {
		logger = slog.Default()
	}
	health := api.SandboxHealth{
		Enabled:     cfg.SandboxEnabled,
		RuntimeKind: string(sandbox.RuntimeKindUnavailable),
		Detail:      "sandbox disabled",
	}
	if !cfg.SandboxEnabled {
		return nil, health
	}

	timeout := time.Duration(cfg.SandboxTimeoutSec) * time.Second
	runner, err := sandbox.NewProcessRunner(sandbox.ProcessRunnerConfig{
		WorkDir: cfg.WorkspaceRoot,
		Timeout: timeout,
	})
	if err != nil {
		health.Detail = err.Error()
		logger.Warn("sandbox process runner configuration invalid, execute_code disabled",
			"runtime_kind", sandbox.RuntimeKindProcess,
			"workdir", cfg.WorkspaceRoot,
			"detail", health.Detail)
		return nil, health
	}
	availability := runner.CheckAvailability()
	health.Runtime = cfg.WorkspaceRoot
	health.Available = availability.Available
	health.RuntimeKind = string(availability.Kind)
	health.Detail = availability.Detail
	if !availability.Available {
		logger.Warn("sandbox process runtime unavailable, execute_code disabled",
			"runtime_kind", health.RuntimeKind,
			"workdir", cfg.WorkspaceRoot,
			"detail", availability.Detail)
		return nil, health
	}
	manager, err := sandbox.NewManager(sandbox.Config{
		Runtime: runner,
		Timeout: timeout,
	})
	if err != nil {
		health.Available = false
		health.Detail = err.Error()
		logger.Warn("sandbox process manager unavailable, execute_code disabled",
			"runtime_kind", health.RuntimeKind,
			"workdir", cfg.WorkspaceRoot,
			"detail", health.Detail)
		return nil, health
	}
	logger.Info("sandbox process runtime available, execute_code enabled",
		"runtime_kind", health.RuntimeKind,
		"workdir", cfg.WorkspaceRoot,
		"detail", health.Detail)
	return manager, health
}

// seedProjectionState inserts the 5 canonical projection_state rows if they do
// not already exist. Idempotent: rows that already exist are left untouched.
func seedProjectionState(ctx context.Context, fs *freshness.Store, embeddingModelID string, embeddingDim int) error {
	type rowSpec struct {
		id    string
		kind  string
		model string
		dim   int
	}
	qdrantModel := embeddingModelID
	qdrantDim := embeddingDim
	specs := []rowSpec{
		{"wiki_documents_fts5", "fts5", "", 0},
		{"aura_memory_v1", "qdrant", qdrantModel, qdrantDim},
		{"compact_memory_documents", "sqlite", "", 0},
		{"aura_memory_v1_compact", "qdrant", qdrantModel, qdrantDim},
		{"embedding_cache", "cache", "", 0},
	}
	buildID := uuid.NewString()
	now := time.Now().Unix()
	for _, spec := range specs {
		_, found, err := fs.Get(ctx, spec.id)
		if err != nil {
			return err
		}
		if found {
			continue
		}
		row := freshness.Row{
			ProjectionID:      spec.id,
			Kind:              spec.kind,
			EmbeddingModelID:  spec.model,
			EmbeddingDim:      spec.dim,
			IndexBuildID:      buildID,
			SchemaVersion:     1,
			LastFullRebuildAt: now,
			Status:            "fresh",
			Version:           1,
		}
		if _, err := fs.Upsert(ctx, row); err != nil {
			return err
		}
	}
	return nil
}
