package health

import (
	"context"
	"log/slog"
	"strings"
)

// SanitizeHandler wraps a slog.Handler to redact known secret patterns from log output.
type SanitizeHandler struct {
	inner slog.Handler
}

// NewSanitizeHandler creates a handler that redacts secrets before passing to the inner handler.
func NewSanitizeHandler(inner slog.Handler) *SanitizeHandler {
	return &SanitizeHandler{inner: inner}
}

func (h *SanitizeHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *SanitizeHandler) Handle(ctx context.Context, r slog.Record) error {
	var attrs []slog.Attr
	r.Attrs(func(a slog.Attr) bool {
		attrs = append(attrs, sanitizeAttr(a))
		return true
	})

	newR := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)
	for _, a := range attrs {
		newR.AddAttrs(a)
	}
	return h.inner.Handle(ctx, newR)
}

func (h *SanitizeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, len(attrs))
	for i, a := range attrs {
		sanitized[i] = sanitizeAttr(a)
	}
	return &SanitizeHandler{inner: h.inner.WithAttrs(sanitized)}
}

func (h *SanitizeHandler) WithGroup(name string) slog.Handler {
	return &SanitizeHandler{inner: h.inner.WithGroup(name)}
}

func sanitizeAttr(a slog.Attr) slog.Attr {
	key := strings.ToLower(a.Key)
	if isSecretKey(key) {
		return slog.String(a.Key, "[REDACTED]")
	}
	return a
}

// isSecretKey checks if a log attribute key likely contains a secret value.
func isSecretKey(key string) bool {
	secretPatterns := []string{
		"password",
		"secret",
		"token",
		"api_key",
		"apikey",
		"api-key",
		"credential",
		"auth",
		"cookie",
	}
	for _, pattern := range secretPatterns {
		if strings.Contains(key, pattern) {
			return true
		}
	}
	return false
}