// redact.go holds the SC3 allowlist projections BuildSnapshot composes:
// projectTurns, toolNames, and projectArtifact. Every one CONSTRUCTS its
// output field-by-field from an explicit allowlist — never by copying an
// input struct/map and deleting the keys that must not survive. This
// mirrors the technique at web/src/chat/sseAdapter.ts:353-361, the repo's
// only other allowlist projection, which builds its 4-key artifact object
// literal the same way.
//
// Trust boundary (R-09): the 37A path strip at sseAdapter.ts:346-360 runs
// client-side because that browser is the OWNER's own authenticated
// session. A share recipient has no such relationship to this process — the
// recipient's browser is NOT a trust boundary. This file is: redaction must
// be complete before any byte leaves the server.
//
// Anti-analog: internal/agui/server_redact.go's SanitizeString is a regex
// DENYLIST, and it is the right tool for its own job (wire-error credential
// scrubbing, T-12-10) — but it is the WRONG shape for SC3. A denylist only
// catches the path shapes someone thought to write a pattern for; it misses
// "/etc/passwd" in a raw shell result and every path shape nobody thought
// of. Do not refactor this file into a regex pass. (SanitizeString does
// stay correct for a different 37F surface: share_audit rows go through the
// same audit-union path audit_store.go already applies it to, so they
// inherit the scrub for free — that is a different trust boundary than
// this one.)
package share

import (
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// projectTurns builds the recipient-visible turn list from the owner-trusted
// message history. Role is an explicit user/assistant ALLOWLIST with a
// default that drops — never a denylist of roles known to be unsafe, so an
// unrecognized future role fails closed (never emitted) rather than falling
// through. Seq is assigned from a dense counter over the EMITTED turns, not
// from the input index, so it never reveals how many turns were dropped.
func projectTurns(msgs []llm.Message) []SnapshotTurn {
	turns := make([]SnapshotTurn, 0, len(msgs))
	seq := 0
	for _, m := range msgs {
		switch m.Role {
		case llm.RoleUser, llm.RoleAssistant:
			turns = append(turns, SnapshotTurn{
				Seq:       seq,
				Role:      m.Role,
				Text:      m.Content,
				ToolNames: toolNames(m.ToolCalls),
			})
			seq++
		default:
			// role=="tool" (the raw tool result) and role=="system" — and any
			// unrecognized role — never reach the snapshot.
		}
	}
	return turns
}

// toolNames extracts each call's Function.Name in call order, preserving
// duplicates (a turn that called send_file twice reports it twice —
// collapsing would misreport provenance) and dropping a blank or
// whitespace-only name. It never reads Function.Arguments or any
// ToolCall/Message correlation id — there is no code path here that could
// reach either. The trailing len(names)==0 check normalizes make()'s
// non-nil empty slice back to nil — both an empty len(calls) input and an
// all-blank-names input fall through to it, so ToolNames is nil (never a
// non-nil empty slice) whenever nothing survives, matching the omitempty
// json tag's intent byte-for-byte.
func toolNames(calls []llm.ToolCall) []string {
	names := make([]string, 0, len(calls))
	for _, c := range calls {
		name := strings.TrimSpace(c.Function.Name)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	if len(names) == 0 {
		return nil
	}
	return names
}

// projectArtifact constructs the 4-key allowlist descriptor field-by-field
// from ArtifactMeta, the same technique sseAdapter.ts:353-361 uses for the
// aura.artifact wire event — never by unmarshalling a source descriptor into
// a map[string]any and deleting a path key. ArtifactMeta itself has no
// path-shaped field, so there is nothing to delete: the four assignments
// below are the entire function. staticcheck S1016 would collapse this to a
// type conversion (the two structs happen to share a field layout today);
// that is deliberately NOT done here — the explicit field list is the SC3
// allowlist contract, and it must keep failing loudly (a compile error) the
// day someone adds a field to either struct that the other doesn't share.
func projectArtifact(a ArtifactMeta) SnapshotArtifact {
	return SnapshotArtifact{ //nolint:staticcheck // S1016: explicit fields are the allowlist, not incidental duplication
		AssetID:   a.AssetID,
		FileName:  a.FileName,
		MIMEType:  a.MIMEType,
		SizeBytes: a.SizeBytes,
	}
}
