package config

import (
	"slices"
	"strings"
)

const DefaultOllamaWebBaseURL = "https://ollama.com/api"

// Config holds all application configuration loaded from environment variables.
type Config struct {
	TelegramToken       string   `envconfig:"TELEGRAM_TOKEN" required:"true"`
	Allowlist           []string `envconfig:"TELEGRAM_ALLOWLIST"`
	AllowlistConfigured bool
	MaxContextTokens    int     `envconfig:"MAX_CONTEXT_TOKENS" default:"4000"`
	SoftBudget          float64 `envconfig:"SOFT_BUDGET" default:"10.0"`
	HardBudget          float64 `envconfig:"HARD_BUDGET" default:"20.0"`
	CostPerToken        float64 `envconfig:"COST_PER_TOKEN" default:"0.00001"`
	LogLevel            string  `envconfig:"LOG_LEVEL" default:"info"`
	LLMAPIKey           string  `envconfig:"LLM_API_KEY"`
	LLMBaseURL          string  `envconfig:"LLM_BASE_URL"`
	LLMModel            string  `envconfig:"LLM_MODEL"`
	LLMMaxRetries       int     `envconfig:"LLM_MAX_RETRIES" default:"5"`
	OllamaBaseURL       string  `envconfig:"OLLAMA_BASE_URL"`
	OllamaModel         string  `envconfig:"OLLAMA_MODEL"`
	OllamaAPIKey        string  `envconfig:"OLLAMA_API_KEY"`
	OllamaWebBaseURL    string  `envconfig:"OLLAMA_WEB_BASE_URL"`
	MaxToolIterations   int     `envconfig:"MAX_TOOL_ITERATIONS" default:"10"`
	WikiPath            string  `envconfig:"WIKI_PATH" default:"./wiki"`
	SkillsPath          string  `envconfig:"SKILLS_PATH" default:"./skills"`
	SkillsCatalogURL    string  `envconfig:"SKILLS_CATALOG_URL" default:"https://skills.sh/"`
	MCPServersPath      string  `envconfig:"MCP_SERVERS_PATH" default:"./mcp.json"`
	EmbeddingAPIKey     string  `envconfig:"EMBEDDING_API_KEY"`
	EmbeddingBaseURL    string  `envconfig:"EMBEDDING_BASE_URL"`
	EmbeddingModel      string  `envconfig:"EMBEDDING_MODEL" default:"mistral-embed"`
	DBPath              string  `envconfig:"DB_PATH" default:"./aura.db"`
	HTTPPort            string  `envconfig:"HTTP_PORT" default:"127.0.0.1:8080"`
	OTelEnabled         bool    `envconfig:"OTEL_ENABLED" default:"false"`

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

// Load reads configuration from environment variables using envconfig.
func Load() (*Config, error) {
	cfg := &Config{}

	token := getEnv("TELEGRAM_TOKEN", "")
	if token == "" {
		return nil, errMissing("TELEGRAM_TOKEN")
	}
	cfg.TelegramToken = token

	allowlistStr := getEnv("TELEGRAM_ALLOWLIST", "")
	cfg.Allowlist = parseAllowlist(allowlistStr)
	cfg.AllowlistConfigured = len(cfg.Allowlist) > 0

	cfg.MaxContextTokens = getEnvInt("MAX_CONTEXT_TOKENS", 4000)
	cfg.SoftBudget = getEnvFloat("SOFT_BUDGET", 10.0)
	cfg.HardBudget = getEnvFloat("HARD_BUDGET", 20.0)
	cfg.CostPerToken = getEnvFloat("COST_PER_TOKEN", 0.00001)
	cfg.LogLevel = getEnv("LOG_LEVEL", "info")

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
	cfg.SkillsPath = getEnv("SKILLS_PATH", "./skills")
	cfg.SkillsCatalogURL = getEnv("SKILLS_CATALOG_URL", "https://skills.sh/")
	cfg.MCPServersPath = getEnv("MCP_SERVERS_PATH", "./mcp.json")

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
