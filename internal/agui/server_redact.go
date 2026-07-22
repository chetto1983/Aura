package agui

import "github.com/chetto1983/aura/internal/redact"

// server_redact.go holds the wire-error credential-redaction controls (T-12-10 / V7 /
// WR-03), split off server.go on the refactor-on-touch 600-LOC trip (Phase 27 plan 02).

func sanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	return SanitizeString(err.Error())
}

// SanitizeString strips credential-bearing substrings from an arbitrary string.
func SanitizeString(message string) string {
	return redact.String(message)
}
