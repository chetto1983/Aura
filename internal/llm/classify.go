package llm

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
)

// Bucket classifies an LLM call failure for retry policy selection.
type Bucket int

const (
	BucketTransient Bucket = iota
	BucketContent
	BucketPermanent
)

func (b Bucket) String() string {
	return [...]string{"transient", "content", "permanent"}[b]
}

// Sentinel errors signalled by tool execute paths and schema validators.
// The classifier maps these to BucketContent.
var (
	ErrSchemaValidation  = errors.New("schema validation failed")
	ErrEmptyOutput       = errors.New("empty assistant output")
	ErrMalformedToolCall = errors.New("malformed tool call arguments")
)

// APIError is a typed wrapper around an HTTP response failure from an
// OpenAI-compatible provider. The classifier uses errors.As to extract
// StatusCode for priority-pipeline matching.
type APIError struct {
	StatusCode int
	Body       string // already redacted by the producer
}

func (e *APIError) Error() string {
	return fmt.Sprintf("LLM API error (status %d): %s", e.StatusCode, e.Body)
}

// Pre-compiled redaction patterns. Order matters — most-specific first.
var (
	redactJWT          = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{4,}\.[A-Za-z0-9_\-]{4,}\.[A-Za-z0-9_\-]{4,}`)
	redactOpenRouter   = regexp.MustCompile(`sk-or-v1-[A-Za-z0-9_\-]+`)
	redactBearer       = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._\-]+`)
	redactAuthHeader   = regexp.MustCompile(`(?i)Authorization:\s*[^\s\n\r]+`)
	redactBasicAuthURL = regexp.MustCompile(`(https?)://[^:/@\s]+:[^@\s]+@([^\s]+)`)
	redactURLToken     = regexp.MustCompile(`([?&](?:token|api_key|apikey|key|secret|access_token|sig)=)[^&\s"']+`)
	redactBase64Long   = regexp.MustCompile(`[A-Za-z0-9+/]{32,}={0,2}`)
)

// redact strips secrets from an error message BEFORE the cleaned string
// escapes Classify. CLAUDE.md "no secrets in logs" + Pitfall #5.
func redact(s string) string {
	s = redactJWT.ReplaceAllString(s, "***REDACTED-JWT***")
	s = redactOpenRouter.ReplaceAllString(s, "***REDACTED-API-KEY***")
	s = redactBearer.ReplaceAllString(s, "Bearer ***REDACTED***")
	s = redactAuthHeader.ReplaceAllString(s, "Authorization: ***REDACTED***")
	s = redactBasicAuthURL.ReplaceAllString(s, "$1://***REDACTED***@$2")
	s = redactURLToken.ReplaceAllString(s, "${1}***REDACTED***")
	s = redactBase64Long.ReplaceAllString(s, "***REDACTED-BASE64***")
	return s
}

// Classify returns (bucket, retryable, cleaned).
// Priority: context shape → local sentinel → HTTP status → transport → message-pattern.
// The cleaned string has secrets stripped via redact() before it escapes this function.
func Classify(err error) (Bucket, bool, string) {
	if err == nil {
		return BucketPermanent, false, ""
	}
	cleaned := redact(err.Error())

	// 1. Context-shape errors.
	if errors.Is(err, context.Canceled) {
		return BucketPermanent, false, cleaned
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return BucketTransient, true, cleaned
	}

	// 2. Local sentinels (set by tool execute path on schema fail).
	if errors.Is(err, ErrSchemaValidation) {
		return BucketContent, true, cleaned
	}
	if errors.Is(err, ErrEmptyOutput) {
		return BucketContent, true, cleaned
	}
	if errors.Is(err, ErrMalformedToolCall) {
		return BucketContent, true, cleaned
	}

	// 3. HTTP status via typed *APIError.
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.StatusCode == 429:
			return BucketTransient, true, cleaned
		case apiErr.StatusCode >= 500:
			return BucketTransient, true, cleaned
		case apiErr.StatusCode == 401 || apiErr.StatusCode == 403:
			return BucketPermanent, false, cleaned
		case apiErr.StatusCode == 400:
			return BucketPermanent, false, cleaned
		}
	}

	// 4. Transport / network errors.
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return BucketTransient, true, cleaned
	}

	// 5. Message-pattern fallback (OpenAI / OpenAI-compatible).
	lower := strings.ToLower(cleaned)
	switch {
	case strings.Contains(lower, "rate limit"):
		return BucketTransient, true, cleaned
	case strings.Contains(lower, "overloaded"):
		return BucketTransient, true, cleaned
	case strings.Contains(lower, "quota"):
		return BucketPermanent, false, cleaned
	case strings.Contains(lower, "model not found"):
		return BucketPermanent, false, cleaned
	}

	// Default: unknown errors are treated as transient (one retry is cheap).
	return BucketTransient, true, cleaned
}
