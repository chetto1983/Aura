package settings

import (
	"context"
	"slices"
	"strings"

	"github.com/aura/aura/internal/config"
)

// Bootstrap fields are never overridden by the DB. They're either needed
// before the DB can be opened (TelegramToken, HTTPPort, DBPath, LogLevel)
// or hold path roots that the running process has already opened
// (WikiPath, SkillsPath, MCPServersPath, PromptOverlayPath). Edits to
// these go to .env and take effect on next restart.
//
// Everything else is fair game for runtime override via this applier.
const (
	KeyAllowlist                 = "TELEGRAM_ALLOWLIST"
	KeyMaxContextTokens          = "MAX_CONTEXT_TOKENS"
	KeyMaxHistoryMessages        = "MAX_HISTORY_MESSAGES"
	KeySoftBudget                = "SOFT_BUDGET"
	KeyHardBudget                = "HARD_BUDGET"
	KeyCostPerToken              = "COST_PER_TOKEN"
	KeyLLMAPIKey                 = "LLM_API_KEY"
	KeyLLMBaseURL                = "LLM_BASE_URL"
	KeyLLMModel                  = "LLM_MODEL"
	KeyLLMMaxRetries             = "LLM_MAX_RETRIES"
	KeyOllamaBaseURL             = "OLLAMA_BASE_URL"
	KeyOllamaModel               = "OLLAMA_MODEL"
	KeyOllamaAPIKey              = "OLLAMA_API_KEY"
	KeyOllamaWebBaseURL          = "OLLAMA_WEB_BASE_URL"
	KeyMaxToolIterations         = "MAX_TOOL_ITERATIONS"
	KeySkillsCatalogURL          = "SKILLS_CATALOG_URL"
	KeySkillsAdmin               = "SKILLS_ADMIN"
	KeyAuraBotEnabled            = "AURABOT_ENABLED"
	KeyAuraBotMaxActive          = "AURABOT_MAX_ACTIVE"
	KeyAuraBotMaxDepth           = "AURABOT_MAX_DEPTH"
	KeyAuraBotTimeoutSec         = "AURABOT_TIMEOUT_SEC"
	KeyAuraBotMaxIterations      = "AURABOT_MAX_ITERATIONS"
	KeyEmbeddingAPIKey           = "EMBEDDING_API_KEY"
	KeyEmbeddingBaseURL          = "EMBEDDING_BASE_URL"
	KeyEmbeddingModel            = "EMBEDDING_MODEL"
	KeyOTelEnabled               = "OTEL_ENABLED"
	KeyMistralAPIKey             = "MISTRAL_API_KEY"
	KeyMistralOCRModel           = "MISTRAL_OCR_MODEL"
	KeyMistralOCRBaseURL         = "MISTRAL_OCR_BASE_URL"
	KeyMistralOCRTableFormat     = "MISTRAL_OCR_TABLE_FORMAT"
	KeyMistralOCRIncludeImages   = "MISTRAL_OCR_INCLUDE_IMAGES"
	KeyMistralOCRExtractHeader   = "MISTRAL_OCR_EXTRACT_HEADER"
	KeyMistralOCRExtractFooter   = "MISTRAL_OCR_EXTRACT_FOOTER"
	KeyOCREnabled                = "OCR_ENABLED"
	KeyOCRMaxPages               = "OCR_MAX_PAGES"
	KeyOCRMaxFileMB              = "OCR_MAX_FILE_MB"
	KeyConvArchiveEnabled        = "CONV_ARCHIVE_ENABLED"
	KeySummarizerEnabled         = "SUMMARIZER_ENABLED"
	KeySummarizerMode            = "SUMMARIZER_MODE"
	KeySummarizerTurnInterval    = "SUMMARIZER_TURN_INTERVAL"
	KeySummarizerMinSalience     = "SUMMARIZER_MIN_SALIENCE"
	KeySummarizerLookbackTurns   = "SUMMARIZER_LOOKBACK_TURNS"
	KeySummarizerCooldownSeconds = "SUMMARIZER_COOLDOWN_SECONDS"
)

