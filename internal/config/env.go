package config

import (
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getSecretEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	if path := os.Getenv(key + "_FILE"); path != "" {
		if raw, err := os.ReadFile(path); err == nil {
			if v := strings.TrimSpace(string(raw)); v != "" {
				return v
			}
		}
	}
	return fallback
}

func getEnvInt(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func getEnvFloat(key string, fallback float64) float64 {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return fallback
	}
	return f
}

func getEnvBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// getEnvDuration reads a Go duration string (e.g. "30s", "5m") from the named
// env var and returns the parsed value. Returns fallback on missing or invalid
// input. Mirrors the existing getEnvInt / getEnvBool / getEnvFloat helpers in
// env.go which all use bare os.Getenv (no TrimSpace) — trailing whitespace in
// duration env vars yields a ParseDuration error and falls back, which is fine.
//
// WR-02: When the operator supplies a value that ParseDuration rejects or a
// negative duration, log a warning so the misconfiguration is visible at
// startup instead of silently falling through to the default.
func getEnvDuration(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		slog.Default().Warn("config: invalid duration env var; using default",
			"key", key, "value", v, "error", err, "default", fallback)
		return fallback
	}
	if d < 0 {
		slog.Default().Warn("config: negative duration env var rejected; using default",
			"key", key, "value", v, "default", fallback)
		return fallback
	}
	return d
}
