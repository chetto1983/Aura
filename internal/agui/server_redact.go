package agui

import (
	"regexp"
	"strings"
)

// server_redact.go holds the wire-error credential-redaction controls (T-12-10 / V7 /
// WR-03), split off server.go on the refactor-on-touch 600-LOC trip (Phase 27 plan 02).
// sanitizeErr/SanitizeString are the server-side belt-and-suspenders applied to every
// string that reaches the wire (4xx/502 bodies, RUN_ERROR frames): a DSN, a URL userinfo,
// or a bearer/api-key token is collapsed to a marker before it leaves the process. The
// in-flight RUN_ERROR redactor (redactEvent) stays in server.go with the SSE pump it
// guards.

// secretPattern collapses a whole DB DSN (scheme + userinfo + host path) to a
// scheme + "[redacted]" marker — the password AND the host path both leak operational
// detail, so the entire connection string after the scheme is dropped (T-12-10 / V7). The
// Neo4j bolt/neo4j schemes (incl. the +s/+ssc TLS variants) are included so a graph read
// error carrying a `bolt://user:pass@host:7687` DSN leaks neither the credential NOR the
// host (T-27-05 / V13 — the Phase-27 graph routes are the first wire surface a bolt DSN
// can reach).
var secretPattern = regexp.MustCompile(`(?i)(postgres(?:ql)?|mysql|mongodb|redis|amqp|bolt(?:\+s|\+ssc)?|neo4j(?:\+s|\+ssc)?)://[^\s"']*`)

// urlUserinfoPattern matches `scheme://user:password@` for ANY URL scheme (an HTTP MCP
// server, webhook, or proxy URL — not just the DB DSNs above). Only the userinfo is
// collapsed; the rest of the URL is left intact so the error stays diagnosable (WR-03).
// The DSN pass runs first and already consumes its schemes, so this never double-matches
// them.
var urlUserinfoPattern = regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^\s@]+@`)

// tokenPattern matches common credential tokens embedded in free-form error strings:
// `Bearer <token>`, `api_key=<...>`, `api-key=<...>`, `apikey=<...>`, `token=<...>`. The
// token body is collapsed, the prefix kept so the redaction is legible (WR-03).
var tokenPattern = regexp.MustCompile(`(?i)(bearer\s+|api[_-]?key=|token=)\S+`)

// sanitizeErr redacts credential-bearing substrings from an error string before it is
// surfaced over the wire (RUN_ERROR / 4xx body). The agent path already structurally
// redacts the OpenRouter key (D-28); this is the server-side belt-and-suspenders for the
// tool/infra error strings the translator forwards (T-12-10).
func sanitizeErr(err error) string {
	if err == nil {
		return ""
	}
	return SanitizeString(err.Error())
}

// SanitizeString strips credential-bearing substrings from an arbitrary string: whole DB
// DSNs collapse to a scheme marker, generic URL userinfo collapses to `scheme://[redacted]@`,
// and bearer/api-key/token shapes collapse to a `prefix[redacted]` marker (WR-03). The DSN
// pass runs first so the generic userinfo pass never reaches the DB schemes.
func SanitizeString(msg string) string {
	out := secretPattern.ReplaceAllStringFunc(msg, func(match string) string {
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