// OverridableKeys returns every key the applier touches. Callers (e.g. the
// settings UI handler) use this to validate that an inbound write
// targets a real config field instead of stuffing arbitrary KV pairs.
func OverridableKeys() []string {
	return []string{
		KeyAllowlist,
		KeyMaxContextTokens, KeyMaxHistoryMessages,
		KeySoftBudget, KeyHardBudget, KeyCostPerToken,
		KeyLLMAPIKey, KeyLLMBaseURL, KeyLLMModel, KeyLLMMaxRetries,
		KeyOllamaBaseURL, KeyOllamaModel, KeyOllamaAPIKey, KeyOllamaWebBaseURL,
		KeyMaxToolIterations,
		KeySkillsCatalogURL, KeySkillsAdmin,
		KeyAuraBotEnabled, KeyAuraBotMaxActive, KeyAuraBotMaxDepth,
		KeyAuraBotTimeoutSec, KeyAuraBotMaxIterations,
		KeyEmbeddingAPIKey, KeyEmbeddingBaseURL, KeyEmbeddingModel,
		KeyOTelEnabled,
		KeyMistralAPIKey, KeyMistralOCRModel, KeyMistralOCRBaseURL,
		KeyMistralOCRTableFormat, KeyMistralOCRIncludeImages,
		KeyMistralOCRExtractHeader, KeyMistralOCRExtractFooter,
		KeyOCREnabled, KeyOCRMaxPages, KeyOCRMaxFileMB,
		KeyConvArchiveEnabled,
		KeySummarizerEnabled, KeySummarizerMode, KeySummarizerTurnInterval,
		KeySummarizerMinSalience, KeySummarizerLookbackTurns, KeySummarizerCooldownSeconds,
	}
}

// IsOverridable reports whether the dashboard is allowed to set the key.
func IsOverridable(key string) bool {
	return slices.Contains(OverridableKeys(), key)
}

