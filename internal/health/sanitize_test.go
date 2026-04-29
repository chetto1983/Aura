package health

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestSanitizeHandlerRedactsSecrets(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizeHandler(inner)
	logger := slog.New(handler)

	logger.Info("test message", "api_key", "sk-secret-12345", "token", "abc123", "username", "alice")

	output := buf.String()
	if strings.Contains(output, "sk-secret-12345") {
		t.Error("api_key should be redacted")
	}
	if strings.Contains(output, "abc123") {
		t.Error("token should be redacted")
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Error("secrets should be replaced with [REDACTED]")
	}
	if !strings.Contains(output, "alice") {
		t.Error("non-secret values should not be redacted")
	}
}

func TestSanitizeHandlerPassesNonSecrets(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelInfo})
	handler := NewSanitizeHandler(inner)
	logger := slog.New(handler)

	logger.Info("test", "user_id", "123", "duration", "5s")

	output := buf.String()
	if !strings.Contains(output, "123") {
		t.Error("user_id should not be redacted")
	}
	if !strings.Contains(output, "5s") {
		t.Error("duration should not be redacted")
	}
}

func TestIsSecretKey(t *testing.T) {
	tests := []struct {
		key   string
		secret bool
	}{
		{"api_key", true},
		{"token", true},
		{"password", true},
		{"secret", true},
		{"authorization", true},
		{"cookie", true},
		{"credential", true},
		{"user_id", false},
		{"duration", false},
		{"message", false},
		{"status", false},
	}

	for _, tt := range tests {
		got := isSecretKey(tt.key)
		if got != tt.secret {
			t.Errorf("isSecretKey(%q) = %v, want %v", tt.key, got, tt.secret)
		}
	}
}