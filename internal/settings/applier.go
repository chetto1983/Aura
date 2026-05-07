package settings

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/config"
)

// Settings are persisted in SQLite and overlaid after the database opens.
// Early process fields such as AURA_HEADLESS, AURA_ENV_PATH, and DB_PATH are
// still surfaced so the dashboard can be the single place to edit config, but
// they only become authoritative after restart when the process can load them
// before opening long-lived resources.
const (
	KeyTelegramToken              = "TELEGRAM_TOKEN"
	KeyAllowlist                  = "TELEGRAM_ALLOWLIST"
	KeyHTTPPort                   = "HTTP_PORT"
	KeyTimezone                   = "AURA_TIMEZONE"
	KeyHeadless                   = "AURA_HEADLESS"
	KeyEnvPath                    = "AURA_ENV_PATH"
	KeyDBPath                     = "DB_PATH"
	KeyLogLevel                   = "LOG_LEVEL"
	KeyLogDir                     = "LOG_DIR"
	KeyWikiPath                   = "WIKI_PATH"
	KeySkillsPath                 = "SKILLS_PATH"
	KeySkillsInstallProjectDir    = "SKILLS_INSTALL_PROJECT_DIR"
	KeyMCPServersPath             = "MCP_SERVERS_PATH"
	KeyPromptOverlayPath          = "PROMPT_OVERLAY_PATH"
	KeyDashboardTokenTTLHours     = "DASHBOARD_TOKEN_TTL_HOURS"
	KeyMaxContextTokens           = "MAX_CONTEXT_TOKENS"
	KeyMaxHistoryMessages         = "MAX_HISTORY_MESSAGES"
	KeySoftBudget                 = "SOFT_BUDGET"
	KeyHardBudget                 = "HARD_BUDGET"
	KeyCostInputPerMTokens        = "COST_INPUT_PER_M_TOKENS"
	KeyCostOutputPerMTokens       = "COST_OUTPUT_PER_M_TOKENS"
	KeyLLMAPIKey                  = "LLM_API_KEY"
	KeyLLMBaseURL                 = "LLM_BASE_URL"
	KeyLLMModel                   = "LLM_MODEL"
	KeyLLMMaxRetries              = "LLM_MAX_RETRIES"
	KeyOllamaBaseURL              = "OLLAMA_BASE_URL"
	KeyOllamaModel                = "OLLAMA_MODEL"
	KeyOllamaAPIKey               = "OLLAMA_API_KEY"
	KeyOllamaWebBaseURL           = "OLLAMA_WEB_BASE_URL"
	KeyWebSearchProvider          = "WEB_SEARCH_PROVIDER"
	KeySearXNGBaseURL             = "SEARXNG_BASE_URL"
	KeyGarageS3Endpoint           = "GARAGE_S3_ENDPOINT"
	KeyGarageS3Region             = "GARAGE_S3_REGION"
	KeyGarageS3Bucket             = "GARAGE_S3_BUCKET"
	KeyGarageS3AccessKey          = "GARAGE_S3_ACCESS_KEY"
	KeyGarageS3SecretKey          = "GARAGE_S3_SECRET_KEY"
	KeyQdrantURL                  = "QDRANT_URL"
	KeyQdrantCollection           = "QDRANT_COLLECTION"
	KeyQdrantAPIKey               = "QDRANT_API_KEY"
	KeySearchBackend              = "SEARCH_BACKEND"
	KeySpeculativeSearchTimeoutMS = "SPECULATIVE_SEARCH_TIMEOUT_MS"
	KeyMemorySearchTimeoutMS      = "MEMORY_SEARCH_TIMEOUT_MS"
	KeyMaxToolIterations          = "MAX_TOOL_ITERATIONS"
	KeySkillsCatalogURL           = "SKILLS_CATALOG_URL"
	KeySkillsAdmin                = "SKILLS_ADMIN"
	KeyAuraBotEnabled             = "AURABOT_ENABLED"
	KeyAuraBotMaxActive           = "AURABOT_MAX_ACTIVE"
	KeyAuraBotMaxDepth            = "AURABOT_MAX_DEPTH"
	KeyAuraBotTimeoutSec          = "AURABOT_TIMEOUT_SEC"
	KeyAuraBotMaxIterations       = "AURABOT_MAX_ITERATIONS"
	KeyEmbeddingAPIKey            = "EMBEDDING_API_KEY"
	KeyEmbeddingBaseURL           = "EMBEDDING_BASE_URL"
	KeyEmbeddingModel             = "EMBEDDING_MODEL"
	KeyOTelEnabled                = "OTEL_ENABLED"
	KeyPromptVersion              = "AURA_PROMPT_VERSION"
	KeyToolProfileMode            = "AURA_TOOL_PROFILE_MODE"
	KeyOrchestrationLogLevel      = "AURA_ORCHESTRATION_LOG_LEVEL"
	KeySkillPreflight             = "AURA_SKILL_PREFLIGHT"
	KeyMistralAPIKey              = "MISTRAL_API_KEY"
	KeyMistralOCRModel            = "MISTRAL_OCR_MODEL"
	KeyMistralOCRBaseURL          = "MISTRAL_OCR_BASE_URL"
	KeyMistralOCRTableFormat      = "MISTRAL_OCR_TABLE_FORMAT"
	KeyMistralOCRIncludeImages    = "MISTRAL_OCR_INCLUDE_IMAGES"
	KeyMistralOCRExtractHeader    = "MISTRAL_OCR_EXTRACT_HEADER"
	KeyMistralOCRExtractFooter    = "MISTRAL_OCR_EXTRACT_FOOTER"
	KeyOCREnabled                 = "OCR_ENABLED"
	KeyOCRMaxPages                = "OCR_MAX_PAGES"
	KeyOCRMaxFileMB               = "OCR_MAX_FILE_MB"
	KeyConvArchiveEnabled         = "CONV_ARCHIVE_ENABLED"
	KeySummarizerEnabled          = "SUMMARIZER_ENABLED"
	KeySummarizerMode             = "SUMMARIZER_MODE"
	KeySummarizerTurnInterval     = "SUMMARIZER_TURN_INTERVAL"
	KeySummarizerMinSalience      = "SUMMARIZER_MIN_SALIENCE"
	KeySummarizerLookbackTurns    = "SUMMARIZER_LOOKBACK_TURNS"
	KeySummarizerCooldownSeconds  = "SUMMARIZER_COOLDOWN_SECONDS"
	KeySandboxEnabled             = "SANDBOX_ENABLED"
	KeySandboxRuntimeMode         = "SANDBOX_RUNTIME_MODE"
	KeySandboxRuntimeURL          = "SANDBOX_RUNTIME_URL"
	KeySandboxRuntimeDir          = "SANDBOX_RUNTIME_DIR"
	KeySandboxTimeoutSec          = "SANDBOX_TIMEOUT_SEC"
	KeySandboxAutoImproveMode     = "SANDBOX_AUTO_IMPROVE_MODE"
)

