// Package redact provides dependency-light credential redaction. It is the SINGLE
// credential-pattern table in the tree: logs, wire responses, the append-only
// aura.tool_invocations ledger (via toolinvocations.RedactForLedger) and the context
// summarizer all scrub through String.
//
// It used to be two tables, and neither was a superset of the other: this one caught
// DSNs and URL userinfo the ledger's missed, while the ledger's caught Authorization
// headers, sk-/sk-or- keys, AWS key ids and JSON credential fields this one missed. A
// secret was redacted or not depending on which caller happened to see it — including
// OpenRouter's own sk-or- keys, which this deployment handles and this table did not
// know. Adding a shape here now fixes every caller at once, which is the whole point
// of there being one table.
//
// Callers that persist add their own bounds on top (the ledger caps bytes and replaces
// NUL before redacting, so an over-cap secret tail is truncated away and the patterns
// scan a bounded input).
package redact

import (
	"regexp"
	"strings"
)

// Placeholder replaces every matched credential value. Exported so callers that assert
// on redacted output name it instead of duplicating the literal.
//
// It is the ledger's historical upper-case spelling, deliberately: aura.tool_invocations
// is append-only and un-deletable by trigger, so rows already carry [REDACTED] forever.
// Standardising on the other spelling would have split one durable column across two
// formats to make transient log lines prettier.
const Placeholder = "[REDACTED]"

// A credential value is redacted but its KEY is kept wherever the pattern has one:
// `token=[REDACTED]` says what was scrubbed, a bare `[REDACTED]` does not, and the key
// name is not the secret.
//
// Order is most-specific-first and load-bearing. `Authorization: Bearer sk-a.b.c` must
// be caught by the header pattern, which takes the value to end-of-line; the narrower
// sk- charset would not match a dotted value at all.
var patterns = []*struct {
	re   *regexp.Regexp
	repl string
}{
	// Whole DSN including host: these schemes only ever appear in a connection string.
	// ArcadeDB is deliberately absent and needs no entry — it speaks plain http on :2480
	// and authenticates with a Basic header built in client.go, so no credential ever
	// reaches a URL, and adding `https?` here would wipe every legitimate URL an error
	// message carries.
	{regexp.MustCompile(`(?i)(postgres(?:ql)?|mysql|mongodb|redis|amqp)://[^\s"']*`), ""},
	// user:pass@ in any URL.
	{regexp.MustCompile(`(?i)([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^\s@]+@`), "${1}" + Placeholder + "@"},
	// Authorization header carrying any scheme value (Bearer/Basic/token).
	{regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)[^\r\n]+`), "${1}" + Placeholder},
	// A bare Bearer credential not preceded by the Authorization keyword.
	{regexp.MustCompile(`(?i)(bearer\s+)[^\s,;"']+`), "${1}" + Placeholder},
	// OpenAI / OpenRouter keys: sk-… and sk-or-…. Opaque, so there is no key to keep.
	{regexp.MustCompile(`sk-(?:or-)?[A-Za-z0-9]{16,}`), Placeholder},
	// AWS access key ids (permanent IAM keys and temporary STS credentials).
	{regexp.MustCompile(`(AKIA|ASIA|AROA|AIDA|ANPA|ANVA|AIAA)[0-9A-Z]{16}`), Placeholder},
	// JSON credential fields, e.g. {"password":"hunter2"}. The inline pattern below
	// misses these because a quote sits between the key and the colon.
	//
	// The value is `[^"]+` (any non-empty run) and the CLOSING QUOTE IS OPTIONAL. Both
	// details are load-bearing and were both wrong before:
	//   - a `{4,}` lower bound let `{"password":"x"}` through verbatim. A one-character
	//     credential is still a credential; only the empty value is safely ignorable.
	//   - requiring the closing quote let a TRUNCATED value through, and the ledger
	//     manufactures exactly that case: RedactForLedger caps the string BEFORE
	//     redacting, so a JSON credential cut by the byte cap arrives here unterminated
	//     and, with a mandatory closing quote, survived redaction whole.
	{regexp.MustCompile(`(?i)("(?:password|passwd|pwd|api[_-]?key|token|secret|client[_-]?secret)"\s*:\s*)"[^"]+"?`), `${1}"` + Placeholder + `"`},
	// Inline assignments: password=…, token: …, api_key=…. The value runs to the next
	// whitespace, quote, comma, semicolon or ampersand, so a query string stops at & and
	// a JSON blob stops at the closing quote.
	{regexp.MustCompile(`(?i)((?:password|passwd|pwd|api[_-]?key|token|secret|client[_-]?secret)\s*[:=]\s*)"?[^"\s&,;']+`), "${1}" + Placeholder},
}

// String strips credential-bearing substrings from an arbitrary string. A string with
// no credential shape is returned byte-identical.
func String(message string) string {
	out := message
	for i, p := range patterns {
		if i == 0 {
			// The DSN pattern keeps the scheme and drops everything after it, so it needs
			// the match text rather than a capture-group template.
			out = p.re.ReplaceAllStringFunc(out, func(match string) string {
				scheme, _, _ := strings.Cut(match, "://")
				return scheme + "://" + Placeholder
			})
			continue
		}
		out = p.re.ReplaceAllString(out, p.repl)
	}
	return out
}
