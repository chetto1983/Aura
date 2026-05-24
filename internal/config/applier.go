package config

import (
	"context"
	"slices"
	"strconv"
	"strings"

)

// Settings are persisted in SQLite and overlaid after the database opens.
// Early process fields such as AURA_HEADLESS and DB_PATH are still surfaced
// so the dashboard can be the single place to edit config, but they only
// become authoritative after restart when the process can load them before
// opening long-lived resources.
const (
	KeyTelegramToken           = "TELEGRAM_TOKEN"
	KeyAllowlist               = "TELEGRAM_ALLOWLIST"
	KeyHTTPPort                = "HTTP_PORT"
	KeyTimezone                = "AURA_TIMEZONE"
	KeyHeadless                = "AURA_HEADLESS"
	KeyDBPath                  = "DB_PATH"
	KeyLogLevel                = "LOG_LEVEL"
	KeyLogDir                  = "LOG_DIR"
	KeyWikiPath                = "WIKI_PATH"
	KeySourcesPath             = "SOURCES_PATH"
	KeySkillsPath              = "SKILLS_PATH"
	KeySkillsInstallProjectDir = "SKILLS_INSTALL_PROJECT_DIR"
	KeyMCPServersPath          = "MCP_SERVERS_PATH"
	KeyPromptOverlayPath       = "PROMPT_OVERLAY_PATH"
	KeyDashboardTokenTTLHours  = "DASHBOARD_TOKEN_TTL_HOURS"
	KeyMaxContextTokens        = "MAX_CONTEXT_TOKENS"
	KeyMaxHistoryMessages      = "MAX_HISTORY_MESSAGES"
	KeySoftBudget              = "SOFT_BUDGET"
	KeyHardBudget              = "HARD_BUDGET"
	KeyCostInputPerMTokens     = "COST_INPUT_PER_M_TOKENS"
	KeyCostOutputPerMTokens    = "COST_OUTPUT_PER_M_TOKENS"
	KeyLLMAPIKey               = "LLM_API_KEY"
	KeyLLMBaseURL              = "LLM_BASE_URL"
	KeyLLMModel                = "LLM_MODEL"
	KeyLLMMaxRetries           = "LLM_MAX_RETRIES"
	KeyWebSearchProvider       = "WEB_SEARCH_PROVIDER"
	KeySearXNGBaseURL          = "SEARXNG_BASE_URL"
	KeyMarkitdownURL           = "MARKITDOWN_URL"
	KeyMarkitdownTimeoutSec    = "MARKITDOWN_TIMEOUT_SEC"
	KeyOCRBackend              = "OCR_BACKEND"
	KeyGarageS3Endpoint        = "GARAGE_S3_ENDPOINT"
	KeyGarageS3Region          = "GARAGE_S3_REGION"
	KeyGarageS3Bucket          = "GARAGE_S3_BUCKET"
	KeyGarageS3AccessKey       = "GARAGE_S3_ACCESS_KEY"
	KeyGarageS3SecretKey       = "GARAGE_S3_SECRET_KEY"
	KeyQdrantURL               = "QDRANT_URL"
	KeyQdrantCollection        = "QDRANT_COLLECTION"
	KeyQdrantAPIKey            = "QDRANT_API_KEY"
	KeyMemorySearchTimeoutMS   = "MEMORY_SEARCH_TIMEOUT_MS"
	KeySkillsCatalogURL        = "SKILLS_CATALOG_URL"
	KeySkillsAdmin             = "SKILLS_ADMIN"
	KeyAuraBotEnabled          = "AURABOT_ENABLED"
	KeyAuraBotMaxActive        = "AURABOT_MAX_ACTIVE"
	KeyAuraBotMaxDepth         = "AURABOT_MAX_DEPTH"
	KeyAuraBotTimeoutSec       = "AURABOT_TIMEOUT_SEC"
	KeyAuraBotMaxIterations    = "AURABOT_MAX_ITERATIONS"
	KeyEmbeddingAPIKey         = "EMBEDDING_API_KEY"
	KeyEmbeddingBaseURL        = "EMBEDDING_BASE_URL"
	KeyEmbeddingModel          = "EMBEDDING_MODEL"
	KeyEmbeddingOutputDim      = "EMBEDDING_OUTPUT_DIM"
	KeyPromptVersion           = "AURA_PROMPT_VERSION"
	KeySkillRoutingMode        = "AURA_SKILL_ROUTING_MODE"
	KeyAgentLoopMaxSteps       = "AURA_AGENT_LOOP_MAX_STEPS"
	KeyReasoningEffort         = "AURA_REASONING_EFFORT"
	KeyMaxToolResultChars      = "MAX_TOOL_RESULT_CHARS"
	KeyMicrocompactKeepRecent  = "MICROCOMPACT_KEEP_RECENT"
	KeyMicrocompactMinChars    = "MICROCOMPACT_MIN_CHARS"
	KeyTerminalToolPolicy      = "AURA_TERMINAL_TOOL_POLICY"
	KeyDelegationMode          = "AURA_DELEGATION_MODE"
	KeyTraceRetentionDays      = "AURA_TRACE_RETENTION_DAYS"
	KeyWorkspaceTools          = "AURA_WORKSPACE_TOOLS"
	KeyWorkspaceRoot           = "AURA_WORKSPACE_ROOT"
	KeyMistralAPIKey           = "MISTRAL_API_KEY"
	KeyMistralOCRModel         = "MISTRAL_OCR_MODEL"
	KeyMistralOCRBaseURL       = "MISTRAL_OCR_BASE_URL"
	KeyMistralOCRTableFormat   = "MISTRAL_OCR_TABLE_FORMAT"
	KeyMistralOCRExtractHeader = "MISTRAL_OCR_EXTRACT_HEADER"
	KeyMistralOCRExtractFooter = "MISTRAL_OCR_EXTRACT_FOOTER"
	KeyOCRMaxPages             = "OCR_MAX_PAGES"
	KeyOCRMaxFileMB            = "OCR_MAX_FILE_MB"
	KeyConvArchiveEnabled      = "CONV_ARCHIVE_ENABLED"
	KeySandboxEnabled          = "SANDBOX_ENABLED"
	KeySandboxTimeoutSec       = "SANDBOX_TIMEOUT_SEC"

	// ModelContextWindow override (US-CTX-00). 0 = auto-detect from /models.
	KeyModelContextWindow = "AURA_MODEL_CONTEXT_WINDOW"

	// Context compaction knobs (US-CTX-04).
	KeyCTXCompactPercent = "AURA_CTX_COMPACT_PERCENT"
	KeyCTXCompactScope   = "AURA_CTX_COMPACT_SCOPE"

	// Per-class tool budget caps (US-OUT-07). Dashboard keys use lowercase
	// snake_case; env vars use AURA_TOOL_BUDGET_<CLASS>.
	KeyToolBudgetWeb       = "tool_budget_web"
	KeyToolBudgetWiki      = "tool_budget_wiki"
	KeyToolBudgetExec      = "tool_budget_exec"
	KeyToolBudgetSource    = "tool_budget_source"
	KeyToolBudgetScheduler = "tool_budget_scheduler"
	KeyToolBudgetAskUser   = "tool_budget_ask_user"
	KeyToolBudgetDefault   = "tool_budget_default"
)