// ApplyToConfig overlays any settings rows onto cfg. Each field is
// overwritten only when the corresponding row exists and parses; an
// unset / unparseable row leaves the env-loaded value untouched.
//
// Empty store (no rows) is a no-op, so wiring this in produces zero
// behavior change until the dashboard starts writing settings.
func ApplyToConfig(ctx context.Context, s *Store, cfg *config.Config) {
	if s == nil || cfg == nil {
		return
	}

	if v, err := s.Get(ctx, KeyAllowlist); err == nil {
		// Re-use config's allowlist parser so semantics match Load.
		cfg.Allowlist = parseAllowlist(v)
		cfg.AllowlistConfigured = len(cfg.Allowlist) > 0
	}

	cfg.MaxContextTokens = s.GetInt(ctx, KeyMaxContextTokens, cfg.MaxContextTokens)
	cfg.MaxHistoryMessages = s.GetInt(ctx, KeyMaxHistoryMessages, cfg.MaxHistoryMessages)
	cfg.SoftBudget = s.GetFloat(ctx, KeySoftBudget, cfg.SoftBudget)
	cfg.HardBudget = s.GetFloat(ctx, KeyHardBudget, cfg.HardBudget)
	cfg.CostPerToken = s.GetFloat(ctx, KeyCostPerToken, cfg.CostPerToken)

	cfg.LLMAPIKey = s.GetString(ctx, KeyLLMAPIKey, cfg.LLMAPIKey)
	cfg.LLMBaseURL = s.GetString(ctx, KeyLLMBaseURL, cfg.LLMBaseURL)
	cfg.LLMModel = s.GetString(ctx, KeyLLMModel, cfg.LLMModel)
	cfg.LLMMaxRetries = s.GetInt(ctx, KeyLLMMaxRetries, cfg.LLMMaxRetries)

	cfg.OllamaBaseURL = s.GetString(ctx, KeyOllamaBaseURL, cfg.OllamaBaseURL)
	cfg.OllamaModel = s.GetString(ctx, KeyOllamaModel, cfg.OllamaModel)
	cfg.OllamaAPIKey = s.GetString(ctx, KeyOllamaAPIKey, cfg.OllamaAPIKey)
	cfg.OllamaWebBaseURL = s.GetString(ctx, KeyOllamaWebBaseURL, cfg.OllamaWebBaseURL)
	cfg.MaxToolIterations = s.GetInt(ctx, KeyMaxToolIterations, cfg.MaxToolIterations)

	cfg.SkillsCatalogURL = s.GetString(ctx, KeySkillsCatalogURL, cfg.SkillsCatalogURL)
	cfg.SkillsAdmin = s.GetBool(ctx, KeySkillsAdmin, cfg.SkillsAdmin)
	cfg.AuraBotEnabled = s.GetBool(ctx, KeyAuraBotEnabled, cfg.AuraBotEnabled)
	cfg.AuraBotMaxActive = s.GetInt(ctx, KeyAuraBotMaxActive, cfg.AuraBotMaxActive)
	cfg.AuraBotMaxDepth = s.GetInt(ctx, KeyAuraBotMaxDepth, cfg.AuraBotMaxDepth)
	cfg.AuraBotTimeoutSec = s.GetInt(ctx, KeyAuraBotTimeoutSec, cfg.AuraBotTimeoutSec)
	cfg.AuraBotMaxIterations = s.GetInt(ctx, KeyAuraBotMaxIterations, cfg.AuraBotMaxIterations)

	cfg.EmbeddingAPIKey = s.GetString(ctx, KeyEmbeddingAPIKey, cfg.EmbeddingAPIKey)
	cfg.EmbeddingBaseURL = s.GetString(ctx, KeyEmbeddingBaseURL, cfg.EmbeddingBaseURL)
	cfg.EmbeddingModel = s.GetString(ctx, KeyEmbeddingModel, cfg.EmbeddingModel)
	cfg.OTelEnabled = s.GetBool(ctx, KeyOTelEnabled, cfg.OTelEnabled)

	cfg.MistralAPIKey = s.GetString(ctx, KeyMistralAPIKey, cfg.MistralAPIKey)
	cfg.MistralOCRModel = s.GetString(ctx, KeyMistralOCRModel, cfg.MistralOCRModel)
	cfg.MistralOCRBaseURL = s.GetString(ctx, KeyMistralOCRBaseURL, cfg.MistralOCRBaseURL)
	cfg.MistralOCRTableFormat = s.GetString(ctx, KeyMistralOCRTableFormat, cfg.MistralOCRTableFormat)
	cfg.MistralOCRIncludeImages = s.GetBool(ctx, KeyMistralOCRIncludeImages, cfg.MistralOCRIncludeImages)
	cfg.MistralOCRExtractHeader = s.GetBool(ctx, KeyMistralOCRExtractHeader, cfg.MistralOCRExtractHeader)
	cfg.MistralOCRExtractFooter = s.GetBool(ctx, KeyMistralOCRExtractFooter, cfg.MistralOCRExtractFooter)
	cfg.OCREnabled = s.GetBool(ctx, KeyOCREnabled, cfg.OCREnabled)
	cfg.OCRMaxPages = s.GetInt(ctx, KeyOCRMaxPages, cfg.OCRMaxPages)
	cfg.OCRMaxFileMB = s.GetInt(ctx, KeyOCRMaxFileMB, cfg.OCRMaxFileMB)

	cfg.ConvArchiveEnabled = s.GetBool(ctx, KeyConvArchiveEnabled, cfg.ConvArchiveEnabled)

	cfg.SummarizerEnabled = s.GetBool(ctx, KeySummarizerEnabled, cfg.SummarizerEnabled)
	cfg.SummarizerMode = s.GetString(ctx, KeySummarizerMode, cfg.SummarizerMode)
	cfg.SummarizerTurnInterval = s.GetInt(ctx, KeySummarizerTurnInterval, cfg.SummarizerTurnInterval)
	cfg.SummarizerMinSalience = s.GetFloat(ctx, KeySummarizerMinSalience, cfg.SummarizerMinSalience)
	cfg.SummarizerLookbackTurns = s.GetInt(ctx, KeySummarizerLookbackTurns, cfg.SummarizerLookbackTurns)
	cfg.SummarizerCooldownSeconds = s.GetInt(ctx, KeySummarizerCooldownSeconds, cfg.SummarizerCooldownSeconds)
}

// parseAllowlist mirrors the comma-split semantics in config.Load.
func parseAllowlist(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if id := strings.TrimSpace(p); id != "" {
			out = append(out, id)
		}
	}
	return out
}
