package logging

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/aura/aura/internal/health"
)

func TestSetupCreatesLogger(t *testing.T) {
	logger, cleanup := Setup("info", t.TempDir())
	defer cleanup()
	if logger == nil {
		t.Fatal("expected non-nil logger")
	}
}

func TestSetupLevels(t *testing.T) {
	for _, level := range []string{"debug", "info", "warn", "error"} {
		logger, cleanup := Setup(level, t.TempDir())
		defer cleanup()
		if logger == nil {
			t.Errorf("expected non-nil logger for level %q", level)
		}
	}
}

func TestSanitizeIntegration(t *testing.T) {
	var buf bytes.Buffer
	handler := slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	sanitized := health.NewSanitizeHandler(handler)
	testLogger := slog.New(sanitized)

	testLogger.Info("test", "api_key", "sk-secret", "user_id", "alice")

	output := buf.String()
	if strings.Contains(output, "sk-secret") {
		t.Error("api_key should be redacted")
	}
	if !strings.Contains(output, "[REDACTED]") {
		t.Error("secrets should be replaced with [REDACTED]")
	}
	if !strings.Contains(output, "alice") {
		t.Error("non-secret values should not be redacted")
	}
}

func TestZapLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"debug", "debug"},
		{"info", "info"},
		{"warn", "warn"},
		{"error", "error"},
		{"unknown", "info"},
	}
	for _, tt := range tests {
		level := zapLevel(tt.input)
		if level.String() != tt.expected {
			t.Errorf("zapLevel(%q) = %q, want %q", tt.input, level.String(), tt.expected)
		}
	}
}

func TestNewNopLogger(t *testing.T) {
	logger := NewNopLogger()
	if logger == nil {
		t.Fatal("expected non-nil nop logger")
	}
	logger.Info("this should be discarded")
}

func TestAttrToZapField(t *testing.T) {
	tests := []struct {
		name string
		attr slog.Attr
	}{
		{"string", slog.String("key", "value")},
		{"int64", slog.Int64("count", 42)},
		{"bool", slog.Bool("enabled", true)},
		{"float64", slog.Float64("ratio", 3.14)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := attrToZapField(tt.attr)
			if field.Key != tt.attr.Key {
				t.Errorf("expected key %q, got %q", tt.attr.Key, field.Key)
			}
		})
	}
}
