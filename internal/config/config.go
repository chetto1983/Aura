package config

import (
	"strings"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	TelegramToken    string   `envconfig:"TELEGRAM_TOKEN" required:"true"`
	Allowlist        []string `envconfig:"TELEGRAM_ALLOWLIST" required:"true"`
	MaxContextTokens int      `envconfig:"MAX_CONTEXT_TOKENS" default:"4000"`
	SoftBudget       float64 `envconfig:"SOFT_BUDGET" default:"10.0"`
	HardBudget       float64 `envconfig:"HARD_BUDGET" default:"20.0"`
	LogLevel         string   `envconfig:"LOG_LEVEL" default:"info"`
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

	return cfg, nil
}