// Package share implements the phase 37F conversation/artifact sharing
// redaction core. BuildSnapshot is the ONLY function in the repo that
// accepts an llm.Message and returns share-bound data: the Markdown
// formatter, the JSON formatter, and the public share page model are all
// pure functions of the Snapshot type below, so redaction cannot diverge
// across those surfaces (D-07). Snapshot has no field capable of holding a
// tool argument, a tool result, a filesystem path, or the owner's identity
// id — there is no field shaped to hold any of them, so a leak there is a
// compile error at the call site that tries to populate it, not something a
// reviewer has to catch by reading a diff (SC3).
package share

import (
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// Snapshot is the complete, recipient-safe projection of one conversation:
// the ONLY output BuildSnapshot produces, and the ONLY input every
// downstream format (Markdown/JSON/the public page model) consumes. It
// structurally cannot carry a tool argument, a tool result, a filesystem
// path, or the owner's identity id (the Conversation.identity_id column) —
// there is no field shaped to hold any of them.
type Snapshot struct {
	SchemaVersion int                `json:"schema_version"`
	Title         string             `json:"title"`
	Model         string             `json:"model"`
	CreatedAt     time.Time          `json:"created_at"`
	SnapshotAt    time.Time          `json:"snapshot_at"`
	Turns         []SnapshotTurn     `json:"turns"`
	Artifacts     []SnapshotArtifact `json:"artifacts"`
}

// SnapshotTurn is one recipient-visible turn. Only a "user" or "assistant"
// Role reaches this type: projectTurns (redact.go) drops role=="tool" and
// role=="system" turns entirely, so the raw tool result never has anywhere
// to land. ToolNames carries the called tool's name only, in call order,
// with duplicates preserved — never the tool call's argument JSON, and
// never a tool-call or message correlation id.
type SnapshotTurn struct {
	Seq       int      `json:"seq"`
	Role      string   `json:"role"`
	Text      string   `json:"text"`
	ToolNames []string `json:"tool_names,omitempty"`
}

// SnapshotArtifact is the 4-key allowlist descriptor for one delivered
// artifact: asset id, filename, mime type, and size. A source descriptor
// carrying any other key (in particular the host or staged-container path
// send_file emits) contributes nothing beyond these four — see
// projectArtifact in redact.go.
type SnapshotArtifact struct {
	AssetID   string `json:"asset_id"`
	FileName  string `json:"filename"`
	MIMEType  string `json:"mime_type"`
	SizeBytes int64  `json:"size_bytes"`
}

// ConvMeta is the narrow, local input BuildSnapshot accepts in place of
// conversations.Conversation. Conversation carries the owner's identity
// UUID; accepting it here would put that leak back within reach and would
// pull a conversations import into this package for no benefit. ConvMeta
// carries only what a Snapshot needs: title, model, and creation time.
type ConvMeta struct {
	Title     string
	Model     string
	CreatedAt time.Time
}

// ArtifactMeta is the narrow local input for one delivered artifact —
// exactly the four fields projectArtifact allowlists into a
// SnapshotArtifact. It deliberately has no path-shaped field: a caller
// building this from a persisted descriptor that also carries a path has
// nowhere to smuggle it through.
type ArtifactMeta struct {
	AssetID   string
	FileName  string
	MIMEType  string
	SizeBytes int64
}

// schemaVersion1 is the current Snapshot wire-contract version. Plan 37F-05
// mirrors this shape in TypeScript and plan 37F-06's adapters and the public
// share page all consume these json tags verbatim — a tag rename here is a
// cross-surface breaking change, not a local refactor.
const schemaVersion1 = 1

// BuildSnapshot is the ONLY function in the repo that accepts an
// []llm.Message and returns share-bound data — the SC3 trust boundary. The
// recipient's browser is NOT a trust boundary (R-09: unlike the 37A
// client-side path strip, which runs in the OWNER's own session); this
// constructor is where redaction must complete before any byte leaves the
// server. snapshotAt is taken as a parameter rather than read from
// time.Now() internally: a constructor that reads the wall clock is not
// deterministically testable, and the caller (the share service) already
// owns the transaction clock.
//
// Turns and Artifacts are built by the allowlist projections in redact.go
// (projectTurns / projectArtifact) — this function never touches a message
// or artifact field directly, so the allowlist boundary stays in one place.
func BuildSnapshot(conv ConvMeta, msgs []llm.Message, artifacts []ArtifactMeta, snapshotAt time.Time) (Snapshot, error) {
	snapArtifacts := make([]SnapshotArtifact, 0, len(artifacts))
	for _, a := range artifacts {
		snapArtifacts = append(snapArtifacts, projectArtifact(a))
	}
	return Snapshot{
		SchemaVersion: schemaVersion1,
		Title:         conv.Title,
		Model:         conv.Model,
		CreatedAt:     conv.CreatedAt,
		SnapshotAt:    snapshotAt,
		Turns:         projectTurns(msgs),
		Artifacts:     snapArtifacts,
	}, nil
}
