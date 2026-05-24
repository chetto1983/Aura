package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/aura/aura/internal/api"
	"github.com/aura/aura/internal/config"
	"github.com/aura/aura/internal/ctxmetrics"
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

const modelContextWindowHardDefault = 128000

// populateModelContextWindow fills cfg.ModelContextWindow via the resolution chain:
//  1. env override (AURA_MODEL_CONTEXT_WINDOW > 0) — already loaded by config.Load(); skip fetch.
//  2. fetch from provider /models endpoint.
//  3. curated fallback table (config.ContextWindowFallback).
//  4. hard default (128000).
//
// Non-blocking: a slow or unreachable provider causes fallback to (3)/(4), never
// a startup failure.
func populateModelContextWindow(ctx context.Context, cfg *config.Config, logger *slog.Logger) {
	if cfg.ModelContextWindow > 0 {
		logger.Info("model context window: using env override", "window", cfg.ModelContextWindow)
		return
	}
	if cfg.LLMAPIKey == "" {
		applyModelContextWindowFallback(cfg, logger, nil)
		return
	}
	rawClient := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  cfg.LLMAPIKey,
		BaseURL: cfg.LLMBaseURL,
		Model:   cfg.LLMModel,
	})
	fetchCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	meta, err := rawClient.FetchModelMetadata(fetchCtx, cfg.LLMModel)
	if err == nil && meta.ContextLength > 0 {
		cfg.ModelContextWindow = meta.ContextLength
		logger.Info("model context window: detected from provider",
			"model", cfg.LLMModel,
			"context_length", meta.ContextLength,
			"max_completion_tokens", meta.MaxCompletionTokens)
		return
	}
	applyModelContextWindowFallback(cfg, logger, err)
}

// populateMaxConversationTokens computes cfg.MaxConversationTokens from the
// resolved ModelContextWindow and CTXCompactPercent. Call this immediately after
// populateModelContextWindow.
//
// Backward-compat shim: if MAX_HISTORY_MESSAGES was set to a non-default value
// (default = 50), a deprecation warning is logged and the legacy message count
// is approximated at 500 tokens/message.
func populateMaxConversationTokens(cfg *config.Config, logger *slog.Logger) {
	const legacyDefault = 50
	ctxmetrics.GlobalGauges.ModelContextWindow = cfg.ModelContextWindow
	ctxmetrics.GlobalGauges.CompactionThresholdTokens = 0

	if cfg.MaxHistoryMessages != legacyDefault {
		approx := cfg.MaxHistoryMessages * 500
		cfg.MaxConversationTokens = approx
		ctxmetrics.GlobalGauges.CompactionThresholdTokens = cfg.MaxConversationTokens
		logger.Warn("MAX_HISTORY_MESSAGES is deprecated; set AURA_CTX_COMPACT_PERCENT to control token-budget compaction",
			"max_history_messages", cfg.MaxHistoryMessages,
			"approx_threshold_tokens", approx)
		return
	}
	if cfg.CTXCompactPercent <= 0 || cfg.ModelContextWindow <= 0 {
		return
	}
	cfg.MaxConversationTokens = int(float64(cfg.ModelContextWindow) * cfg.CTXCompactPercent)
	logger.Info("context compaction threshold computed",
		"model_context_window", cfg.ModelContextWindow,
		"compact_percent", cfg.CTXCompactPercent,
		"threshold_tokens", cfg.MaxConversationTokens,
		"scope", cfg.CTXCompactScope)
	ctxmetrics.GlobalGauges.CompactionThresholdTokens = cfg.MaxConversationTokens
}

func applyModelContextWindowFallback(cfg *config.Config, logger *slog.Logger, fetchErr error) {
	if f := config.ContextWindowFallback(cfg.LLMModel); f > 0 {
		cfg.ModelContextWindow = f
		logger.Warn("model context window: using curated fallback",
			"model", cfg.LLMModel, "window", f, "fetch_error", fetchErr)
		return
	}
	cfg.ModelContextWindow = modelContextWindowHardDefault
	logger.Warn("model context window: using hard default",
		"model", cfg.LLMModel, "window", modelContextWindowHardDefault, "fetch_error", fetchErr)
}

func createLLMClient(cfg *config.Config, logger *slog.Logger) llm.Client {
	_ = logger
	// Prompt cache is always-on: DetectCacheSupport (cache.go) is the only
	// gate — it heuristically activates on openrouter.ai/anthropic.com or
	// claude/deepseek model names, and silently no-ops on every other
	// provider, so passing true here has zero effect when unsupported.
	openaiClient := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:       cfg.LLMAPIKey,
		BaseURL:      cfg.LLMBaseURL,
		Model:        cfg.LLMModel,
		CacheEnabled: true,
	})
	// All client.Chat()/Stream() calls inherit retry via this wrap.
	// Config: MaxRetries=cfg.LLMMaxRetries (default 5), BaseDelay=1s, MaxDelay=30s.
	// Telegram streaming (streamingChatClient.llmc.Stream) and web-chat
	// (noStreamClient.Send) both resolve through this single construction site.
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

// checkEmbedSidecarNCtx hits {embeddingBaseURL}/props (stripping /v1 suffix)
// and asserts the llama.cpp effective per-slot n_ctx >= minNCtx.
//
// Returns nil when the endpoint is unreachable or the response is not a
// recognisable llama.cpp /props payload (e.g. OpenAI or another provider).
// Returns an error only when the endpoint responds with a parseable n_ctx below
// minNCtx — a clear misconfiguration that will cause long inputs to be rejected
// with HTTP 400. Callers should treat that as a fatal boot failure.
func checkEmbedSidecarNCtx(ctx context.Context, embeddingBaseURL string, minNCtx int, logger *slog.Logger) error {
	base := strings.TrimRight(strings.TrimSpace(embeddingBaseURL), "/")
	base = strings.TrimSuffix(base, "/v1")
	if base == "" {
		return nil
	}
	propsURL := base + "/props"

	reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, propsURL, nil)
	if err != nil {
		return nil
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		logger.Debug("embed sidecar /props unreachable, skipping n_ctx check", "url", propsURL)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Debug("embed sidecar /props non-200, skipping n_ctx check", "status", resp.StatusCode, "url", propsURL)
		return nil
	}

	var propsResp struct {
		DefaultGenerationSettings struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 8192))
	if err != nil || json.Unmarshal(data, &propsResp) != nil || propsResp.DefaultGenerationSettings.NCtx == 0 {
		return nil // Not a llama.cpp /props response; skip.
	}

	nCtx := propsResp.DefaultGenerationSettings.NCtx
	if nCtx < minNCtx {
		return fmt.Errorf("embed sidecar effective n_ctx=%d < %d; per-slot n_ctx = --ctx-size / --parallel — bump compose.yaml --ctx-size so the quotient is >= %d", nCtx, minNCtx, minNCtx)
	}
	logger.Info("embed sidecar n_ctx check passed", "url", propsURL, "n_ctx", nCtx)
	return nil
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
