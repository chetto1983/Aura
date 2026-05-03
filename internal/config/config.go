package config

import (
	"slices"
	"strings"
)

const DefaultOllamaWebBaseURL = "https://ollama.com/api"
const DefaultAuraBotTimeoutSec = 300

// Config holds all application configuration loaded from environment variables.
type Config struct {
	TelegramToken        string   `envconfig:"TELEGRAM_TOKEN" required:"true"`
	Allowlist            []string `envconfig:"TELEGRAM_ALLOWLIST"`
	AllowlistConfigured  bool
	MaxContextTokens     int     `envconfig:"MAX_CONTEXT_TOKENS" default:"4000"`
	MaxHistoryMessages   int     `envconfig:"MAX_HISTORY_MESSAGES" default:"50"`
	SoftBudget           float64 `envconfig:"SOFT_BUDGET" default:"10.0"`
	HardBudget           float64 `envconfig:"HARD_BUDGET" default:"20.0"`
	CostPerToken         float64 `envconfig:"COST_PER_TOKEN" default:"0.00001"`
	LogLevel             string  `envconfig:"LOG_LEVEL" default:"info"`
	LogDir               string  `envconfig:"LOG_DIR" default:"./logs"`
	LLMAPIKey            string  `envconfig:"LLM_API_KEY"`
	LLMBaseURL           string  `envconfig:"LLM_BASE_URL"`
	LLMModel             string  `envconfig:"LLM_MODEL"`
	LLMMaxRetries        int     `envconfig:"LLM_MAX_RETRIES" default:"5"`
	OllamaBaseURL        string  `envconfig:"OLLAMA_BASE_URL"`
	OllamaModel          string  `envconfig:"OLLAMA_MODEL"`
	OllamaAPIKey         string  `envconfig:"OLLAMA_API_KEY"`
	OllamaWebBaseURL     string  `envconfig:"OLLAMA_WEB_BASE_URL"`
	MaxToolIterations    int     `envconfig:"MAX_TOOL_ITERATIONS" default:"10"`
	WikiPath             string  `envconfig:"WIKI_PATH" default:"./wiki"`
	PromptOverlayPath    string  `envconfig:"PROMPT_OVERLAY_PATH" default:"."`
	SkillsPath           string  `envconfig:"SKILLS_PATH" default:"./skills"`
	SkillsCatalogURL     string  `envconfig:"SKILLS_CATALOG_URL" default:"https://skills.sh/"`
	SkillsAdmin          bool    `envconfig:"SKILLS_ADMIN" default:"false"`
	MCPServersPath       string  `envconfig:"MCP_SERVERS_PATH" default:"./mcp.json"`
	AuraBotEnabled       bool    `envconfig:"AURABOT_ENABLED" default:"false"`
	AuraBotMaxActive     int     `envconfig:"AURABOT_MAX_ACTIVE" default:"4"`
	AuraBotMaxDepth      int     `envconfig:"AURABOT_MAX_DEPTH" default:"1"`
	AuraBotTimeoutSec    int     `envconfig:"AURABOT_TIMEOUT_SEC" default:"300"`
	AuraBotMaxIterations int     `envconfig:"AURABOT_MAX_ITERATIONS" default:"5"`
	EmbeddingAPIKey      string  `envconfig:"EMBEDDING_API_KEY"`
	EmbeddingBaseURL     string  `envconfig:"EMBEDDING_BASE_URL"`
	EmbeddingModel       string  `envconfig:"EMBEDDING_MODEL" default:"mistral-embed"`
	DBPath               string  `envconfig:"DB_PATH" default:"./aura.db"`
	HTTPPort             string  `envconfig:"HTTP_PORT" default:"127.0.0.1:8080"`
	OTelEnabled          bool    `envconfig:"OTEL_ENABLED" default:"false"`

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

	// Auto-summarization (Phase 12e+)
	SummarizerEnabled         bool    `envconfig:"SUMMARIZER_ENABLED" default:"true"`
	SummarizerMode            string  `envconfig:"SUMMARIZER_MODE" default:"off"`
	SummarizerTurnInterval    int     `envconfig:"SUMMARIZER_TURN_INTERVAL" default:"5"`
	SummarizerMinSalience     float64 `envconfig:"SUMMARIZER_MIN_SALIENCE" default:"0.5"`
	SummarizerLookbackTurns   int     `envconfig:"SUMMARIZER_LOOKBACK_TURNS" default:"10"`
	SummarizerCooldownSeconds int     `envconfig:"SUMMARIZER_COOLDOWN_SECONDS" default:"60"`
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

	cfg.TelegramToken = getEnv("TELEGRAM_TOKEN", "")

	allowlistStr := getEnv("TELEGRAM_ALLOWLIST", "")
	cfg.Allowlist = parseAllowlist(allowlistStr)
	cfg.AllowlistConfigured = len(cfg.Allowlist) > 0

	cfg.MaxContextTokens = getEnvInt("MAX_CONTEXT_TOKENS", 4000)
	cfg.MaxHistoryMessages = getEnvInt("MAX_HISTORY_MESSAGES", 50)
	cfg.SoftBudget = getEnvFloat("SOFT_BUDGET", 10.0)
	cfg.HardBudget = getEnvFloat("HARD_BUDGET", 20.0)
	cfg.CostPerToken = getEnvFloat("COST_PER_TOKEN", 0.00001)
	cfg.LogLevel = getEnv("LOG_LEVEL", "info")
	cfg.LogDir = getEnv("LOG_DIR", "./logs")

	cfg.LLMAPIKey = getEnv("LLM_API_KEY", "")
	cfg.LLMBaseURL = getEnv("LLM_BASE_URL", "https://api.openai.com/v1")
	cfg.LLMModel = getEnv("LLM_MODEL", "gpt-4")
	cfg.LLMMaxRetries = getEnvInt("LLM_MAX_RETRIES", 5)

	cfg.OllamaBaseURL = getEnv("OLLAMA_BASE_URL", "")
	cfg.OllamaModel = getEnv("OLLAMA_MODEL", "")
	cfg.OllamaAPIKey = getEnv("OLLAMA_API_KEY", "")
	cfg.OllamaWebBaseURL = getEnv("OLLAMA_WEB_BASE_URL", DefaultOllamaWebBaseURL)
	cfg.MaxToolIterations = getEnvInt("MAX_TOOL_ITERATIONS", 10)

	cfg.WikiPath = getEnv("WIKI_PATH", "./wiki")
	cfg.PromptOverlayPath = getEnv("PROMPT_OVERLAY_PATH", ".")
	cfg.SkillsPath = getEnv("SKILLS_PATH", "./skills")
	cfg.SkillsCatalogURL = getEnv("SKILLS_CATALOG_URL", "https://skills.sh/")
	cfg.SkillsAdmin = getEnvBool("SKILLS_ADMIN", false)
	cfg.MCPServersPath = getEnv("MCP_SERVERS_PATH", "./mcp.json")
	cfg.AuraBotEnabled = getEnvBool("AURABOT_ENABLED", false)
	cfg.AuraBotMaxActive = getEnvInt("AURABOT_MAX_ACTIVE", 4)
	cfg.AuraBotMaxDepth = getEnvInt("AURABOT_MAX_DEPTH", 1)
	cfg.AuraBotTimeoutSec = getEnvInt("AURABOT_TIMEOUT_SEC", DefaultAuraBotTimeoutSec)
	cfg.AuraBotMaxIterations = getEnvInt("AURABOT_MAX_ITERATIONS", 5)

	cfg.EmbeddingAPIKey = getEnv("EMBEDDING_API_KEY", "")
	cfg.EmbeddingBaseURL = getEnv("EMBEDDING_BASE_URL", "https://api.mistral.ai/v1")
	cfg.EmbeddingModel = getEnv("EMBEDDING_MODEL", "mistral-embed")
	cfg.DBPath = getEnv("DB_PATH", "./aura.db")
	cfg.HTTPPort = getEnv("HTTP_PORT", "127.0.0.1:8080")
	cfg.OTelEnabled = getEnvBool("OTEL_ENABLED", false)

	cfg.MistralAPIKey = getEnv("MISTRAL_API_KEY", "")
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

	cfg.SummarizerEnabled = getEnvBool("SUMMARIZER_ENABLED", true)
	cfg.SummarizerMode = getEnv("SUMMARIZER_MODE", "off")
	cfg.SummarizerTurnInterval = getEnvInt("SUMMARIZER_TURN_INTERVAL", 5)
	cfg.SummarizerMinSalience = getEnvFloat("SUMMARIZER_MIN_SALIENCE", 0.5)
	cfg.SummarizerLookbackTurns = getEnvInt("SUMMARIZER_LOOKBACK_TURNS", 10)
	cfg.SummarizerCooldownSeconds = getEnvInt("SUMMARIZER_COOLDOWN_SECONDS", 60)

	return cfg, nil
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
