package config

import (
	"slices"
	"strings"
	"time"
)

const DefaultSearXNGBaseURL = "http://127.0.0.1:8088"
const DefaultMarkitdownURL = "http://127.0.0.1:3001"
const DefaultMarkitdownTimeoutSec = 120
const DefaultQdrantCollection = "aura_memory_v1"
const DefaultMemorySearchTimeoutMS = 5000
const DefaultAuraBotTimeoutSec = 300
const DefaultSandboxTimeoutSec = 120
const DefaultSkillRoutingMode = "manifest"
const DefaultAgentLoopMaxSteps = 100

// Capability limits raised in Phase-F (2026-05-15): the agent caps LATENCY
// and COST, not CAPABILITY. Per docs/aura-main-loop-limits-audit.md §3.5
// "cap LATENCY and COST, not CAPABILITY".
const DefaultMaxToolResultChars = 24000
const DefaultMicrocompactKeepRecent = 10
const DefaultMicrocompactMinChars = 2000
const DefaultTerminalToolPolicy = "on"
const DefaultDelegationMode = "fast"
const DefaultTraceRetentionDays = 30
const DefaultWorkspaceTools = "enabled"
const DefaultWorkspaceRoot = "."
const DefaultRuntimeWorkspacePath = "./runtime-workspace"
const DefaultToolSearchBackend = "hybrid"
const DefaultToolSearchTopK = 20
const DefaultOP07NFailThreshold = 2
const DefaultOP07RecentTurns = 10

