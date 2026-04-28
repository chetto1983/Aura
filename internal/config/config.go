package config

import (
	"strings"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	TelegramToken    string   `envconfig:"TELEGRAM_TOKEN" required:"true"`
	Allowlist        []string `envconfig:"TELEGRAM_ALLOWLIST" required:"true"`
	MaxContextTokens int      `envconfig:"MAX_CONTEXT_TOKENS" default:"4000"`
	SoftBudget       float64  `envconfig:"SOFT_BUDGET" default:"10.0"`
	HardBudget       float64  `envconfig:"HARD_BUDGET" default:"20.0"`
	LogLevel         string   `envconfig:"LOG_LEVEL" default:"info"`
	LLMAPIKey        string   `envconfig:"LLM_API_KEY"`
	LLMBaseURL       string   `envconfig:"LLM_BASE_URL"`
	LLMModel         string   `envconfig:"LLM_MODEL"`
	LLMMaxRetries    int      `envconfig:"LLM_MAX_RETRIES" default:"5"`
	WikiPath         string   `envconfig:"WIKI_PATH" default:"./wiki"`
	EmbeddingAPIKey  string   `envconfig:"EMBEDDING_API_KEY"`
	EmbeddingBaseURL string   `envconfig:"EMBEDDING_BASE_URL"`
	EmbeddingModel   string   `envconfig:"EMBEDDING_MODEL" default:"text-embedding-3-small"`
	PgConnString     string   `envconfig:"PG_CONN_STRING"`
}

// IsAllowlisted checks if a Telegram user ID is in the allowlist.
func (c *Config) IsAllowlisted(userID string) bool {
	for _, id := range c.Allowlist {
		if id == userID {
			return true
		}
	}
	return false
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
	if allowlistStr == "" {
		return nil, errMissing("TELEGRAM_ALLOWLIST")
	}
	cfg.Allowlist = strings.Split(allowlistStr, ",")

	cfg.MaxContextTokens = getEnvInt("MAX_CONTEXT_TOKENS", 4000)
	cfg.SoftBudget = getEnvFloat("SOFT_BUDGET", 10.0)
	cfg.HardBudget = getEnvFloat("HARD_BUDGET", 20.0)
	cfg.LogLevel = getEnv("LOG_LEVEL", "info")

	cfg.LLMAPIKey = getEnv("LLM_API_KEY", "")
	cfg.LLMBaseURL = getEnv("LLM_BASE_URL", "https://api.openai.com/v1")
	cfg.LLMModel = getEnv("LLM_MODEL", "gpt-4")
	cfg.LLMMaxRetries = getEnvInt("LLM_MAX_RETRIES", 5)

	cfg.WikiPath = getEnv("WIKI_PATH", "./wiki")

	cfg.EmbeddingAPIKey = getEnv("EMBEDDING_API_KEY", "")
	cfg.EmbeddingBaseURL = getEnv("EMBEDDING_BASE_URL", "https://api.openai.com/v1")
	cfg.EmbeddingModel = getEnv("EMBEDDING_MODEL", "text-embedding-3-small")
	cfg.PgConnString = getEnv("PG_CONN_STRING", "")

	return cfg, nil
}
