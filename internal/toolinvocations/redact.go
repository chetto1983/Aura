package toolinvocations

import (
	"regexp"
	"unicode/utf8"
)

// WR-02: the append-only aura.tool_invocations ledger captures the VERBATIM tool
// argument JSON + result preview for forensics. A model can place a secret on a
// shell_exec/sandbox_exec command line (an inline `Authorization: Bearer ...`, an
// API key, a `password=...`). Migration 0011's triggers reject DELETE, so any
// captured secret is durable, append-only, and un-deletable (removed only by a
// conversation FK-cascade) while aura_app retains SELECT. RedactForLedger is the
// bounded, capped, redacted capture seam applied at the persistence boundary
// (toParams) BEFORE a value reaches the durable column, so no caller can write a
// raw secret into the ledger.
//
// Posture: cap FIRST (bound the durable footprint), then redact over the capped
// string (so an over-cap secret tail is already truncated away, and the redactor
// scans a bounded input). The caps mirror the existing preview discipline: the
// result preview cap is AURA_CONTEXT_PREVIEW_CAP_BYTES=2048 (internal/agent), so
// result_preview keeps that ceiling here; args_raw gets a larger 8 KiB ceiling
// because a legitimate multi-tool argument JSON (a long shell script, a write
// payload) is bulkier than a result preview yet still bounded for the ledger.
const (
	// ArgsRawCapBytes bounds the durable args_raw column (8 KiB). A larger ceiling
	// than the preview cap because tool argument JSON (scripts, write bodies) is
	// legitimately bulkier; still bounded so a pathological arg blob cannot bloat
	// the un-deletable ledger.
	ArgsRawCapBytes = 8 * 1024
	// ResultPreviewCapBytes bounds the durable result_preview column (2 KiB),
	// mirroring AURA_CONTEXT_PREVIEW_CAP_BYTES (the agent's tool-result preview cap)
	// so the ledger preview never exceeds the in-context preview the model saw.
	ResultPreviewCapBytes = 2 * 1024

	redactedPlaceholder = "[REDACTED]"
	// capMarker is appended when a value is truncated to its byte cap, so a reader
	// can tell a capped value from one that happened to end at the boundary.
	capMarker = "…[capped]"
)

// secretPattern is one named credential shape redacted out of a ledger value.
// The table is package-level + deterministic (no NFKC: tool argument JSON and
// shell command lines are ASCII-credential shaped in practice, and the skills
// NFKC fold is not importable here without a dependency the WR-02 design rejects
// as non-trivial). Each pattern replaces its match with [REDACTED]; the order is
// most-specific-first so a header match wins over a bare-key match on the same span.
type secretPattern struct {
	name string
	re   *regexp.Regexp
}

// secretPatterns is the redaction table. Patterns are case-insensitive where the
// credential keyword is (Authorization/Bearer/password/token/api_key); the opaque
// key shapes (sk-/sk-or-, AKIA) are matched on their literal prefix + charset.
var secretPatterns = []secretPattern{
	// Authorization header carrying any scheme value (Bearer/Basic/token), e.g.
	// `Authorization: Bearer abc.def` or `-H "Authorization: Basic Zm9v"`.
	{"authorization_header", regexp.MustCompile(`(?i)authorization\s*[:=]\s*[^\r\n]+`)},
	// A bare Bearer credential not preceded by the Authorization keyword.
	{"bearer_token", regexp.MustCompile(`(?i)bearer\s+[A-Za-z0-9._\-+/=]+`)},
	// OpenAI / OpenRouter style keys: sk-… and sk-or-… (sk-or- is a prefix of sk-,
	// but the charset run captures the whole key either way).
	{"openai_key", regexp.MustCompile(`sk-(or-)?[A-Za-z0-9]{16,}`)},
	// AWS access key ids (permanent IAM keys and temporary STS credentials).
	{"aws_key", regexp.MustCompile(`(AKIA|ASIA|AROA|AIDA|ANPA|ANVA|AIAA)[0-9A-Z]{16}`)},
	// Inline credential assignments: password=…, token=…, api_key=… / apikey=… —
	// value runs to the next whitespace, quote, or ampersand (query-string safe).
	{"inline_credential", regexp.MustCompile(`(?i)(password|api[_-]?key|token|secret)\s*[:=]\s*("?)[^"\s&]+`)},
}

// RedactForLedger caps s to capBytes (UTF-8 boundary-safe) then redacts every
// known credential shape, returning the value safe to persist into the append-only
// ledger. An empty input returns empty (the column stays NULL upstream via the
// existing valid-flag). capBytes <= 0 means "no cap" (redact only).
func RedactForLedger(s string, capBytes int) string {
	if s == "" {
		return ""
	}
	capped := capUTF8(s, capBytes)
	for _, p := range secretPatterns {
		capped = p.re.ReplaceAllString(capped, redactedPlaceholder)
	}
	return capped
}

// capUTF8 truncates s to at most capBytes bytes WITHOUT splitting a multi-byte
// rune (a naive s[:capBytes] could slice mid-rune and corrupt the stored UTF-8).
// When truncation happens a capMarker is appended so a reader can distinguish a
// capped value. capBytes <= 0 returns s unchanged.
func capUTF8(s string, capBytes int) string {
	if capBytes <= 0 || len(s) <= capBytes {
		return s
	}
	cut := capBytes
	// Walk back to a rune boundary: a continuation byte is 0b10xxxxxx.
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + capMarker
}
