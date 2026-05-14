// Package reindex provides an async, slug-only background worker that
// batches wiki reindex requests with drop-newest coalescing. Producers
// (wiki.Store, internal/ingest) call Submit non-blocking; a single
// drain goroutine invokes search.WikiPageReindexer.ReindexWikiPage.
package reindex

import "time"

// Op selects the reindex operation to perform on a slug.
type Op int

const (
	// OpUpsert indicates the worker should re-read the page from disk and
	// upsert it into the vector index. Safe to drop — disk is source of truth.
	OpUpsert Op = iota
	// OpDelete indicates the worker should remove the slug from the vector index.
	OpDelete
)

func (o Op) String() string {
	if o == OpDelete {
		return "delete"
	}
	return "upsert"
}

// Job is the unit of work delivered to the reindex worker.
// Body is intentionally not carried — the worker re-reads the page from
// disk when processing, so drop-newest is safe (worker always sees the
// latest snapshot).
type Job struct {
	Slug string
	Op   Op
}

// Submitter is the producer-side boundary for the reindex worker.
// Implementations MUST NOT block; Submit returns false when the job was
// dropped due to a full queue or because the worker has been Stopped.
type Submitter interface {
	Submit(Job) bool
}

// Health is the operational snapshot surfaced to /api/health.
type Health struct {
	QueueDepth       int       // current buffered jobs awaiting drain
	Dropped          int64     // cumulative drop-newest count (queue full)
	DroppedAfterStop int64     // cumulative submit-after-stop count
	LastSuccess      time.Time // wall clock of the most recent successful Reindex
	LastError        string    // redacted message from the most recent failed Reindex; "" if none
}

// Config tunes the Worker. Zero values fall back to defaults via DefaultConfig.
type Config struct {
	QueueSize int // default 100 (D-12); configurable via env at the caller
}

// DefaultConfig returns a Config with production-safe defaults.
func DefaultConfig() Config {
	return Config{QueueSize: 100}
}
