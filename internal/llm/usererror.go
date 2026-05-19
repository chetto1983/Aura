package llm

import (
	"context"
	"errors"
	"net"
	"strings"
)

// Fixed user-visible templates. None of these leak err.Error() content.
const (
	msgRateLimited      = "Aura is rate-limited by the LLM provider. Retrying automatically did not recover -- try again in ~30s."
	msgNetworkIssue     = "Network issue talking to the LLM. Try again."
	msgAuthFailed       = "LLM auth failed -- check API key in /settings."
	msgModelUnavailable = "LLM model unavailable -- check model name in /settings."
	msgUnknown          = "Aura could not reach the LLM. Try again."
)

// UserMessageFor maps an LLM call error to a short, user-visible diagnostic
// string. It never leaks err.Error() content — only fixed template strings are
// returned. Sub-classification priority: HTTP status > transport > message pattern > bucket.
func UserMessageFor(err error) string {
	if err == nil {
		return ""
	}

	// HTTP status via typed *APIError (most specific).
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 429:
			return msgRateLimited
		case apiErr.StatusCode == 401 || apiErr.StatusCode == 403:
			return msgAuthFailed
		case apiErr.StatusCode == 404:
			return msgModelUnavailable
		case apiErr.StatusCode >= 500:
			return msgNetworkIssue
		}
	}

	// Network / timeout errors.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return msgNetworkIssue
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return msgNetworkIssue
	}

	// Message-pattern sub-classification on the redacted error message.
	lower := strings.ToLower(redact(err.Error()))
	switch {
	case strings.Contains(lower, "rate"):
		return msgRateLimited
	case strings.Contains(lower, "overload"):
		return msgNetworkIssue
	case strings.Contains(lower, "model not found"):
		return msgModelUnavailable
	case strings.Contains(lower, "auth") || strings.Contains(lower, "unauthorized"):
		return msgAuthFailed
	}

	// Bucket-level coarse fallback.
	bucket, _, _ := Classify(err)
	if bucket == BucketTransient {
		return msgNetworkIssue
	}
	return msgUnknown
}
