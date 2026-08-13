package toolinvocations

import (
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/redact"
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
// Posture: replace DB-invalid NUL bytes, cap the durable footprint, then redact
// over the capped string (so an over-cap secret tail is already truncated away,
// and the redactor scans a bounded input). The durable result_preview cap is
// deliberately independent of, and far tighter than, the in-context tool-result
// preview cap AURA_CONTEXT_PREVIEW_CAP_BYTES (default 30000 bytes,
// internal/config/config_knobs.go:95 / config_defaults.go:36 defaultToolPreviewCapBytes):
// the durable column lives in an append-only, DELETE-rejecting ledger (migration
// 0011's triggers), so its footprint is bounded far below what the model saw
// rather than tracking it 1:1. args_raw gets a larger 8 KiB ceiling because a
// legitimate multi-tool argument JSON (a long shell script, a write payload) is
// bulkier than a result preview yet still bounded for the ledger.
const (
	// ArgsRawCapBytes bounds the durable args_raw column (8 KiB). A larger ceiling
	// than the preview cap because tool argument JSON (scripts, write bodies) is
	// legitimately bulkier; still bounded so a pathological arg blob cannot bloat
	// the un-deletable ledger.
	ArgsRawCapBytes = 8 * 1024
	// ResultPreviewCapBytes bounds the durable result_preview column (2 KiB) —
	// deliberately ~15x tighter than AURA_CONTEXT_PREVIEW_CAP_BYTES's 30000-byte
	// default (internal/config/config_knobs.go:95), never widened to match it: the
	// durable column is an append-only, DELETE-rejecting forensic ledger (migration
	// 0011's triggers), so 2 KiB < 30000 still holds (the ledger preview never
	// exceeds the in-context preview the model saw), but the two caps are
	// independently chosen, not mirrored.
	ResultPreviewCapBytes = 2 * 1024

	// redactedPlaceholder is the shared marker; the pattern table lives in
	// internal/redact so a shape added for one caller protects every caller.
	redactedPlaceholder = redact.Placeholder
	// capMarker is appended when a value is truncated to its byte cap, so a reader
	// can tell a capped value from one that happened to end at the boundary.
	capMarker = "…[capped]"
)

// RedactForLedger caps s to capBytes (UTF-8 boundary-safe) then redacts every
// known credential shape, returning the value safe to persist into the append-only
// ledger. An empty input returns empty (the column stays NULL upstream via the
// existing valid-flag). capBytes <= 0 means "no cap" (redact only).
//
// What is owned HERE is the durability posture — NUL replacement and the byte cap,
// applied BEFORE redaction so an over-cap secret tail is truncated away and the
// patterns scan a bounded input. The patterns themselves are redact.String's: this
// package used to carry a second, divergent table that missed DSNs and URL userinfo
// while redact's missed sk- keys and Authorization headers.
func RedactForLedger(s string, capBytes int) string {
	if s == "" {
		return ""
	}
	return redact.String(capUTF8(db.PostgresTextSafe(s), capBytes))
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
