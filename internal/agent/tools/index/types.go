// Package toolindex keeps the Qdrant tool-search collection in sync with the
// live tool registry through content-hash-based reconcile.
//
// Why this package exists (Wave 2.10.b):
//
// Before the Reconciler, Aura built the tool embedding index once at boot.
// Any change to a tool description, an input schema field, or a skill
// manifest after boot was invisible to tool_search until restart. That is
// wrong by 2026 standards — LangChain RecordManager, vector-vault, and
// incremental-indexing patterns all compute per-document content hashes
// and apply the diff on every reconcile pass.
//
// The Reconciler diffs wanted (Registry.Definitions()) vs indexed
// (tool_index_state SQLite table) on every trigger. Upsert only the
// changed/added; delete only the removed; the rest costs zero embedder
// calls. Same boot wall-clock when cold (one embed call per tool); near-
// zero wall-clock on every subsequent reconcile.
//
// Interfaces are kept narrow so the package is testable without spinning up
// a real Qdrant or embedding sidecar. The production wiring lives in
// internal/telegram/setup.go.
package toolindex

import (
	"context"

	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/qdrant"
)

// ToolProvider is the source-of-truth for "what tools should be in the
// index right now". Production implementation: *tools.Registry. Tests use
// a slice-backed fake. Definitions() is expected to be cheap (held-RLock
// snapshot) and to return a deterministic order so Reconcile reports
// remain stable across runs.
type ToolProvider interface {
	Definitions() []llm.ToolDefinition
}

// EmbeddingTextFn turns a single tool definition into the string the
// embedding model sees. Hashed alongside (name, description, schema) so a
// change to this function bumps every tool's content_hash and forces a
// full re-embed on the next reconcile — exactly the behavior we want when
// the search format itself changes.
type EmbeddingTextFn func(def llm.ToolDefinition) string

// Embedder produces a single dense vector for a single text. Production
// implementation: *search.EmbedCache.Embed, which already handles the
// SQLite-backed (content_sha, model) cache and the upstream HTTP call.
// Reconciler never embeds in batch — per-tool calls keep the cache hit
// rate maximal at the cost of one round-trip per cache miss, which is the
// right trade for 60 tools with a fast local sidecar.
type Embedder interface {
	Embed(ctx context.Context, text string) ([]float32, error)
}

// QdrantClient is the slice of internal/qdrant.Client we use. Narrowing it
// lets the test rig substitute a mock without depending on the full
// upstream surface.
type QdrantClient interface {
	Health(ctx context.Context) error
	CollectionInfo(ctx context.Context, collection string) (qdrant.CollectionInfo, error)
	CreateCollection(ctx context.Context, collection string, vectorSize int) error
	Upsert(ctx context.Context, collection string, points []qdrant.Point) error
	Delete(ctx context.Context, collection string, ids []string) error
}

// StateStore is the SQLite-backed memory of "what was last successfully
// indexed". Reconciler diffs ToolProvider.Definitions() against this set
// to compute the upsert/delete buckets. Persisted because Qdrant has no
// scroll/list API: enumerating indexed points server-side would require a
// full-collection paged scan we don't want to ship.
type StateStore interface {
	// LoadAll returns every row keyed by tool_name. Empty map (not nil) on
	// fresh DB — callers iterate via len() / range safely.
	LoadAll(ctx context.Context) (map[string]StateRow, error)
	// Upsert writes a row. Used after a successful Qdrant upsert.
	Upsert(ctx context.Context, row StateRow) error
	// Remove deletes a row by tool_name. Used after a successful Qdrant
	// delete.
	Remove(ctx context.Context, toolName string) error
}

// StateRow mirrors the tool_index_state migration in
// internal/db/migrations/. Time is RFC3339 UTC in storage; we keep it as
// time.Time at the boundary so callers don't reparse.
type StateRow struct {
	ToolName    string
	ContentHash string
	PointID     string
	EmbedModel  string
	IndexedAt   string // RFC3339 UTC
}
