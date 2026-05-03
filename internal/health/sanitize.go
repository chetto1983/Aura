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
	// Exact matches — keys that are always secrets
	exactMatches := map[string]bool{
		"token":      true,
		"auth":       true,
		"cookie":     true,
		"secret":     true,
		"credential": true,
		"password":   true,
		"apikey":     true,
		"api_key":    true,
		"api-key":    true,
	}
	if exactMatches[key] {
		return true
	}

	// Prefix matches — keys that start with these are likely secrets
	secretPrefixes := []string{
		"token_",
		"api_key_",
		"auth_",
		"secret_",
		"password_",
	}
	for _, prefix := range secretPrefixes {
		if strings.HasPrefix(key, prefix) {
			return true
		}
	}
	return false
}
