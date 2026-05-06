package settings

import (
	"context"
	"slices"
	"strings"

	"github.com/aura/aura/internal/config"
)

// Settings are persisted in SQLite and overlaid after the database opens.
// Early process fields such as AURA_HEADLESS, AURA_ENV_PATH, and DB_PATH are
// still surfaced so the dashboard can be the single place to edit config, but
// they only become authoritative after restart when the process can load them
// before opening long-lived resources.
const (
	KeyTelegramToken             = "TELEGRAM_TOKEN"
	KeyAllowlist                 = "TELEGRAM_ALLOWLIST"
	KeyHTTPPort                  = "HTTP_PORT"
	KeyHeadless                  = "AURA_HEADLESS"
	KeyEnvPath                   = "AURA_ENV_PATH"
	KeyDBPath                    = "DB_PATH"
	KeyLogLevel                  = "LOG_LEVEL"
	KeyLogDir                    = "LOG_DIR"
	KeyWikiPath                  = "WIKI_PATH"
	KeySkillsPath                = "SKILLS_PATH"
	KeySkillsInstallProjectDir   = "SKILLS_INSTALL_PROJECT_DIR"
	KeyMCPServersPath            = "MCP_SERVERS_PATH"
	KeyPromptOverlayPath         = "PROMPT_OVERLAY_PATH"
	KeyDashboardTokenTTLHours    = "DASHBOARD_TOKEN_TTL_HOURS"
	KeyMaxContextTokens          = "MAX_CONTEXT_TOKENS"
	KeyMaxHistoryMessages        = "MAX_HISTORY_MESSAGES"
	KeySoftBudget                = "SOFT_BUDGET"
	KeyHardBudget                = "HARD_BUDGET"
	KeyCostInputPerMTokens       = "COST_INPUT_PER_M_TOKENS"
	KeyCostOutputPerMTokens      = "COST_OUTPUT_PER_M_TOKENS"
	KeyLLMAPIKey                 = "LLM_API_KEY"
	KeyLLMBaseURL                = "LLM_BASE_URL"
	KeyLLMModel                  = "LLM_MODEL"
	KeyLLMMaxRetries             = "LLM_MAX_RETRIES"
	KeyOllamaBaseURL             = "OLLAMA_BASE_URL"
	KeyOllamaModel               = "OLLAMA_MODEL"
	KeyOllamaAPIKey              = "OLLAMA_API_KEY"
	KeyOllamaWebBaseURL          = "OLLAMA_WEB_BASE_URL"
	KeyWebSearchProvider         = "WEB_SEARCH_PROVIDER"
	KeySearXNGBaseURL            = "SEARXNG_BASE_URL"
	KeyGarageS3Endpoint          = "GARAGE_S3_ENDPOINT"
	KeyGarageS3Region            = "GARAGE_S3_REGION"
	KeyGarageS3Bucket            = "GARAGE_S3_BUCKET"
	KeyGarageS3AccessKey         = "GARAGE_S3_ACCESS_KEY"
	KeyGarageS3SecretKey         = "GARAGE_S3_SECRET_KEY"
	KeyQdrantURL                 = "QDRANT_URL"
	KeyQdrantCollection          = "QDRANT_COLLECTION"
	KeyQdrantAPIKey              = "QDRANT_API_KEY"
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
	KeyPromptVersion             = "AURA_PROMPT_VERSION"
	KeyToolProfileMode           = "AURA_TOOL_PROFILE_MODE"
	KeyOrchestrationLogLevel     = "AURA_ORCHESTRATION_LOG_LEVEL"
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
	KeySandboxEnabled            = "SANDBOX_ENABLED"
	KeySandboxRuntimeDir         = "SANDBOX_RUNTIME_DIR"
	KeySandboxTimeoutSec         = "SANDBOX_TIMEOUT_SEC"
	KeySandboxAutoImproveMode    = "SANDBOX_AUTO_IMPROVE_MODE"
)

