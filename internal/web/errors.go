package web

import (
	"encoding/json"
	"strconv"
)

// The D-38 stable error enum. These strings are the model-visible `error` field
// values; they are a contract — never rename without a PRD amendment.
const (
	CodeSearchUnavailable  = "web_search_unavailable"
	CodeBlockedURL         = "blocked_url"
	CodeUnsupportedScheme  = "unsupported_scheme"
	CodeUnsupportedContent = "unsupported_content_type"
	CodeResponseTooLarge   = "response_too_large"
	CodeTimeout            = "timeout"
	CodeHTTPError          = "http_error"
	CodeExtractionFailed   = "extraction_failed"
)

// Stable, non-sensitive reason strings. Reasons name a CLASS of block, never a
// concrete IP/host/CIDR (D-27): "private_or_metadata_target" not "169.254.169.254".
const (
	ReasonSearxngNotConfigured = "searxng_not_configured"
	ReasonSearxngUnreachable   = "searxng_unreachable"
	ReasonPrivateOrMetadata    = "private_or_metadata_target"
	ReasonRedirectToBlocked    = "redirect_to_blocked_target"
	ReasonInvalidTarget        = "invalid_target"
)

// WebError is the ONLY error shape the model ever sees. It is deliberately flat
// and carries no IP, CIDR, internal hostname, response header, body snippet, or
// redirect-chain detail (D-26/27/28). It serializes to
// {error, reason, message, status_code?} (D-39/D-40).
type WebError struct {
	Code       string
	Reason     string
	Message    string
	StatusCode int
}

// MarshalJSON emits the D-39/D-40 wire shape: `error` (not `code`), optional
// `reason`/`message`, and `status_code` only when set. Hand-rolled rather than
// struct tags so an unset reason/status omits its key entirely (no leaky zero
// values, no `"status_code":0` on an SSRF block).
func (e *WebError) MarshalJSON() ([]byte, error) {
	m := map[string]any{"error": e.Code}
	if e.Reason != "" {
		m["reason"] = e.Reason
	}
	if e.Message != "" {
		m["message"] = e.Message
	}
	if e.StatusCode != 0 {
		m["status_code"] = e.StatusCode
	}
	return json.Marshal(m)
}

// JSON is the adapter-facing serializer; the tool adapter feeds the bytes to
// tools.NewResult so the model self-corrects (D-41 — errors stay inline, only
// successful large content spills).
func (e *WebError) JSON() ([]byte, error) { return json.Marshal(e) }

// Error renders the headline without any sensitive field (it mirrors only the
// already-sanitized Code/Reason/StatusCode). Safe to log.
func (e *WebError) Error() string {
	s := e.Code
	if e.Reason != "" {
		s += " (" + e.Reason + ")"
	}
	if e.StatusCode != 0 {
		s += " status " + strconv.Itoa(e.StatusCode)
	}
	return s
}

// internalError is the RICH, INTERNAL-ONLY error (D-28). It may carry the
// resolved IP, the host, and the redirect chain for tests and local debug logs.
// It MUST NEVER be returned to the model directly — sanitize() strips it to a
// WebError first. The sensitive fields are unexported so they cannot be JSON
// marshalled by accident.
type internalError struct {
	code       string
	reason     string
	message    string
	statusCode int

	// Sensitive — logs/tests only, never crosses the model boundary.
	resolvedIP   string
	host         string
	redirectFrom string
}

// Error renders a detailed headline for logs/tests; it intentionally includes the
// sensitive fields because this layer never reaches the model.
func (e *internalError) Error() string {
	s := e.code
	if e.reason != "" {
		s += " (" + e.reason + ")"
	}
	if e.host != "" {
		s += " host=" + e.host
	}
	if e.resolvedIP != "" {
		s += " ip=" + e.resolvedIP
	}
	if e.redirectFrom != "" {
		s += " redirect=" + e.redirectFrom
	}
	return s
}

// sanitize is the internal→model-visible mapper the adapters call. It copies ONLY
// the safe fields (code/reason/message/status) and drops every sensitive field
// (D-27). This is the single chokepoint that enforces the non-leak invariant.
func sanitize(e *internalError) *WebError {
	return &WebError{
		Code:       e.code,
		Reason:     e.reason,
		Message:    e.message,
		StatusCode: e.statusCode,
	}
}