// OverridableKeys returns every key the applier touches. Callers (e.g. the
// settings UI handler) use this to validate that an inbound write
// targets a real config field instead of stuffing arbitrary KV pairs.
func OverridableKeys() []string {
	return []string{
		KeyTelegramToken,
		KeyAllowlist,
		KeyHTTPPort, KeyTimezone, KeyHeadless, KeyDBPath,
		KeyLogLevel, KeyLogDir, KeyWikiPath, KeySourcesPath, KeySkillsPath, KeySkillsInstallProjectDir,
		KeyMCPServersPath, KeyPromptOverlayPath, KeyDashboardTokenTTLHours,
		KeyMaxContextTokens, KeyMaxHistoryMessages,
		KeySoftBudget, KeyHardBudget, KeyCostInputPerMTokens, KeyCostOutputPerMTokens,
		KeyLLMAPIKey, KeyLLMBaseURL, KeyLLMModel, KeyLLMMaxRetries,
		KeyWebSearchProvider, KeySearXNGBaseURL,
		KeyMarkitdownURL, KeyMarkitdownTimeoutSec, KeyOCRBackend,
		KeyGarageS3Endpoint, KeyGarageS3Region, KeyGarageS3Bucket,
		KeyGarageS3AccessKey, KeyGarageS3SecretKey,
		KeyQdrantURL, KeyQdrantCollection, KeyQdrantAPIKey, KeyMemorySearchTimeoutMS,
		KeySkillsCatalogURL, KeySkillsAdmin,
		KeyAuraBotEnabled, KeyAuraBotMaxActive, KeyAuraBotMaxDepth,
		KeyAuraBotTimeoutSec, KeyAuraBotMaxIterations,
		KeyEmbeddingAPIKey, KeyEmbeddingBaseURL, KeyEmbeddingModel, KeyEmbeddingOutputDim,
		KeyPromptVersion,
		KeySkillRoutingMode, KeyAgentLoopMaxSteps, KeyReasoningEffort,
		KeyMaxToolResultChars, KeyMicrocompactKeepRecent, KeyMicrocompactMinChars,
		KeyTerminalToolPolicy, KeyDelegationMode, KeyTraceRetentionDays,
		KeyWorkspaceTools, KeyWorkspaceRoot,
		KeyMistralAPIKey, KeyMistralOCRModel, KeyMistralOCRBaseURL,
		KeyMistralOCRTableFormat,
		KeyMistralOCRExtractHeader, KeyMistralOCRExtractFooter,
		KeyOCRMaxPages, KeyOCRMaxFileMB,
		KeyConvArchiveEnabled,
		KeySandboxEnabled, KeySandboxTimeoutSec,
		KeyToolBudgetWeb, KeyToolBudgetWiki, KeyToolBudgetExec, KeyToolBudgetSource,
		KeyToolBudgetScheduler, KeyToolBudgetAskUser, KeyToolBudgetDefault,
		KeyModelContextWindow,
		KeyCTXCompactPercent, KeyCTXCompactScope,
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
func ApplyToConfig(ctx context.Context, s Reader, cfg *Config) {
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
	cfg.DBPath = settingString(ctx, s, KeyDBPath, cfg.DBPath)
	cfg.LogLevel = settingString(ctx, s, KeyLogLevel, cfg.LogLevel)
	cfg.LogDir = settingString(ctx, s, KeyLogDir, cfg.LogDir)
	cfg.WikiPath = settingString(ctx, s, KeyWikiPath, cfg.WikiPath)
	cfg.SourcesPath = settingString(ctx, s, KeySourcesPath, cfg.SourcesPath)
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

	cfg.WebSearchProvider = strings.ToLower(strings.TrimSpace(settingString(ctx, s, KeyWebSearchProvider, cfg.WebSearchProvider)))
	cfg.SearXNGBaseURL = settingString(ctx, s, KeySearXNGBaseURL, cfg.SearXNGBaseURL)
	cfg.MarkitdownURL = settingString(ctx, s, KeyMarkitdownURL, cfg.MarkitdownURL)
	cfg.MarkitdownTimeoutSec = settingInt(ctx, s, KeyMarkitdownTimeoutSec, cfg.MarkitdownTimeoutSec)
	// KeyOCRBackend is reserved for Wave 2.9.5. Aura reads but does not act on
	// it yet — the row is dashboard-visible so users can pre-select.
	cfg.GarageS3Endpoint = settingString(ctx, s, KeyGarageS3Endpoint, cfg.GarageS3Endpoint)
	cfg.GarageS3Region = settingString(ctx, s, KeyGarageS3Region, cfg.GarageS3Region)
	cfg.GarageS3Bucket = settingString(ctx, s, KeyGarageS3Bucket, cfg.GarageS3Bucket)
	cfg.GarageS3AccessKey = settingString(ctx, s, KeyGarageS3AccessKey, cfg.GarageS3AccessKey)
	cfg.GarageS3SecretKey = settingString(ctx, s, KeyGarageS3SecretKey, cfg.GarageS3SecretKey)
	cfg.QdrantURL = settingString(ctx, s, KeyQdrantURL, cfg.QdrantURL)
	cfg.QdrantCollection = settingString(ctx, s, KeyQdrantCollection, cfg.QdrantCollection)
	cfg.QdrantAPIKey = settingString(ctx, s, KeyQdrantAPIKey, cfg.QdrantAPIKey)
	cfg.MemorySearchTimeoutMS = settingInt(ctx, s, KeyMemorySearchTimeoutMS, cfg.MemorySearchTimeoutMS)

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
	cfg.EmbeddingOutputDim = settingIntRange(ctx, s, KeyEmbeddingOutputDim, cfg.EmbeddingOutputDim, 0, 768, 0)
	cfg.PromptVersion = settingString(ctx, s, KeyPromptVersion, cfg.PromptVersion)
	cfg.SkillRoutingMode = NormalizeSkillRoutingMode(settingString(ctx, s, KeySkillRoutingMode, cfg.SkillRoutingMode))
	cfg.AgentLoopMaxSteps = settingIntRange(ctx, s, KeyAgentLoopMaxSteps, cfg.AgentLoopMaxSteps, 1, 10000, DefaultAgentLoopMaxSteps)
	cfg.ReasoningEffort = NormalizeReasoningEffort(settingString(ctx, s, KeyReasoningEffort, cfg.ReasoningEffort))
	cfg.MaxToolResultChars = settingIntRange(ctx, s, KeyMaxToolResultChars, cfg.MaxToolResultChars, 1000, 500000, DefaultMaxToolResultChars)
	cfg.MicrocompactKeepRecent = settingIntRange(ctx, s, KeyMicrocompactKeepRecent, cfg.MicrocompactKeepRecent, 1, 500, DefaultMicrocompactKeepRecent)
	cfg.MicrocompactMinChars = settingIntRange(ctx, s, KeyMicrocompactMinChars, cfg.MicrocompactMinChars, 100, 100000, DefaultMicrocompactMinChars)
	cfg.TerminalToolPolicy = NormalizeTerminalToolPolicy(settingString(ctx, s, KeyTerminalToolPolicy, cfg.TerminalToolPolicy))
	cfg.DelegationMode = NormalizeDelegationMode(settingString(ctx, s, KeyDelegationMode, cfg.DelegationMode))
	cfg.TraceRetentionDays = settingIntRange(ctx, s, KeyTraceRetentionDays, cfg.TraceRetentionDays, 1, 365, DefaultTraceRetentionDays)
	cfg.WorkspaceTools = NormalizeWorkspaceTools(settingString(ctx, s, KeyWorkspaceTools, cfg.WorkspaceTools))
	cfg.WorkspaceRoot = settingString(ctx, s, KeyWorkspaceRoot, cfg.WorkspaceRoot)

	cfg.MistralAPIKey = settingString(ctx, s, KeyMistralAPIKey, cfg.MistralAPIKey)
	cfg.MistralOCRModel = settingString(ctx, s, KeyMistralOCRModel, cfg.MistralOCRModel)
	cfg.MistralOCRBaseURL = settingString(ctx, s, KeyMistralOCRBaseURL, cfg.MistralOCRBaseURL)
	cfg.MistralOCRTableFormat = settingString(ctx, s, KeyMistralOCRTableFormat, cfg.MistralOCRTableFormat)
	cfg.MistralOCRExtractHeader = settingBool(ctx, s, KeyMistralOCRExtractHeader, cfg.MistralOCRExtractHeader)
	cfg.MistralOCRExtractFooter = settingBool(ctx, s, KeyMistralOCRExtractFooter, cfg.MistralOCRExtractFooter)
	cfg.OCRMaxPages = settingInt(ctx, s, KeyOCRMaxPages, cfg.OCRMaxPages)
	cfg.OCRMaxFileMB = settingInt(ctx, s, KeyOCRMaxFileMB, cfg.OCRMaxFileMB)

	cfg.ConvArchiveEnabled = settingBool(ctx, s, KeyConvArchiveEnabled, cfg.ConvArchiveEnabled)

	cfg.SandboxEnabled = settingBool(ctx, s, KeySandboxEnabled, cfg.SandboxEnabled)
	cfg.SandboxTimeoutSec = settingInt(ctx, s, KeySandboxTimeoutSec, cfg.SandboxTimeoutSec)

	// ModelContextWindow override (US-CTX-00). 0 = auto-detect; positive = manual override.
	if v := settingInt(ctx, s, KeyModelContextWindow, -1); v >= 0 {
		cfg.ModelContextWindow = v
	}

	// Context compaction knobs (US-CTX-04). Clamp percent to [0.20, 0.90]; 0 disables.
	if v := settingFloat(ctx, s, KeyCTXCompactPercent, -1); v >= 0 {
		if v != 0 {
			if v < 0.20 {
				v = 0.20
			} else if v > 0.90 {
				v = 0.90
			}
		}
		cfg.CTXCompactPercent = v
	}
	cfg.CTXCompactScope = normalizeCTXCompactScope(settingString(ctx, s, KeyCTXCompactScope, cfg.CTXCompactScope))

	// Per-class tool budget caps (US-OUT-07). Clamped to [1,100]; invalid rows
	// leave the env-loaded value intact (fail-soft).
	cfg.ToolBudgetWeb = settingIntRange(ctx, s, KeyToolBudgetWeb, cfg.ToolBudgetWeb, 1, 100, cfg.ToolBudgetWeb)
	cfg.ToolBudgetWiki = settingIntRange(ctx, s, KeyToolBudgetWiki, cfg.ToolBudgetWiki, 1, 100, cfg.ToolBudgetWiki)
	cfg.ToolBudgetExec = settingIntRange(ctx, s, KeyToolBudgetExec, cfg.ToolBudgetExec, 1, 100, cfg.ToolBudgetExec)
	cfg.ToolBudgetSource = settingIntRange(ctx, s, KeyToolBudgetSource, cfg.ToolBudgetSource, 1, 100, cfg.ToolBudgetSource)
	cfg.ToolBudgetScheduler = settingIntRange(ctx, s, KeyToolBudgetScheduler, cfg.ToolBudgetScheduler, 1, 100, cfg.ToolBudgetScheduler)
	cfg.ToolBudgetAskUser = settingIntRange(ctx, s, KeyToolBudgetAskUser, cfg.ToolBudgetAskUser, 1, 100, cfg.ToolBudgetAskUser)
	cfg.ToolBudgetDefault = settingIntRange(ctx, s, KeyToolBudgetDefault, cfg.ToolBudgetDefault, 1, 100, cfg.ToolBudgetDefault)
}

func settingString(ctx context.Context, s Reader, key, fallback string) string {
	v, err := s.Get(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	return v
}

func settingInt(ctx context.Context, s Reader, key string, fallback int) int {
	return settingParse(ctx, s, key, fallback, strconv.Atoi)
}

// settingParse is the shared read-trim-parse-or-fallback path used by
// settingInt + settingBool. Extracted to kill the dupl clone the file
// accumulated as more typed accessors were added.
func settingParse[T any](ctx context.Context, s Reader, key string, fallback T, parse func(string) (T, error)) T {
	v, err := s.Get(ctx, key)
	if err != nil || strings.TrimSpace(v) == "" {
		return fallback
	}
	n, err := parse(strings.TrimSpace(v))
	if err != nil {
		return fallback
	}
	return n
}

func settingIntRange(ctx context.Context, s Reader, key string, fallback, min, max, defaultValue int) int {
	value := settingInt(ctx, s, key, fallback)
	if value < min || value > max {
		return defaultValue
	}
	return value
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
	return settingParse(ctx, s, key, fallback, strconv.ParseBool)
}

