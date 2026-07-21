// Package redact provides dependency-light credential redaction for logs and
// wire responses.
package redact

import (
	"regexp"
	"strings"
)

var secretPattern = regexp.MustCompile(`(?i)(postgres(?:ql)?|mysql|mongodb|redis|amqp|bolt(?:\+s|\+ssc)?|neo4j(?:\+s|\+ssc)?)://[^\s"']*`)
var urlUserinfoPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^\s@]+@`)
var tokenPattern = regexp.MustCompile(`(?i)(bearer\s+|api[_-]?key=|token=)\S+`)

// String strips credential-bearing substrings from an arbitrary string.
func String(message string) string {
	out := secretPattern.ReplaceAllStringFunc(message, func(match string) string {
		scheme := match
		if idx := strings.Index(match, "://"); idx >= 0 {
			scheme = match[:idx]
		}
		return scheme + "://[redacted]"
	})
	out = urlUserinfoPattern.ReplaceAllString(out, "${1}[redacted]@")
	return tokenPattern.ReplaceAllStringFunc(out, func(match string) string {
		prefix := match
		if idx := strings.IndexAny(match, " =\t"); idx >= 0 {
			prefix = match[:idx+1]
		}
		return prefix + "[redacted]"
	})
}
