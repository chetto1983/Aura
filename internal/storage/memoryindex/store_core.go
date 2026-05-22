package memoryindex

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/storage/freshness"
)

const (
	KindSource      = string(CollectionSource)
	KindArchive     = string(CollectionArchive)
	KindProposal    = string(CollectionProposal)
	KindUserMemory  = string(CollectionUserMemory)
	KindOperational = string(CollectionOperational)
)

const (
	PriorityNormal   = "normal"
	PriorityHigh     = "high"
	PriorityCritical = "critical"
)

// defaultSearchLimit caps memory-search results when the caller's Filter.Limit
// is zero. Aligned with config.DefaultToolSearchTopK (20) per Phase-F: cap
// LATENCY and COST, not CAPABILITY. The wiki/archive search is the core of
// the second-brain product; an 8-result default was a capability throttle on
// the memory layer.
const defaultSearchLimit = 20

const compactMemoryFTSCreateSQL = `
CREATE VIRTUAL TABLE compact_memory_fts
USING fts5(id UNINDEXED, kind, title, body, handle, source_id, status, entities, tags);
`

const compactMemoryProjectionID = "compact_memory_documents"

type Document struct {
	ID               string
	Kind             string
	Title            string
	Body             string
	Handle           string
	SourceID         string
	Page             int
	ChunkIndex       int
	ByteStart        int
	ByteEnd          int
	ChatID           int64
	ConversationID   int64
	ProposalID       int64
	Status           string
	Priority         string
	LastRecalledAt   time.Time
	RecallCount      int
	Entities         []string
	Tags             []string
	UpdatedAt        time.Time
	ContentHash      string
	EmbeddingModelID string
	IndexBuildID     string
	Score            float64
	ScoreExact       float64
	ScoreFTS         float64
	ScoreVector      float64
	// Freshness annotations computed at retrieval time by Store.Search.
	// StalenessSeconds is elapsed seconds since the projection was last rebuilt.
	// DegradedRead is true when pending_count > 0 or staleness exceeds threshold.
	StalenessSeconds int64
	DegradedRead     bool
}

type Filter struct {
	Kinds    []string
	ChatID   int64
	Limit    int
	SourceID string
}

type VectorReport struct {
	Collection  string
	DocsIndexed int
	VectorSize  int
}

type VectorIndex interface {
	Upsert(ctx context.Context, docs []Document) error
	Recreate(ctx context.Context, docs []Document) (VectorReport, error)
	Search(ctx context.Context, query string, filter Filter) ([]Document, error)
	Delete(ctx context.Context, docIDs []string) error
}

type Store struct {
	db     *sql.DB
	vector VectorIndex
	logger *slog.Logger
	now    func() time.Time

	// writeMu serializes the delete fan-out: SELECT-ids → Qdrant delete →
	// SQLite BeginTx-Commit. Without it a batch of N concurrent PurgeSource
	// callers (e.g. the LLM emitting N parallel delete_source tool calls in
	// one turn) race for SQLite write lock; with busy_timeout=5000 a batch
	// of >5 reliably trips SQLITE_BUSY. The lock also shields the Qdrant
	// sidecar from N parallel HTTP DELETEs that the mini-PC budget cannot
	// absorb. Read paths (Search, Get, allDocuments) DO NOT take this lock.
	writeMu sync.Mutex

	// freshnessStore tracks per-projection drift. nil = freshness disabled.
	freshnessStore *freshness.Store

	// staleThresholdSecs is the elapsed-seconds cutoff beyond which a projection
	// is considered stale. 0 falls back to 3600 (1 hour).
	staleThresholdSecs int64

	// EmbeddingModelID is the model used by this writer. When Upsert is called
	// with an empty doc.ContentHash or doc.EmbeddingModelID, these are
	// auto-populated from this field so callers don't need to stamp them
	// manually on every incremental write.
	EmbeddingModelID string
}

// SetFreshnessStore injects a freshness.Store so that ReplaceKind can call
// BumpPending when content hashes change. Safe to call before first write.
func (s *Store) SetFreshnessStore(fs *freshness.Store) {
	s.freshnessStore = fs
}

// SetStaleThresholdSecs sets the elapsed-seconds cutoff beyond which a projection
// is considered stale. n <= 0 is ignored; 3600 is the package default.
func (s *Store) SetStaleThresholdSecs(n int64) {
	if n > 0 {
		s.staleThresholdSecs = n
	}
}

func NewStore(db *sql.DB) (*Store, error) {
	return NewStoreWithVector(db, nil, nil)
}

func NewStoreWithVector(db *sql.DB, vector VectorIndex, logger *slog.Logger) (*Store, error) {
	if db == nil {
		return nil, fmt.Errorf("memoryindex: db required")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Store{
		db:     db,
		vector: vector,
		logger: logger,
		now:    func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *Store) Upsert(ctx context.Context, doc Document) error {
	if s == nil || s.db == nil {
		return fmt.Errorf("memoryindex: store unavailable")
	}
	doc.ID = strings.TrimSpace(doc.ID)
	doc.Kind = strings.TrimSpace(doc.Kind)
	doc.Body = strings.TrimSpace(doc.Body)
	if doc.ID == "" {
		return fmt.Errorf("memoryindex: document id required")
	}
	if doc.Kind == "" {
		return fmt.Errorf("memoryindex: document kind required")
	}
	if doc.Body == "" {
		return fmt.Errorf("memoryindex: document body required")
	}
	if doc.Handle == "" {
		doc.Handle = doc.ID
	}
	doc.Priority = NormalizePriority(doc.Priority)
	if doc.UpdatedAt.IsZero() {
		doc.UpdatedAt = s.now()
	}
	// Auto-fill freshness fields when the caller hasn't stamped them.
	// EmbeddingModelID is drawn from the store's writer config.
	// ContentHash encodes kind + body + model so hash comparisons detect both
	// textual drift and model changes without caller involvement.
	if doc.EmbeddingModelID == "" {
		doc.EmbeddingModelID = s.EmbeddingModelID
	}
	if doc.ContentHash == "" {
		doc.ContentHash = ContentHash(doc.Kind, doc.Body, doc.EmbeddingModelID)
	}
	entitiesJSON := stringListJSON(doc.Entities)
	tagsJSON := stringListJSON(doc.Tags)
	updatedAt := doc.UpdatedAt.UTC().Format(time.RFC3339Nano)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("memoryindex: begin upsert: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM compact_memory_fts WHERE id = ?`, doc.ID); err != nil {
		return fmt.Errorf("memoryindex: delete old fts row: %w", err)
	}
	if err := insertDocumentRow(ctx, tx, "INSERT OR REPLACE", doc, entitiesJSON, tagsJSON, updatedAt); err != nil {
		return fmt.Errorf("memoryindex: upsert document: %w", err)
	}
	if err := insertFTSRow(ctx, tx, doc); err != nil {
		return fmt.Errorf("memoryindex: upsert fts row: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("memoryindex: commit upsert: %w", err)
	}
	if s.vector != nil {
		if err := s.vector.Upsert(ctx, []Document{doc}); err != nil && s.logger != nil {
			s.logger.Warn("memoryindex: vector upsert failed", "id", doc.ID, "error", err)
		}
	}
	return nil
}
