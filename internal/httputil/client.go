// Package httputil holds small HTTP-shaped helpers shared across packages.
//
// Like internal/stringx, the surface is deliberately narrow: domain-neutral
// utilities only. Helpers that touch business semantics (auth headers,
// retry policy, body parsing) belong in the package that owns the semantic.
package httputil

import (
	"net/http"
	"time"
)

// NewHTTPClient returns an *http.Client with timeout, falling back to
// fallback when timeout is zero or negative. The shape matches the
// duplicated `if timeout <= 0 { timeout = X } &http.Client{Timeout: timeout}`
// pattern that appeared in 4 caller-vs-default sites across the codebase
// before extraction (qdrant, ocr, embed_batch, embed_http).
//
// Use this when the timeout comes from a caller-supplied value AND there is
// a meaningful fallback. For hardcoded singletons (e.g. a 60s constant in
// internal/mcp/client.go) keep the inline literal — wrapping a single
// constant just makes the line longer.
//
// Callers that need NO timeout (e.g. open-ended LLM streaming) should not
// use this helper — instantiate &http.Client{} directly with a comment
// explaining the deliberate choice. A "no timeout" signal would widen the
// helper API for a single-caller flag and is not worth the trade.
func NewHTTPClient(timeout, fallback time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = fallback
	}
	return &http.Client{Timeout: timeout}
}