// Per-user gate configuration defaults (Phase 1 / CONC-01).
const DefaultInboxSize = 8
const DefaultInboxQueueNoticeAfter = 30 * time.Second
const DefaultInactivityThreshold = 30 * time.Minute
const DefaultInactivitySweepInterval = 60 * time.Second
const (
	DefaultCostInputPerMTokens  = 0.20
	DefaultCostOutputPerMTokens = 0.80
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	TelegramToken       string   `envconfig:"TELEGRAM_TOKEN" required:"true"`
	Allowlist           []string `envconfig:"TELEGRAM_ALLOWLIST"`
	AllowlistConfigured bool
	// MaxContextTokens is the LLM OUTPUT cap (passed as llm.Request.MaxTokens).
	// The env var name is historical; see docs/aura-main-loop-limits-audit.md
	// §3.1 for the misname analysis. Rename deferred to a future cleanup story.
	MaxContextTokens               int     `envconfig:"MAX_CONTEXT_TOKENS" default:"16000"`
	MaxHistoryMessages             int     `envconfig:"MAX_HISTORY_MESSAGES" default:"50"`
	SoftBudget                     float64 `envconfig:"SOFT_BUDGET" default:"10.0"`
	HardBudget                     float64 `envconfig:"HARD_BUDGET" default:"20.0"`
	CostInputPerMTokens            float64 `envconfig:"COST_INPUT_PER_M_TOKENS" default:"0.20"`
	CostOutputPerMTokens           float64 `envconfig:"COST_OUTPUT_PER_M_TOKENS" default:"0.80"`
	LogLevel                       string  `envconfig:"LOG_LEVEL" default:"info"`
	LogDir                         string  `envconfig:"LOG_DIR" default:"./logs"`
	LLMAPIKey                      string  `envconfig:"LLM_API_KEY"`
	LLMBaseURL                     string  `envconfig:"LLM_BASE_URL"`
	LLMModel                       string  `envconfig:"LLM_MODEL"`
	LLMMaxRetries                  int     `envconfig:"LLM_MAX_RETRIES" default:"5"`
	WebSearchProvider              string  `envconfig:"WEB_SEARCH_PROVIDER" default:"disabled"`
	SearXNGBaseURL                 string  `envconfig:"SEARXNG_BASE_URL"`
	MarkitdownURL                  string  `envconfig:"MARKITDOWN_URL"`
	MarkitdownTimeoutSec           int     `envconfig:"MARKITDOWN_TIMEOUT_SEC" default:"120"`
	GarageS3Endpoint               string  `envconfig:"GARAGE_S3_ENDPOINT"`
	GarageS3Region                 string  `envconfig:"GARAGE_S3_REGION" default:"garage"`
	GarageS3Bucket                 string  `envconfig:"GARAGE_S3_BUCKET" default:"aura-artifacts"`
	GarageS3AccessKey              string  `envconfig:"GARAGE_S3_ACCESS_KEY"`
	GarageS3SecretKey              string  `envconfig:"GARAGE_S3_SECRET_KEY"`
	QdrantURL                      string  `envconfig:"QDRANT_URL"`
	QdrantCollection               string  `envconfig:"QDRANT_COLLECTION" default:"aura_memory_v1"`
	QdrantAPIKey                   string  `envconfig:"QDRANT_API_KEY"`
	MemorySearchTimeoutMS          int     `envconfig:"MEMORY_SEARCH_TIMEOUT_MS" default:"5000"`
	MemorySearchStaleThresholdSecs int     `envconfig:"MEMORY_SEARCH_STALE_THRESHOLD_SECS" default:"3600"`
	// Phase 02 governance knobs. Defaults match the constants we shipped in
	// agent loop governance and tools/memory_search.go. Operators can
	// shrink MAX_TOOL_RESULT_CHARS to fit smaller context windows, raise
	// the half-life for archive when running a project that wants slower
	// memory aging, etc.
	MaxToolResultChars         int     `envconfig:"MAX_TOOL_RESULT_CHARS" default:"24000"`
	MicrocompactKeepRecent     int     `envconfig:"MICROCOMPACT_KEEP_RECENT" default:"10"`
	MicrocompactMinChars       int     `envconfig:"MICROCOMPACT_MIN_CHARS" default:"2000"`
	RecencyHalfLifeWikiDays    float64 `envconfig:"MEMORY_RECENCY_HALFLIFE_WIKI_DAYS" default:"180"`
	RecencyHalfLifeArchiveDays float64 `envconfig:"MEMORY_RECENCY_HALFLIFE_ARCHIVE_DAYS" default:"30"`
	ToolSearchBackend          string  `envconfig:"TOOL_SEARCH_BACKEND" default:"hybrid"`
	ToolSearchTopK             int     `envconfig:"TOOL_SEARCH_TOP_K" default:"5"`
	WikiPath                   string  `envconfig:"WIKI_PATH" default:"./runtime-workspace/wiki"`
	PromptOverlayPath          string  `envconfig:"PROMPT_OVERLAY_PATH" default:"."`
	SkillsPath                 string  `envconfig:"SKILLS_PATH" default:"./skills"`
	SkillsInstallProjectDir    string  `envconfig:"SKILLS_INSTALL_PROJECT_DIR"`
	SkillsCatalogURL           string  `envconfig:"SKILLS_CATALOG_URL" default:"https://skills.sh/"`
	SkillsAdmin                bool    `envconfig:"SKILLS_ADMIN" default:"false"`
	MCPServersPath             string  `envconfig:"MCP_SERVERS_PATH" default:"./mcp.json"`
	AuraBotEnabled             bool    `envconfig:"AURABOT_ENABLED" default:"false"`
	AuraBotMaxActive           int     `envconfig:"AURABOT_MAX_ACTIVE" default:"4"`
	AuraBotMaxDepth            int     `envconfig:"AURABOT_MAX_DEPTH" default:"3"`
	AuraBotTimeoutSec          int     `envconfig:"AURABOT_TIMEOUT_SEC" default:"300"`
	AuraBotMaxIterations       int     `envconfig:"AURABOT_MAX_ITERATIONS" default:"100"`
	EmbeddingAPIKey            string  `envconfig:"EMBEDDING_API_KEY"`
	EmbeddingBaseURL           string  `envconfig:"EMBEDDING_BASE_URL"`
	EmbeddingModel             string  `envconfig:"EMBEDDING_MODEL" default:"embeddinggemma"`
	// EmbeddingOutputDim activates Matryoshka Representation Learning
	// truncation on the embedding response. 0 returns the model's native
	// dim (e.g. 768 for embeddinggemma-300m). Setting to 256 or 128 (only
	// meaningful for MRL-trained models) trades a tiny MTEB delta for
	// 3x-6x smaller Qdrant vectors and cosine compute. Aura targets 256
	// in production.
	EmbeddingOutputDim     int    `envconfig:"EMBEDDING_OUTPUT_DIM" default:"0"`
	DBPath                 string `envconfig:"DB_PATH" default:"./aura.db"`
	HTTPPort               string `envconfig:"HTTP_PORT" default:"127.0.0.1:8080"`
	Timezone               string `envconfig:"AURA_TIMEZONE"`
	Headless               bool   `envconfig:"AURA_HEADLESS" default:"false"`
	DashboardTokenTTLHours int    `envconfig:"DASHBOARD_TOKEN_TTL_HOURS" default:"720"`
	PromptVersion          string `envconfig:"AURA_PROMPT_VERSION" default:"aura-agent-v1"`
	SkillRoutingMode       string `envconfig:"AURA_SKILL_ROUTING_MODE" default:"manifest"`
	AgentLoopMaxSteps      int    `envconfig:"AURA_AGENT_LOOP_MAX_STEPS" default:"100"`
	// ReasoningEffort drives the provider-side chain-of-thought field.
	// Accepted values: "", "none", "minimal", "low", "medium", "high",
	// "xhigh", "true"/"enabled". Empty means "do not emit any reasoning
	// field" — matches default OpenAI gpt-4o, vanilla fakes, etc.
	// DeepSeek V4 Flash via OpenRouter accepts "high" or "xhigh"; OpenAI
	// gpt-5/o-series accepts the full set per model.
	ReasoningEffort             string `envconfig:"AURA_REASONING_EFFORT" default:""`
	TerminalToolPolicy          string `envconfig:"AURA_TERMINAL_TOOL_POLICY" default:"on"`
	DelegationMode              string `envconfig:"AURA_DELEGATION_MODE" default:"fast"`
	TraceRetentionDays          int    `envconfig:"AURA_TRACE_RETENTION_DAYS" default:"30"`
	WorkspaceTools              string `envconfig:"AURA_WORKSPACE_TOOLS" default:"enabled"`
	WorkspaceRoot               string `envconfig:"AURA_WORKSPACE_ROOT" default:"."`
	RuntimeWorkspacePath        string `envconfig:"AURA_RUNTIME_WORKSPACE_PATH" default:"./runtime-workspace"`
	OP07HeuristicEnabled        bool   `envconfig:"AURA_OP07_HEURISTIC_ENABLED" default:"false"`
	OP07NFailThreshold          int    `envconfig:"AURA_OP07_NFAIL_THRESHOLD" default:"2"`
	OP07RecentTurns             int    `envconfig:"AURA_OP07_RECENT_TURNS" default:"10"`
	MemoryJudgeEnabled          bool   `envconfig:"AURA_MEMORY_JUDGE_ENABLED" default:"false"`
	OP12PrecallValidatorEnabled bool   `envconfig:"AURA_OP12_PRECALL_VALIDATOR_ENABLED" default:"false"`
	OP12RetryHintEnabled        bool   `envconfig:"AURA_OP12B_RETRY_HINT_ENABLED" default:"false"`

	// Mistral Document AI OCR. Keys are kept separate from LLM_API_KEY and
	// EMBEDDING_API_KEY: OCR is a distinct capability with its own billing,
	// and reusing chat/embedding keys would leak quota and access scope.
	MistralAPIKey           string `envconfig:"MISTRAL_API_KEY"`
	MistralOCRModel         string `envconfig:"MISTRAL_OCR_MODEL" default:"mistral-ocr-latest"`
	MistralOCRBaseURL       string `envconfig:"MISTRAL_OCR_BASE_URL" default:"https://api.mistral.ai/v1"`
	MistralOCRTableFormat   string `envconfig:"MISTRAL_OCR_TABLE_FORMAT" default:"markdown"`
	MistralOCRExtractHeader bool   `envconfig:"MISTRAL_OCR_EXTRACT_HEADER" default:"false"`
	MistralOCRExtractFooter bool   `envconfig:"MISTRAL_OCR_EXTRACT_FOOTER" default:"false"`
	OCRMaxPages             int    `envconfig:"OCR_MAX_PAGES" default:"500"`
	OCRMaxFileMB            int    `envconfig:"OCR_MAX_FILE_MB" default:"100"`

	// Whisper speech-to-text sidecar (Phase-MM, Wave 2).
	// Points at the aura-whisper container's /inference endpoint.
	WhisperBaseURL    string `envconfig:"WHISPER_BASE_URL" default:"http://aura-whisper:8082"`
	WhisperLanguage   string `envconfig:"WHISPER_LANGUAGE" default:"it"`
	WhisperTimeoutSec int    `envconfig:"WHISPER_TIMEOUT_SEC" default:"60"`

	// TokenJuice rule-driven output compaction (Phase-TJ). Enabled by default after
	// US-TJ07 confirmed 15.8% heavy-turn savings + 0 regressions. Set
	// AURA_TOKENJUICE_ENABLED=false to disable without a code change.
	TokenJuiceEnabled bool `envconfig:"AURA_TOKENJUICE_ENABLED" default:"true"`

	// Conversation archive (Phase 12a/12b)
	ConvArchiveEnabled bool `envconfig:"CONV_ARCHIVE_ENABLED" default:"true"`

	// Sandbox code execution. Runs Python directly inside the Aura container.
	SandboxEnabled    bool `envconfig:"SANDBOX_ENABLED" default:"true"`
	SandboxTimeoutSec int  `envconfig:"SANDBOX_TIMEOUT_SEC" default:"120"`

	// Per-user gate configuration (Phase 1 / CONC-01). All four are environment-tunable.
	// W5: NO `default:"..."` tags here -- defaults come from Default* constants applied
	// inside Load() via getEnvInt / getEnvDuration. This matches the existing convention
	// for duration/int fields like SandboxTimeoutSec (envconfig tag without default; Default*
	// constant + getEnvInt does the work in Load). Avoids double-defaulting between the
	// envconfig struct decoder and the explicit getEnv* helpers.
	InboxSize               int           `envconfig:"AURA_INBOX_SIZE"`
	InboxQueueNoticeAfter   time.Duration `envconfig:"AURA_INBOX_QUEUE_NOTICE_AFTER"`
	InactivityThreshold     time.Duration `envconfig:"AURA_INACTIVITY_THRESHOLD"`
	InactivitySweepInterval time.Duration `envconfig:"AURA_INACTIVITY_SWEEP_INTERVAL"`
}

