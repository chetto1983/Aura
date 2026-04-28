package config

import (
	"os"
	"testing"
)

func TestIsAllowlisted(t *testing.T) {
	cfg := &Config{
		Allowlist: []string{"123456", "789012"},
	}

	tests := []struct {
		userID string
		want   bool
	}{
		{"123456", true},
		{"789012", true},
		{"999999", false},
		{"", false},
	}

	for _, tt := range tests {
		got := cfg.IsAllowlisted(tt.userID)
		if got != tt.want {
			t.Errorf("IsAllowlisted(%q) = %v, want %v", tt.userID, got, tt.want)
		}
	}
}

func TestLoadMissingToken(t *testing.T) {
	os.Unsetenv("TELEGRAM_TOKEN")
	os.Unsetenv("TELEGRAM_ALLOWLIST")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing TELEGRAM_TOKEN")
	}
}

func TestLoadMissingAllowlist(t *testing.T) {
	os.Setenv("TELEGRAM_TOKEN", "test-token")
	defer os.Unsetenv("TELEGRAM_TOKEN")
	os.Unsetenv("TELEGRAM_ALLOWLIST")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing TELEGRAM_ALLOWLIST")
	}
}

func TestLoadSuccess(t *testing.T) {
	os.Setenv("TELEGRAM_TOKEN", "test-token")
	os.Setenv("TELEGRAM_ALLOWLIST", "123,456")
	defer os.Unsetenv("TELEGRAM_TOKEN")
	defer os.Unsetenv("TELEGRAM_ALLOWLIST")
	os.Unsetenv("MAX_CONTEXT_TOKENS")
	os.Unsetenv("SOFT_BUDGET")
	os.Unsetenv("HARD_BUDGET")
	os.Unsetenv("LOG_LEVEL")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.TelegramToken != "test-token" {
		t.Errorf("TelegramToken = %q, want %q", cfg.TelegramToken, "test-token")
	}
	if len(cfg.Allowlist) != 2 || cfg.Allowlist[0] != "123" || cfg.Allowlist[1] != "456" {
		t.Errorf("Allowlist = %v, want [123 456]", cfg.Allowlist)
	}
	if cfg.MaxContextTokens != 4000 {
		t.Errorf("MaxContextTokens = %d, want 4000", cfg.MaxContextTokens)
	}
}