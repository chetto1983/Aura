package redact

import "strings"

// Line makes an untrusted string safe to embed in a single log line. It scrubs
// credential shapes through String, escapes CR/LF visibly (the log-forging vector
// CodeQL's go/log-injection flags: a value carrying "\nlevel=INFO ..." would mint a
// counterfeit log record), and drops every remaining C0/C1 control character — the
// ESC that opens an ANSI terminal-rewrite sequence above all. Tab survives as the one
// legitimate inline control.
//
// The two strings.ReplaceAll calls are the barrier CodeQL models, so keep them as
// literal ReplaceAll calls rather than folding them into the strings.Map below.
func Line(message string) string {
	out := String(message)
	out = strings.ReplaceAll(out, "\r", `\r`)
	out = strings.ReplaceAll(out, "\n", `\n`)
	return strings.Map(func(r rune) rune {
		if r == '\t' {
			return r
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) {
			return -1
		}
		return r
	}, out)
}