// OverridableKeys returns every key the applier touches. Callers (e.g. the
// settings UI handler) use this to validate that an inbound write
// targets a real config field instead of stuffing arbitrary KV pairs.
func OverridableKeys() []string {
	return []string{
		KeyTelegramToken,
		KeyAllowlist,
		KeyHTTPPort, KeyHeadless, KeyEnvPath, KeyDBPath,
		KeyLogLevel, KeyLogDir, KeyWikiPath, KeySkillsPath, KeySkillsInstallProjectDir,
		KeyMCPServersPath, KeyPromptOverlayPath, KeyDashboardTokenTTLHours,
		KeyMaxContextTokens, KeyMaxHistoryMessages,
		KeySoftBudget, KeyHardBudget, KeyCostInputPerMTokens, KeyCostOutputPerMTokens,
		KeyLLMAPIKey, KeyLLMBaseURL, KeyLLMModel, KeyLLMMaxRetries,
		KeyOllamaBaseURL, KeyOllamaModel, KeyOllamaAPIKey, KeyOllamaWebBaseURL,
		KeyWebSearchProvider, KeySearXNGBaseURL,
		KeyGarageS3Endpoint, KeyGarageS3Region, KeyGarageS3Bucket,
		KeyGarageS3AccessKey, KeyGarageS3SecretKey,
		KeyQdrantURL, KeyQdrantCollection, KeyQdrantAPIKey,
		KeyMaxToolIterations,
		KeySkillsCatalogURL, KeySkillsAdmin,
		KeyAuraBotEnabled, KeyAuraBotMaxActive, KeyAuraBotMaxDepth,
		KeyAuraBotTimeoutSec, KeyAuraBotMaxIterations,
		KeyEmbeddingAPIKey, KeyEmbeddingBaseURL, KeyEmbeddingModel,
		KeyOTelEnabled, KeyPromptVersion, KeyToolProfileMode, KeyOrchestrationLogLevel,
		KeyMistralAPIKey, KeyMistralOCRModel, KeyMistralOCRBaseURL,
		KeyMistralOCRTableFormat, KeyMistralOCRIncludeImages,
		KeyMistralOCRExtractHeader, KeyMistralOCRExtractFooter,
		KeyOCREnabled, KeyOCRMaxPages, KeyOCRMaxFileMB,
		KeyConvArchiveEnabled,
		KeySummarizerEnabled, KeySummarizerMode, KeySummarizerTurnInterval,
		KeySummarizerMinSalience, KeySummarizerLookbackTurns, KeySummarizerCooldownSeconds,
		KeySandboxEnabled, KeySandboxRuntimeDir, KeySandboxTimeoutSec, KeySandboxAutoImproveMode,
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

	cfg.TelegramToken = s.GetString(ctx, KeyTelegramToken, cfg.TelegramToken)

	if v, err := s.Get(ctx, KeyAllowlist); err == nil {
		// Re-use config's allowlist parser so semantics match Load.
		cfg.Allowlist = parseAllowlist(v)
		cfg.AllowlistConfigured = len(cfg.Allowlist) > 0
	}

	cfg.HTTPPort = s.GetString(ctx, KeyHTTPPort, cfg.HTTPPort)
	cfg.Headless = s.GetBool(ctx, KeyHeadless, cfg.Headless)
	cfg.EnvPath = s.GetString(ctx, KeyEnvPath, cfg.EnvPath)
	cfg.DBPath = s.GetString(ctx, KeyDBPath, cfg.DBPath)
	cfg.LogLevel = s.GetString(ctx, KeyLogLevel, cfg.LogLevel)
	cfg.LogDir = s.GetString(ctx, KeyLogDir, cfg.LogDir)
	cfg.WikiPath = s.GetString(ctx, KeyWikiPath, cfg.WikiPath)
	cfg.SkillsPath = s.GetString(ctx, KeySkillsPath, cfg.SkillsPath)
	cfg.SkillsInstallProjectDir = s.GetString(ctx, KeySkillsInstallProjectDir, cfg.SkillsInstallProjectDir)
	cfg.MCPServersPath = s.GetString(ctx, KeyMCPServersPath, cfg.MCPServersPath)
	cfg.PromptOverlayPath = s.GetString(ctx, KeyPromptOverlayPath, cfg.PromptOverlayPath)
	cfg.DashboardTokenTTLHours = s.GetInt(ctx, KeyDashboardTokenTTLHours, cfg.DashboardTokenTTLHours)

	cfg.MaxContextTokens = s.GetInt(ctx, KeyMaxContextTokens, cfg.MaxContextTokens)
	cfg.MaxHistoryMessages = s.GetInt(ctx, KeyMaxHistoryMessages, cfg.MaxHistoryMessages)
	cfg.SoftBudget = s.GetFloat(ctx, KeySoftBudget, cfg.SoftBudget)
	cfg.HardBudget = s.GetFloat(ctx, KeyHardBudget, cfg.HardBudget)
	if v := s.GetFloat(ctx, KeyCostInputPerMTokens, cfg.CostInputPerMTokens); v > 0 {
		cfg.CostInputPerMTokens = v
	}
	if v := s.GetFloat(ctx, KeyCostOutputPerMTokens, cfg.CostOutputPerMTokens); v > 0 {
		cfg.CostOutputPerMTokens = v
	}

	cfg.LLMAPIKey = s.GetString(ctx, KeyLLMAPIKey, cfg.LLMAPIKey)
	cfg.LLMBaseURL = s.GetString(ctx, KeyLLMBaseURL, cfg.LLMBaseURL)
	cfg.LLMModel = s.GetString(ctx, KeyLLMModel, cfg.LLMModel)
	cfg.LLMMaxRetries = s.GetInt(ctx, KeyLLMMaxRetries, cfg.LLMMaxRetries)

	cfg.OllamaBaseURL = s.GetString(ctx, KeyOllamaBaseURL, cfg.OllamaBaseURL)
	cfg.OllamaModel = s.GetString(ctx, KeyOllamaModel, cfg.OllamaModel)
	cfg.OllamaAPIKey = s.GetString(ctx, KeyOllamaAPIKey, cfg.OllamaAPIKey)
	cfg.OllamaWebBaseURL = s.GetString(ctx, KeyOllamaWebBaseURL, cfg.OllamaWebBaseURL)
	cfg.WebSearchProvider = strings.ToLower(strings.TrimSpace(s.GetString(ctx, KeyWebSearchProvider, cfg.WebSearchProvider)))
	cfg.SearXNGBaseURL = s.GetString(ctx, KeySearXNGBaseURL, cfg.SearXNGBaseURL)
	cfg.GarageS3Endpoint = s.GetString(ctx, KeyGarageS3Endpoint, cfg.GarageS3Endpoint)
	cfg.GarageS3Region = s.GetString(ctx, KeyGarageS3Region, cfg.GarageS3Region)
	cfg.GarageS3Bucket = s.GetString(ctx, KeyGarageS3Bucket, cfg.GarageS3Bucket)
	cfg.GarageS3AccessKey = s.GetString(ctx, KeyGarageS3AccessKey, cfg.GarageS3AccessKey)
	cfg.GarageS3SecretKey = s.GetString(ctx, KeyGarageS3SecretKey, cfg.GarageS3SecretKey)
	cfg.QdrantURL = s.GetString(ctx, KeyQdrantURL, cfg.QdrantURL)
	cfg.QdrantCollection = s.GetString(ctx, KeyQdrantCollection, cfg.QdrantCollection)
	cfg.QdrantAPIKey = s.GetString(ctx, KeyQdrantAPIKey, cfg.QdrantAPIKey)
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
	cfg.PromptVersion = s.GetString(ctx, KeyPromptVersion, cfg.PromptVersion)
	cfg.ToolProfileMode = strings.ToLower(strings.TrimSpace(s.GetString(ctx, KeyToolProfileMode, cfg.ToolProfileMode)))
	cfg.OrchestrationLogLevel = strings.ToLower(strings.TrimSpace(s.GetString(ctx, KeyOrchestrationLogLevel, cfg.OrchestrationLogLevel)))

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

	cfg.SandboxEnabled = s.GetBool(ctx, KeySandboxEnabled, cfg.SandboxEnabled)
	cfg.SandboxRuntimeDir = s.GetString(ctx, KeySandboxRuntimeDir, cfg.SandboxRuntimeDir)
	cfg.SandboxTimeoutSec = s.GetInt(ctx, KeySandboxTimeoutSec, cfg.SandboxTimeoutSec)
	cfg.SandboxAutoImproveMode = s.GetString(ctx, KeySandboxAutoImproveMode, cfg.SandboxAutoImproveMode)
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