// IsAllowlisted checks if a Telegram user ID is in the allowlist.
func (c *Config) IsAllowlisted(userID string) bool {
	return slices.Contains(c.Allowlist, strings.TrimSpace(userID))
}

// AddToAllowlist adds a user ID to the allowlist if not already present.
func (c *Config) AddToAllowlist(userID string) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return
	}
	if c.IsAllowlisted(userID) {
		return
	}
	c.Allowlist = append(c.Allowlist, userID)
	c.AllowlistConfigured = true
}

// IsBootstrapped reports whether enough config exists for the bot to run.
// Returns false on a fresh install (blank TelegramToken), which the
// startup path uses to invoke the first-run setup wizard. The LLM key is
// not required: the bot still starts in echo mode and the user can chat
// with it for setup feedback even before configuring an LLM.
func (c *Config) IsBootstrapped() bool {
	return strings.TrimSpace(c.TelegramToken) != ""
}

// Load reads configuration from environment variables using envconfig.
//
// Slice 14b: TelegramToken is no longer a hard requirement. When it's
// blank, the caller (cmd/aura/main.go) is expected to launch the
// first-run setup wizard, which mints the token and writes it to .env
// before re-loading. Callers that need the token populated should check
// (*Config).IsBootstrapped() after Load.
func Load() (*Config, error) {
	cfg := &Config{}

	cfg.TelegramToken = getSecretEnv("TELEGRAM_TOKEN", "")

	allowlistStr := getEnv("TELEGRAM_ALLOWLIST", "")
	cfg.Allowlist = parseAllowlist(allowlistStr)
	cfg.AllowlistConfigured = len(cfg.Allowlist) > 0

	cfg.MaxContextTokens = getEnvInt("MAX_CONTEXT_TOKENS", 16000)
	cfg.MaxHistoryMessages = getEnvInt("MAX_HISTORY_MESSAGES", 50)
	cfg.SoftBudget = getEnvFloat("SOFT_BUDGET", 10.0)
	cfg.HardBudget = getEnvFloat("HARD_BUDGET", 20.0)
	cfg.CostInputPerMTokens = getEnvFloat("COST_INPUT_PER_M_TOKENS", 0)
	cfg.CostOutputPerMTokens = getEnvFloat("COST_OUTPUT_PER_M_TOKENS", 0)
	if cfg.CostInputPerMTokens <= 0 {
		cfg.CostInputPerMTokens = DefaultCostInputPerMTokens
	}
	if cfg.CostOutputPerMTokens <= 0 {
		cfg.CostOutputPerMTokens = DefaultCostOutputPerMTokens
	}
	cfg.LogLevel = getEnv("LOG_LEVEL", "info")
	cfg.LogDir = getEnv("LOG_DIR", "./logs")

	cfg.LLMAPIKey = getSecretEnv("LLM_API_KEY", "")
	cfg.LLMBaseURL = getEnv("LLM_BASE_URL", "https://api.openai.com/v1")
	cfg.LLMModel = getEnv("LLM_MODEL", "gpt-4")
	cfg.LLMMaxRetries = getEnvInt("LLM_MAX_RETRIES", 5)

	cfg.WebSearchProvider = strings.ToLower(strings.TrimSpace(getEnv("WEB_SEARCH_PROVIDER", "disabled")))
	cfg.SearXNGBaseURL = getEnv("SEARXNG_BASE_URL", DefaultSearXNGBaseURL)
	cfg.MarkitdownURL = strings.TrimSpace(getEnv("MARKITDOWN_URL", DefaultMarkitdownURL))
	cfg.MarkitdownTimeoutSec = getEnvInt("MARKITDOWN_TIMEOUT_SEC", DefaultMarkitdownTimeoutSec)
	cfg.GarageS3Endpoint = getEnv("GARAGE_S3_ENDPOINT", "")
	cfg.GarageS3Region = getEnv("GARAGE_S3_REGION", "garage")
	cfg.GarageS3Bucket = getEnv("GARAGE_S3_BUCKET", "aura-artifacts")
	cfg.GarageS3AccessKey = getSecretEnv("GARAGE_S3_ACCESS_KEY", "")
	cfg.GarageS3SecretKey = getSecretEnv("GARAGE_S3_SECRET_KEY", "")
	cfg.QdrantURL = getEnv("QDRANT_URL", "")
	cfg.QdrantCollection = getEnv("QDRANT_COLLECTION", DefaultQdrantCollection)
	cfg.QdrantAPIKey = getSecretEnv("QDRANT_API_KEY", "")
	cfg.MemorySearchTimeoutMS = getEnvInt("MEMORY_SEARCH_TIMEOUT_MS", DefaultMemorySearchTimeoutMS)
	cfg.MemorySearchStaleThresholdSecs = getEnvInt("MEMORY_SEARCH_STALE_THRESHOLD_SECS", 3600)
	cfg.ToolSearchBackend = NormalizeToolSearchBackend(getEnv("TOOL_SEARCH_BACKEND", DefaultToolSearchBackend))
	cfg.ToolSearchTopK = normalizeIntRange(getEnvInt("TOOL_SEARCH_TOP_K", DefaultToolSearchTopK), 1, 50, DefaultToolSearchTopK)

	cfg.WikiPath = getEnv("WIKI_PATH", "./runtime-workspace/wiki")
	cfg.PromptOverlayPath = getEnv("PROMPT_OVERLAY_PATH", ".")
	cfg.SkillsPath = getEnv("SKILLS_PATH", "./skills")
	cfg.SkillsInstallProjectDir = getEnv("SKILLS_INSTALL_PROJECT_DIR", "")
	cfg.SkillsCatalogURL = getEnv("SKILLS_CATALOG_URL", "https://skills.sh/")
	cfg.SkillsAdmin = getEnvBool("SKILLS_ADMIN", false)
	cfg.MCPServersPath = getEnv("MCP_SERVERS_PATH", "./mcp.json")
	cfg.AuraBotEnabled = getEnvBool("AURABOT_ENABLED", false)
	cfg.AuraBotMaxActive = getEnvInt("AURABOT_MAX_ACTIVE", 4)
	cfg.AuraBotMaxDepth = getEnvInt("AURABOT_MAX_DEPTH", 3)
	cfg.AuraBotTimeoutSec = getEnvInt("AURABOT_TIMEOUT_SEC", DefaultAuraBotTimeoutSec)
	cfg.AuraBotMaxIterations = getEnvInt("AURABOT_MAX_ITERATIONS", 100)

	cfg.EmbeddingAPIKey = getSecretEnv("EMBEDDING_API_KEY", "no-key")
	cfg.EmbeddingBaseURL = getEnv("EMBEDDING_BASE_URL", "http://aura-llama-embed:8080/v1")
	cfg.EmbeddingModel = getEnv("EMBEDDING_MODEL", "embeddinggemma")
	// 0 keeps native dim. 256 is Aura's locked-in production target via MRL.
	cfg.EmbeddingOutputDim = normalizeIntRange(getEnvInt("EMBEDDING_OUTPUT_DIM", 0), 0, 768, 0)
	cfg.DBPath = getEnv("DB_PATH", "./aura.db")
	cfg.HTTPPort = getEnv("HTTP_PORT", "127.0.0.1:8080")
	cfg.Timezone = strings.TrimSpace(getEnv("AURA_TIMEZONE", ""))
	cfg.Headless = getEnvBool("AURA_HEADLESS", false)
	cfg.DashboardTokenTTLHours = getEnvInt("DASHBOARD_TOKEN_TTL_HOURS", 720)
	cfg.PromptVersion = getEnv("AURA_PROMPT_VERSION", "aura-agent-v1")
	cfg.SkillRoutingMode = NormalizeSkillRoutingMode(getEnv("AURA_SKILL_ROUTING_MODE", DefaultSkillRoutingMode))
	cfg.AgentLoopMaxSteps = normalizeIntRange(getEnvInt("AURA_AGENT_LOOP_MAX_STEPS", DefaultAgentLoopMaxSteps), 1, 10000, DefaultAgentLoopMaxSteps)
	cfg.ReasoningEffort = NormalizeReasoningEffort(getEnv("AURA_REASONING_EFFORT", ""))
	cfg.TerminalToolPolicy = NormalizeTerminalToolPolicy(getEnv("AURA_TERMINAL_TOOL_POLICY", DefaultTerminalToolPolicy))
	cfg.DelegationMode = NormalizeDelegationMode(getEnv("AURA_DELEGATION_MODE", DefaultDelegationMode))
	cfg.TraceRetentionDays = normalizeIntRange(getEnvInt("AURA_TRACE_RETENTION_DAYS", DefaultTraceRetentionDays), 1, 365, DefaultTraceRetentionDays)
	cfg.WorkspaceTools = NormalizeWorkspaceTools(getEnv("AURA_WORKSPACE_TOOLS", DefaultWorkspaceTools))
	cfg.WorkspaceRoot = strings.TrimSpace(getEnv("AURA_WORKSPACE_ROOT", DefaultWorkspaceRoot))
	if cfg.WorkspaceRoot == "" {
		cfg.WorkspaceRoot = DefaultWorkspaceRoot
	}
	cfg.RuntimeWorkspacePath = strings.TrimSpace(getEnv("AURA_RUNTIME_WORKSPACE_PATH", DefaultRuntimeWorkspacePath))
	if cfg.RuntimeWorkspacePath == "" {
		cfg.RuntimeWorkspacePath = DefaultRuntimeWorkspacePath
	}
	cfg.OP07HeuristicEnabled = getEnvBool("AURA_OP07_HEURISTIC_ENABLED", false)
	cfg.OP07NFailThreshold = normalizeIntRange(getEnvInt("AURA_OP07_NFAIL_THRESHOLD", DefaultOP07NFailThreshold), 1, 20, DefaultOP07NFailThreshold)
	cfg.OP07RecentTurns = normalizeIntRange(getEnvInt("AURA_OP07_RECENT_TURNS", DefaultOP07RecentTurns), 1, 100, DefaultOP07RecentTurns)
	cfg.MemoryJudgeEnabled = getEnvBool("AURA_MEMORY_JUDGE_ENABLED", false)
	cfg.OP12PrecallValidatorEnabled = getEnvBool("AURA_OP12_PRECALL_VALIDATOR_ENABLED", false)
	cfg.OP12RetryHintEnabled = getEnvBool("AURA_OP12B_RETRY_HINT_ENABLED", false)

	cfg.MistralAPIKey = getSecretEnv("MISTRAL_API_KEY", "")
	cfg.MistralOCRModel = getEnv("MISTRAL_OCR_MODEL", "mistral-ocr-latest")
	cfg.MistralOCRBaseURL = getEnv("MISTRAL_OCR_BASE_URL", "https://api.mistral.ai/v1")
	cfg.MistralOCRTableFormat = getEnv("MISTRAL_OCR_TABLE_FORMAT", "markdown")
	cfg.MistralOCRExtractHeader = getEnvBool("MISTRAL_OCR_EXTRACT_HEADER", false)
	cfg.MistralOCRExtractFooter = getEnvBool("MISTRAL_OCR_EXTRACT_FOOTER", false)
	cfg.OCRMaxPages = getEnvInt("OCR_MAX_PAGES", 500)
	cfg.OCRMaxFileMB = getEnvInt("OCR_MAX_FILE_MB", 100)

	cfg.WhisperBaseURL = getEnv("WHISPER_BASE_URL", "http://aura-whisper:8082")
	cfg.WhisperLanguage = getEnv("WHISPER_LANGUAGE", "it")
	cfg.WhisperTimeoutSec = getEnvInt("WHISPER_TIMEOUT_SEC", 60)

	cfg.TokenJuiceEnabled = getEnvBool("AURA_TOKENJUICE_ENABLED", true)

	cfg.ConvArchiveEnabled = getEnvBool("CONV_ARCHIVE_ENABLED", true)

	cfg.SandboxEnabled = getEnvBool("SANDBOX_ENABLED", true)
	cfg.SandboxTimeoutSec = getEnvInt("SANDBOX_TIMEOUT_SEC", DefaultSandboxTimeoutSec)

	// Per-user gate configuration (Phase 1 / CONC-01).
	cfg.InboxSize = getEnvInt("AURA_INBOX_SIZE", DefaultInboxSize)
	if cfg.InboxSize <= 0 {
		cfg.InboxSize = DefaultInboxSize
	}
	cfg.InboxQueueNoticeAfter = getEnvDuration("AURA_INBOX_QUEUE_NOTICE_AFTER", DefaultInboxQueueNoticeAfter)
	cfg.InactivityThreshold = getEnvDuration("AURA_INACTIVITY_THRESHOLD", DefaultInactivityThreshold)
	cfg.InactivitySweepInterval = getEnvDuration("AURA_INACTIVITY_SWEEP_INTERVAL", DefaultInactivitySweepInterval)

	return cfg, nil
}