// OverridableKeys returns every key the applier touches. Callers (e.g. the
// settings UI handler) use this to validate that an inbound write
// targets a real config field instead of stuffing arbitrary KV pairs.
func OverridableKeys() []string {
	return []string{
		KeyTelegramToken,
		KeyAllowlist,
		KeyHTTPPort, KeyTimezone, KeyHeadless, KeyEnvPath, KeyDBPath,
		KeyLogLevel, KeyLogDir, KeyWikiPath, KeySkillsPath, KeySkillsInstallProjectDir,
		KeyMCPServersPath, KeyPromptOverlayPath, KeyDashboardTokenTTLHours,
		KeyMaxContextTokens, KeyMaxHistoryMessages,
		KeySoftBudget, KeyHardBudget, KeyCostInputPerMTokens, KeyCostOutputPerMTokens,
		KeyLLMAPIKey, KeyLLMBaseURL, KeyLLMModel, KeyLLMMaxRetries,
		KeyOllamaBaseURL, KeyOllamaModel, KeyOllamaAPIKey, KeyOllamaWebBaseURL,
		KeyWebSearchProvider, KeySearXNGBaseURL,
		KeyGarageS3Endpoint, KeyGarageS3Region, KeyGarageS3Bucket,
		KeyGarageS3AccessKey, KeyGarageS3SecretKey,
		KeyQdrantURL, KeyQdrantCollection, KeyQdrantAPIKey, KeySearchBackend, KeySpeculativeSearchTimeoutMS, KeyMemorySearchTimeoutMS,
		KeyMaxToolIterations,
		KeySkillsCatalogURL, KeySkillsAdmin,
		KeyAuraBotEnabled, KeyAuraBotMaxActive, KeyAuraBotMaxDepth,
		KeyAuraBotTimeoutSec, KeyAuraBotMaxIterations,
		KeyEmbeddingAPIKey, KeyEmbeddingBaseURL, KeyEmbeddingModel,
		KeyOTelEnabled, KeyPromptVersion, KeyToolProfileMode, KeyOrchestrationLogLevel, KeySkillPreflight,
		KeyMistralAPIKey, KeyMistralOCRModel, KeyMistralOCRBaseURL,
		KeyMistralOCRTableFormat, KeyMistralOCRIncludeImages,
		KeyMistralOCRExtractHeader, KeyMistralOCRExtractFooter,
		KeyOCREnabled, KeyOCRMaxPages, KeyOCRMaxFileMB,
		KeyConvArchiveEnabled,
		KeySummarizerEnabled, KeySummarizerMode, KeySummarizerTurnInterval,
		KeySummarizerMinSalience, KeySummarizerLookbackTurns, KeySummarizerCooldownSeconds,
		KeySandboxEnabled, KeySandboxRuntimeMode, KeySandboxRuntimeURL, KeySandboxRuntimeDir, KeySandboxTimeoutSec, KeySandboxAutoImproveMode,
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
func ApplyToConfig(ctx context.Context, s Reader, cfg *config.Config) {
	if s == nil || cfg == nil {
		return
	}

	cfg.TelegramToken = settingString(ctx, s, KeyTelegramToken, cfg.TelegramToken)

	if v, err := s.Get(ctx, KeyAllowlist); err == nil {
		// Re-use config's allowlist parser so semantics match Load.
		cfg.Allowlist = parseAllowlist(v)
		cfg.AllowlistConfigured = len(cfg.Allowlist) > 0
	}

	cfg.HTTPPort = settingString(ctx, s, KeyHTTPPort, cfg.HTTPPort)
	cfg.Timezone = settingString(ctx, s, KeyTimezone, cfg.Timezone)
	cfg.Headless = settingBool(ctx, s, KeyHeadless, cfg.Headless)
	cfg.EnvPath = settingString(ctx, s, KeyEnvPath, cfg.EnvPath)
	cfg.DBPath = settingString(ctx, s, KeyDBPath, cfg.DBPath)
	cfg.LogLevel = settingString(ctx, s, KeyLogLevel, cfg.LogLevel)
	cfg.LogDir = settingString(ctx, s, KeyLogDir, cfg.LogDir)
	cfg.WikiPath = settingString(ctx, s, KeyWikiPath, cfg.WikiPath)
	cfg.SkillsPath = settingString(ctx, s, KeySkillsPath, cfg.SkillsPath)
	cfg.SkillsInstallProjectDir = settingString(ctx, s, KeySkillsInstallProjectDir, cfg.SkillsInstallProjectDir)
	cfg.MCPServersPath = settingString(ctx, s, KeyMCPServersPath, cfg.MCPServersPath)
	cfg.PromptOverlayPath = settingString(ctx, s, KeyPromptOverlayPath, cfg.PromptOverlayPath)
	cfg.DashboardTokenTTLHours = settingInt(ctx, s, KeyDashboardTokenTTLHours, cfg.DashboardTokenTTLHours)

	cfg.MaxContextTokens = settingInt(ctx, s, KeyMaxContextTokens, cfg.MaxContextTokens)
	cfg.MaxHistoryMessages = settingInt(ctx, s, KeyMaxHistoryMessages, cfg.MaxHistoryMessages)
	cfg.SoftBudget = settingFloat(ctx, s, KeySoftBudget, cfg.SoftBudget)
	cfg.HardBudget = settingFloat(ctx, s, KeyHardBudget, cfg.HardBudget)
	if v := settingFloat(ctx, s, KeyCostInputPerMTokens, cfg.CostInputPerMTokens); v > 0 {
		cfg.CostInputPerMTokens = v
	}
	if v := settingFloat(ctx, s, KeyCostOutputPerMTokens, cfg.CostOutputPerMTokens); v > 0 {
		cfg.CostOutputPerMTokens = v
	}

	cfg.LLMAPIKey = settingString(ctx, s, KeyLLMAPIKey, cfg.LLMAPIKey)
	cfg.LLMBaseURL = settingString(ctx, s, KeyLLMBaseURL, cfg.LLMBaseURL)
	cfg.LLMModel = settingString(ctx, s, KeyLLMModel, cfg.LLMModel)
	cfg.LLMMaxRetries = settingInt(ctx, s, KeyLLMMaxRetries, cfg.LLMMaxRetries)

	cfg.OllamaBaseURL = settingString(ctx, s, KeyOllamaBaseURL, cfg.OllamaBaseURL)
	cfg.OllamaModel = settingString(ctx, s, KeyOllamaModel, cfg.OllamaModel)
	cfg.OllamaAPIKey = settingString(ctx, s, KeyOllamaAPIKey, cfg.OllamaAPIKey)
	cfg.OllamaWebBaseURL = settingString(ctx, s, KeyOllamaWebBaseURL, cfg.OllamaWebBaseURL)
	cfg.WebSearchProvider = strings.ToLower(strings.TrimSpace(settingString(ctx, s, KeyWebSearchProvider, cfg.WebSearchProvider)))
	cfg.SearXNGBaseURL = settingString(ctx, s, KeySearXNGBaseURL, cfg.SearXNGBaseURL)
	cfg.GarageS3Endpoint = settingString(ctx, s, KeyGarageS3Endpoint, cfg.GarageS3Endpoint)
	cfg.GarageS3Region = settingString(ctx, s, KeyGarageS3Region, cfg.GarageS3Region)
	cfg.GarageS3Bucket = settingString(ctx, s, KeyGarageS3Bucket, cfg.GarageS3Bucket)
	cfg.GarageS3AccessKey = settingString(ctx, s, KeyGarageS3AccessKey, cfg.GarageS3AccessKey)
	cfg.GarageS3SecretKey = settingString(ctx, s, KeyGarageS3SecretKey, cfg.GarageS3SecretKey)
	cfg.QdrantURL = settingString(ctx, s, KeyQdrantURL, cfg.QdrantURL)
	cfg.QdrantCollection = settingString(ctx, s, KeyQdrantCollection, cfg.QdrantCollection)
	cfg.QdrantAPIKey = settingString(ctx, s, KeyQdrantAPIKey, cfg.QdrantAPIKey)
	cfg.SearchBackend = strings.ToLower(strings.TrimSpace(settingString(ctx, s, KeySearchBackend, cfg.SearchBackend)))
	cfg.SpeculativeSearchTimeoutMS = settingInt(ctx, s, KeySpeculativeSearchTimeoutMS, cfg.SpeculativeSearchTimeoutMS)
	cfg.MemorySearchTimeoutMS = settingInt(ctx, s, KeyMemorySearchTimeoutMS, cfg.MemorySearchTimeoutMS)
	cfg.MaxToolIterations = settingInt(ctx, s, KeyMaxToolIterations, cfg.MaxToolIterations)

	cfg.SkillsCatalogURL = settingString(ctx, s, KeySkillsCatalogURL, cfg.SkillsCatalogURL)
	cfg.SkillsAdmin = settingBool(ctx, s, KeySkillsAdmin, cfg.SkillsAdmin)
	cfg.AuraBotEnabled = settingBool(ctx, s, KeyAuraBotEnabled, cfg.AuraBotEnabled)
	cfg.AuraBotMaxActive = settingInt(ctx, s, KeyAuraBotMaxActive, cfg.AuraBotMaxActive)
	cfg.AuraBotMaxDepth = settingInt(ctx, s, KeyAuraBotMaxDepth, cfg.AuraBotMaxDepth)
	cfg.AuraBotTimeoutSec = settingInt(ctx, s, KeyAuraBotTimeoutSec, cfg.AuraBotTimeoutSec)
	cfg.AuraBotMaxIterations = settingInt(ctx, s, KeyAuraBotMaxIterations, cfg.AuraBotMaxIterations)

	cfg.EmbeddingAPIKey = settingString(ctx, s, KeyEmbeddingAPIKey, cfg.EmbeddingAPIKey)
	cfg.EmbeddingBaseURL = settingString(ctx, s, KeyEmbeddingBaseURL, cfg.EmbeddingBaseURL)
	cfg.EmbeddingModel = settingString(ctx, s, KeyEmbeddingModel, cfg.EmbeddingModel)
	cfg.OTelEnabled = settingBool(ctx, s, KeyOTelEnabled, cfg.OTelEnabled)
	cfg.PromptVersion = settingString(ctx, s, KeyPromptVersion, cfg.PromptVersion)
	cfg.ToolProfileMode = strings.ToLower(strings.TrimSpace(settingString(ctx, s, KeyToolProfileMode, cfg.ToolProfileMode)))
	cfg.OrchestrationLogLevel = strings.ToLower(strings.TrimSpace(settingString(ctx, s, KeyOrchestrationLogLevel, cfg.OrchestrationLogLevel)))
	cfg.SkillPreflight = normalizeSkillPreflight(settingString(ctx, s, KeySkillPreflight, cfg.SkillPreflight))

	cfg.MistralAPIKey = settingString(ctx, s, KeyMistralAPIKey, cfg.MistralAPIKey)
	cfg.MistralOCRModel = settingString(ctx, s, KeyMistralOCRModel, cfg.MistralOCRModel)
	cfg.MistralOCRBaseURL = settingString(ctx, s, KeyMistralOCRBaseURL, cfg.MistralOCRBaseURL)
	cfg.MistralOCRTableFormat = settingString(ctx, s, KeyMistralOCRTableFormat, cfg.MistralOCRTableFormat)
	cfg.MistralOCRIncludeImages = settingBool(ctx, s, KeyMistralOCRIncludeImages, cfg.MistralOCRIncludeImages)
	cfg.MistralOCRExtractHeader = settingBool(ctx, s, KeyMistralOCRExtractHeader, cfg.MistralOCRExtractHeader)
	cfg.MistralOCRExtractFooter = settingBool(ctx, s, KeyMistralOCRExtractFooter, cfg.MistralOCRExtractFooter)
	cfg.OCREnabled = settingBool(ctx, s, KeyOCREnabled, cfg.OCREnabled)
	cfg.OCRMaxPages = settingInt(ctx, s, KeyOCRMaxPages, cfg.OCRMaxPages)
	cfg.OCRMaxFileMB = settingInt(ctx, s, KeyOCRMaxFileMB, cfg.OCRMaxFileMB)

	cfg.ConvArchiveEnabled = settingBool(ctx, s, KeyConvArchiveEnabled, cfg.ConvArchiveEnabled)

	cfg.SummarizerEnabled = settingBool(ctx, s, KeySummarizerEnabled, cfg.SummarizerEnabled)
	cfg.SummarizerMode = config.NormalizeSummarizerMode(settingString(ctx, s, KeySummarizerMode, cfg.SummarizerMode))
	cfg.SummarizerTurnInterval = settingInt(ctx, s, KeySummarizerTurnInterval, cfg.SummarizerTurnInterval)
	cfg.SummarizerMinSalience = settingFloat(ctx, s, KeySummarizerMinSalience, cfg.SummarizerMinSalience)
	cfg.SummarizerLookbackTurns = settingInt(ctx, s, KeySummarizerLookbackTurns, cfg.SummarizerLookbackTurns)
	cfg.SummarizerCooldownSeconds = settingInt(ctx, s, KeySummarizerCooldownSeconds, cfg.SummarizerCooldownSeconds)

	cfg.SandboxEnabled = settingBool(ctx, s, KeySandboxEnabled, cfg.SandboxEnabled)
	cfg.SandboxRuntimeMode = strings.ToLower(strings.TrimSpace(settingString(ctx, s, KeySandboxRuntimeMode, cfg.SandboxRuntimeMode)))
	cfg.SandboxRuntimeURL = settingString(ctx, s, KeySandboxRuntimeURL, cfg.SandboxRuntimeURL)
	cfg.SandboxRuntimeDir = settingString(ctx, s, KeySandboxRuntimeDir, cfg.SandboxRuntimeDir)
	cfg.SandboxTimeoutSec = settingInt(ctx, s, KeySandboxTimeoutSec, cfg.SandboxTimeoutSec)
	cfg.SandboxAutoImproveMode = settingString(ctx, s, KeySandboxAutoImproveMode, cfg.SandboxAutoImproveMode)
}

func normalizeSkillPreflight(value string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "required", "advisory", "off":
		return normalized
	default:
		return config.DefaultSkillPreflight
	}
}

func settingString(ctx context.Context, s Reader, key, fallback string) string {
	v, err := s.Get(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func settingInt(ctx context.Context, s Reader, key string, fallback int) int {
	v, err := s.Get(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}

func settingFloat(ctx context.Context, s Reader, key string, fallback float64) float64 {
	v, err := s.Get(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
	if err != nil {
		return fallback
	}
	return f
}

func settingBool(ctx context.Context, s Reader, key string, fallback bool) bool {
	v, err := s.Get(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	b, err := strconv.ParseBool(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return b
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
