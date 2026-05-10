package config

import (
	"slices"
	"strings"
	"time"
)

const DefaultSearXNGBaseURL = "http://127.0.0.1:8088"
const DefaultQdrantCollection = "aura_memory_v1"
const DefaultSpeculativeSearchTimeoutMS = 1500
const DefaultMemorySearchTimeoutMS = 5000
const DefaultAuraBotTimeoutSec = 300
const DefaultSandboxRuntimeDir = "./runtime/pyodide"
const DefaultSandboxRuntimeMode = "auto"
const DefaultSandboxTimeoutSec = 120
const DefaultSkillRoutingMode = "manifest"
const DefaultAgentLoopMaxSteps = 8
const DefaultTerminalToolPolicy = "on"
const DefaultDelegationMode = "fast"
const DefaultTraceRetentionDays = 30
const DefaultWorkspaceTools = "enabled"
const DefaultWorkspaceRoot = "."
const DefaultRuntimeWorkspacePath = "./runtime-workspace"
const DefaultToolSearchBackend = "fts"
const DefaultToolSearchTopK = 5

// Per-user gate configuration defaults (Phase 1 / CONC-01).
const DefaultInboxSize = 8
const DefaultInboxQueueNoticeAfter = 30 * time.Second
const DefaultInactivityThreshold = 30 * time.Minute
const DefaultInactivitySweepInterval = 60 * time.Second
const (
	DefaultEnvPath = ".env"

	DefaultCostInputPerMTokens  = 0.20
	DefaultCostOutputPerMTokens = 0.80
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	TelegramToken              string   `envconfig:"TELEGRAM_TOKEN" required:"true"`
	Allowlist                  []string `envconfig:"TELEGRAM_ALLOWLIST"`
	AllowlistConfigured        bool
	MaxContextTokens           int     `envconfig:"MAX_CONTEXT_TOKENS" default:"4000"`
	MaxHistoryMessages         int     `envconfig:"MAX_HISTORY_MESSAGES" default:"50"`
	SoftBudget                 float64 `envconfig:"SOFT_BUDGET" default:"10.0"`
	HardBudget                 float64 `envconfig:"HARD_BUDGET" default:"20.0"`
	CostInputPerMTokens        float64 `envconfig:"COST_INPUT_PER_M_TOKENS" default:"0.20"`
	CostOutputPerMTokens       float64 `envconfig:"COST_OUTPUT_PER_M_TOKENS" default:"0.80"`
	LogLevel                   string  `envconfig:"LOG_LEVEL" default:"info"`
	LogDir                     string  `envconfig:"LOG_DIR" default:"./logs"`
	LLMAPIKey                  string  `envconfig:"LLM_API_KEY"`
	LLMBaseURL                 string  `envconfig:"LLM_BASE_URL"`
	LLMModel                   string  `envconfig:"LLM_MODEL"`
	LLMMaxRetries              int     `envconfig:"LLM_MAX_RETRIES" default:"5"`
	WebSearchProvider          string  `envconfig:"WEB_SEARCH_PROVIDER" default:"disabled"`
	SearXNGBaseURL             string  `envconfig:"SEARXNG_BASE_URL"`
	GarageS3Endpoint           string  `envconfig:"GARAGE_S3_ENDPOINT"`
	GarageS3Region             string  `envconfig:"GARAGE_S3_REGION" default:"garage"`
	GarageS3Bucket             string  `envconfig:"GARAGE_S3_BUCKET" default:"aura-artifacts"`
	GarageS3AccessKey          string  `envconfig:"GARAGE_S3_ACCESS_KEY"`
	GarageS3SecretKey          string  `envconfig:"GARAGE_S3_SECRET_KEY"`
	QdrantURL                  string  `envconfig:"QDRANT_URL"`
	QdrantCollection           string  `envconfig:"QDRANT_COLLECTION" default:"aura_memory_v1"`
	QdrantAPIKey               string  `envconfig:"QDRANT_API_KEY"`
	SpeculativeSearchTimeoutMS int     `envconfig:"SPECULATIVE_SEARCH_TIMEOUT_MS" default:"1500"`
	MemorySearchTimeoutMS      int     `envconfig:"MEMORY_SEARCH_TIMEOUT_MS" default:"5000"`
	MaxToolIterations          int     `envconfig:"MAX_TOOL_ITERATIONS" default:"10"`
	ToolSearchBackend          string  `envconfig:"TOOL_SEARCH_BACKEND" default:"fts"`
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
	AuraBotMaxDepth            int     `envconfig:"AURABOT_MAX_DEPTH" default:"1"`
	AuraBotTimeoutSec          int     `envconfig:"AURABOT_TIMEOUT_SEC" default:"300"`
	AuraBotMaxIterations       int     `envconfig:"AURABOT_MAX_ITERATIONS" default:"5"`
	EmbeddingAPIKey            string  `envconfig:"EMBEDDING_API_KEY"`
	EmbeddingBaseURL           string  `envconfig:"EMBEDDING_BASE_URL"`
	EmbeddingModel             string  `envconfig:"EMBEDDING_MODEL" default:"mistral-embed"`
	DBPath                     string  `envconfig:"DB_PATH" default:"./aura.db"`
	HTTPPort                   string  `envconfig:"HTTP_PORT" default:"127.0.0.1:8080"`
	Timezone                   string  `envconfig:"AURA_TIMEZONE"`
	Headless                   bool    `envconfig:"AURA_HEADLESS" default:"false"`
	EnvPath                    string  `envconfig:"AURA_ENV_PATH" default:".env"`
	DashboardTokenTTLHours     int     `envconfig:"DASHBOARD_TOKEN_TTL_HOURS" default:"720"`
	PromptVersion              string  `envconfig:"AURA_PROMPT_VERSION" default:"aura-agent-v1"`
	SkillRoutingMode           string  `envconfig:"AURA_SKILL_ROUTING_MODE" default:"manifest"`
	AgentLoopMaxSteps          int     `envconfig:"AURA_AGENT_LOOP_MAX_STEPS" default:"8"`
	TerminalToolPolicy         string  `envconfig:"AURA_TERMINAL_TOOL_POLICY" default:"on"`
	DelegationMode             string  `envconfig:"AURA_DELEGATION_MODE" default:"fast"`
	TraceRetentionDays         int     `envconfig:"AURA_TRACE_RETENTION_DAYS" default:"30"`
	WorkspaceTools             string  `envconfig:"AURA_WORKSPACE_TOOLS" default:"enabled"`
	WorkspaceRoot              string  `envconfig:"AURA_WORKSPACE_ROOT" default:"."`
	RuntimeWorkspacePath       string  `envconfig:"AURA_RUNTIME_WORKSPACE_PATH" default:"./runtime-workspace"`

	// Mistral Document AI OCR. Keys are kept separate from LLM_API_KEY and
	// EMBEDDING_API_KEY: OCR is a distinct capability with its own billing,
	// and reusing chat/embedding keys would leak quota and access scope.
	MistralAPIKey           string `envconfig:"MISTRAL_API_KEY"`
	MistralOCRModel         string `envconfig:"MISTRAL_OCR_MODEL" default:"mistral-ocr-latest"`
	MistralOCRBaseURL       string `envconfig:"MISTRAL_OCR_BASE_URL" default:"https://api.mistral.ai/v1"`
	MistralOCRTableFormat   string `envconfig:"MISTRAL_OCR_TABLE_FORMAT" default:"markdown"`
	MistralOCRIncludeImages bool   `envconfig:"MISTRAL_OCR_INCLUDE_IMAGES" default:"false"`
	MistralOCRExtractHeader bool   `envconfig:"MISTRAL_OCR_EXTRACT_HEADER" default:"false"`
	MistralOCRExtractFooter bool   `envconfig:"MISTRAL_OCR_EXTRACT_FOOTER" default:"false"`
	OCREnabled              bool   `envconfig:"OCR_ENABLED" default:"true"`
	OCRMaxPages             int    `envconfig:"OCR_MAX_PAGES" default:"500"`
	OCRMaxFileMB            int    `envconfig:"OCR_MAX_FILE_MB" default:"100"`

	// Conversation archive (Phase 12a/12b)
	ConvArchiveEnabled bool `envconfig:"CONV_ARCHIVE_ENABLED" default:"true"`

	// Sandbox code execution. Docker production uses process mode, which runs
	// Python directly inside the Aura container. Pyodide modes remain as legacy
	// local/sidecar adapters until their extraction paths are fully retired.
	SandboxEnabled     bool   `envconfig:"SANDBOX_ENABLED" default:"true"`
	SandboxRuntimeMode string `envconfig:"SANDBOX_RUNTIME_MODE" default:"auto"`
	SandboxRuntimeURL  string `envconfig:"SANDBOX_RUNTIME_URL"`
	SandboxRuntimeDir  string `envconfig:"SANDBOX_RUNTIME_DIR" default:"./runtime/pyodide"`
	SandboxTimeoutSec  int    `envconfig:"SANDBOX_TIMEOUT_SEC" default:"120"`

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

	cfg.MaxContextTokens = getEnvInt("MAX_CONTEXT_TOKENS", 4000)
	cfg.MaxHistoryMessages = getEnvInt("MAX_HISTORY_MESSAGES", 50)
	cfg.SoftBudget = getEnvFloat("SOFT_BUDGET", 10.0)
	cfg.HardBudget = getEnvFloat("HARD_BUDGET", 20.0)
	cfg.CostInputPerMTokens = getEnvFloat("COST_INPUT_PER_M_TOKENS", 0)
	cfg.CostOutputPerMTokens = getEnvFloat("COST_OUTPUT_PER_M_TOKENS", 0)
	if cfg.CostInputPerMTokens <= 0 && cfg.CostOutputPerMTokens <= 0 {
		// Compatibility with the old USD/token knob. Values larger than one cent
		// per token are assumed to be the common "per million token" unit mistake
		// and intentionally fall back to sane defaults.
		if legacy := getEnvFloat("COST_PER_TOKEN", 0); legacy > 0 && legacy <= 0.01 {
			cfg.CostInputPerMTokens = legacy * 1_000_000
			cfg.CostOutputPerMTokens = legacy * 1_000_000
		}
	}
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
	cfg.GarageS3Endpoint = getEnv("GARAGE_S3_ENDPOINT", "")
	cfg.GarageS3Region = getEnv("GARAGE_S3_REGION", "garage")
	cfg.GarageS3Bucket = getEnv("GARAGE_S3_BUCKET", "aura-artifacts")
	cfg.GarageS3AccessKey = getSecretEnv("GARAGE_S3_ACCESS_KEY", "")
	cfg.GarageS3SecretKey = getSecretEnv("GARAGE_S3_SECRET_KEY", "")
	cfg.QdrantURL = getEnv("QDRANT_URL", "")
	cfg.QdrantCollection = getEnv("QDRANT_COLLECTION", DefaultQdrantCollection)
	cfg.QdrantAPIKey = getSecretEnv("QDRANT_API_KEY", "")
	cfg.SpeculativeSearchTimeoutMS = getEnvInt("SPECULATIVE_SEARCH_TIMEOUT_MS", DefaultSpeculativeSearchTimeoutMS)
	cfg.MemorySearchTimeoutMS = getEnvInt("MEMORY_SEARCH_TIMEOUT_MS", DefaultMemorySearchTimeoutMS)
	cfg.MaxToolIterations = getEnvInt("MAX_TOOL_ITERATIONS", 10)
	cfg.ToolSearchBackend = NormalizeToolSearchBackend(getEnv("TOOL_SEARCH_BACKEND", DefaultToolSearchBackend))
	cfg.ToolSearchTopK = normalizeIntRange(getEnvInt("TOOL_SEARCH_TOP_K", DefaultToolSearchTopK), 1, 10, DefaultToolSearchTopK)

	cfg.WikiPath = getEnv("WIKI_PATH", "./runtime-workspace/wiki")
	cfg.PromptOverlayPath = getEnv("PROMPT_OVERLAY_PATH", ".")
	cfg.SkillsPath = getEnv("SKILLS_PATH", "./skills")
	cfg.SkillsInstallProjectDir = getEnv("SKILLS_INSTALL_PROJECT_DIR", "")
	cfg.SkillsCatalogURL = getEnv("SKILLS_CATALOG_URL", "https://skills.sh/")
	cfg.SkillsAdmin = getEnvBool("SKILLS_ADMIN", false)
	cfg.MCPServersPath = getEnv("MCP_SERVERS_PATH", "./mcp.json")
	cfg.AuraBotEnabled = getEnvBool("AURABOT_ENABLED", false)
	cfg.AuraBotMaxActive = getEnvInt("AURABOT_MAX_ACTIVE", 4)
	cfg.AuraBotMaxDepth = getEnvInt("AURABOT_MAX_DEPTH", 1)
	cfg.AuraBotTimeoutSec = getEnvInt("AURABOT_TIMEOUT_SEC", DefaultAuraBotTimeoutSec)
	cfg.AuraBotMaxIterations = getEnvInt("AURABOT_MAX_ITERATIONS", 5)

	cfg.EmbeddingAPIKey = getSecretEnv("EMBEDDING_API_KEY", "")
	cfg.EmbeddingBaseURL = getEnv("EMBEDDING_BASE_URL", "https://api.mistral.ai/v1")
	cfg.EmbeddingModel = getEnv("EMBEDDING_MODEL", "mistral-embed")
	cfg.DBPath = getEnv("DB_PATH", "./aura.db")
	cfg.HTTPPort = getEnv("HTTP_PORT", "127.0.0.1:8080")
	cfg.Timezone = strings.TrimSpace(getEnv("AURA_TIMEZONE", ""))
	cfg.Headless = getEnvBool("AURA_HEADLESS", false)
	cfg.EnvPath = EnvPathFromEnvironment()
	cfg.DashboardTokenTTLHours = getEnvInt("DASHBOARD_TOKEN_TTL_HOURS", 720)
	cfg.PromptVersion = getEnv("AURA_PROMPT_VERSION", "aura-agent-v1")
	cfg.SkillRoutingMode = NormalizeSkillRoutingMode(getEnv("AURA_SKILL_ROUTING_MODE", DefaultSkillRoutingMode))
	cfg.AgentLoopMaxSteps = normalizeIntRange(getEnvInt("AURA_AGENT_LOOP_MAX_STEPS", DefaultAgentLoopMaxSteps), 1, 50, DefaultAgentLoopMaxSteps)
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

	cfg.MistralAPIKey = getSecretEnv("MISTRAL_API_KEY", "")
	cfg.MistralOCRModel = getEnv("MISTRAL_OCR_MODEL", "mistral-ocr-latest")
	cfg.MistralOCRBaseURL = getEnv("MISTRAL_OCR_BASE_URL", "https://api.mistral.ai/v1")
	cfg.MistralOCRTableFormat = getEnv("MISTRAL_OCR_TABLE_FORMAT", "markdown")
	cfg.MistralOCRIncludeImages = getEnvBool("MISTRAL_OCR_INCLUDE_IMAGES", false)
	cfg.MistralOCRExtractHeader = getEnvBool("MISTRAL_OCR_EXTRACT_HEADER", false)
	cfg.MistralOCRExtractFooter = getEnvBool("MISTRAL_OCR_EXTRACT_FOOTER", false)
	cfg.OCREnabled = getEnvBool("OCR_ENABLED", true)
	cfg.OCRMaxPages = getEnvInt("OCR_MAX_PAGES", 500)
	cfg.OCRMaxFileMB = getEnvInt("OCR_MAX_FILE_MB", 100)

	cfg.ConvArchiveEnabled = getEnvBool("CONV_ARCHIVE_ENABLED", true)

	cfg.SandboxEnabled = getEnvBool("SANDBOX_ENABLED", true)
	cfg.SandboxRuntimeMode = strings.ToLower(strings.TrimSpace(getEnv("SANDBOX_RUNTIME_MODE", DefaultSandboxRuntimeMode)))
	cfg.SandboxRuntimeURL = strings.TrimSpace(getEnv("SANDBOX_RUNTIME_URL", ""))
	cfg.SandboxRuntimeDir = getEnv("SANDBOX_RUNTIME_DIR", DefaultSandboxRuntimeDir)
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

func EnvPathFromEnvironment() string {
	return getEnv("AURA_ENV_PATH", DefaultEnvPath)
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