// Location returns Aura's effective wall-clock location for scheduling and
// prompts. Blank AURA_TIMEZONE keeps the process local timezone for desktop
// runs; set an IANA name like Europe/Rome for containers or services that
// otherwise start in UTC.
func (c *Config) Location() (*time.Location, error) {
	if c == nil || strings.TrimSpace(c.Timezone) == "" {
		return time.Local, nil
	}
	loc, err := time.LoadLocation(strings.TrimSpace(c.Timezone))
	if err != nil {
		return nil, err
	}
	return loc, nil
}

func NormalizeSkillRoutingMode(value string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "manifest", "manifest_llm_review":
		return normalized
	default:
		return DefaultSkillRoutingMode
	}
}

func NormalizeTerminalToolPolicy(value string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "on", "enabled", "true", "1", "yes":
		return "on"
	case "toolset":
		return "on"
	case "off", "disabled", "false", "0", "no":
		return "off"
	default:
		return DefaultTerminalToolPolicy
	}
}

// NormalizeReasoningEffort canonicalizes the AURA_REASONING_EFFORT knob.
// Empty / "none" / "off" disable the field entirely. Boolean-ish values
// ("true", "on", "enabled", "yes") map to "enabled" — emitted as
// reasoning.enabled=true on the wire. Explicit depth strings pass through
// lowercased so future provider values (e.g. a new "ultra") are forwarded
// verbatim without a code change here.
func NormalizeReasoningEffort(value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	switch v {
	case "", "none", "off", "disabled", "false", "0", "no":
		return ""
	case "true", "on", "enabled", "yes", "1":
		return "enabled"
	}
	return v
}

func NormalizeDelegationMode(value string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "fast", "bounded", "async":
		return normalized
	default:
		return DefaultDelegationMode
	}
}

func NormalizeToolSearchBackend(value string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "fts", "vector", "hybrid":
		return normalized
	default:
		return DefaultToolSearchBackend
	}
}

func NormalizeWorkspaceTools(value string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(value)); normalized {
	case "enabled", "enable", "true", "1", "on", "yes":
		return "enabled"
	case "disabled", "disable", "false", "0", "off", "no", "":
		return DefaultWorkspaceTools
	default:
		return DefaultWorkspaceTools
	}
}

func normalizeIntRange(value, min, max, fallback int) int {
	if value < min || value > max {
		return fallback
	}
	return value
}

func parseAllowlist(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	allowlist := make([]string, 0, len(parts))
	for _, part := range parts {
		if userID := strings.TrimSpace(part); userID != "" {
			allowlist = append(allowlist, userID)
		}
	}
	return allowlist
}
