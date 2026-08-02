// Package redact provides dependency-light credential redaction for logs and
// wire responses.
package redact

import (
	"regexp"
	"strings"
)

// secretPattern wipes a whole DSN, host included, for the schemes that only ever appear
// in a connection string. ArcadeDB is deliberately NOT here and needs no entry: it speaks
// plain http on :2480 and authenticates with a Basic header built in client.go, so no
// credential ever reaches a URL — and adding `https?` would wipe every legitimate URL an
// error message carries. What survives an ArcadeDB error is the internal hostname, which
// urlUserinfoPattern leaves alone by design.
var secretPattern = regexp.MustCompile(`(?i)(postgres(?:ql)?|mysql|mongodb|redis|amqp)://[^\s"']*`)
var urlUserinfoPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^\s@]+@`)
var tokenPattern = regexp.MustCompile(`(?i)(bearer\s+|(?:api[_-]?key|token|password|passwd|pwd|secret|client[_-]?secret)\s*[:=]\s*)[^\s,;"']+`)

// String strips credential-bearing substrings from an arbitrary string.
func String(message string) string {
	out := secretPattern.ReplaceAllStringFunc(message, func(match string) string {
		scheme := match
		if before, _, ok := strings.Cut(match, "://"); ok {
			scheme = before
		}
		return scheme + "://[redacted]"
	})
	out = urlUserinfoPattern.ReplaceAllString(out, "${1}[redacted]@")
	return tokenPattern.ReplaceAllString(out, "${1}[redacted]")
}
