package api

import (
	"log/slog"

	"github.com/aura/aura/internal/logging"
)

// HealthSanitizeHandler is a type alias kept for API compatibility.
// The implementation lives in internal/logging.
type HealthSanitizeHandler = logging.SanitizeHandler

// NewHealthSanitizeHandler creates a handler that redacts secrets from log output.
func NewHealthSanitizeHandler(inner slog.Handler) *HealthSanitizeHandler {
	return logging.NewSanitizeHandler(inner)
}
