package agui

import (
	"errors"
	"net/http"

	"github.com/chetto1983/aura/internal/idempotency"
)

// Temporary RED shim. GREEN removes this file.
func parseIdempotencyKey(*http.Request) (string, error) { return "", errors.New("not implemented") }

func writeIdempotencyDecision(http.ResponseWriter, idempotency.BeginDecision) bool { return false }
