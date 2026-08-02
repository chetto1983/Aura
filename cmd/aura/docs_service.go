package main

import (
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/jackc/pgx/v5/pgxpool"
)

// docs_service.go owns every documents.Service composition in cmd/aura. Construction is
// split from connection on purpose: db.Open stays in the callers, and the constructors
// here are pure functions of already-built collaborators.
//
// There used to be three compositions — ingest, retrieval, and their union for the CLI —
// and a structural drift test to keep them in agreement, because a half-built service does
// not error, it just quietly does less. There is one composition now: ingestion writes a
// catalog row and nothing else, and retrieval is a Postgres digest ranking that needs no
// service at all.

// documentServiceDeps carries the connected collaborators the composition shares.
type documentServiceDeps struct {
	cfg      *config.Config
	pool     *pgxpool.Pool
	maxBytes int64
}

// newDocumentIngestService composes the job ledger plus the catalog writer. The catalog
// row is not optional bookkeeping: document_search reads only aura.documents, so a file
// ingested without one is invisible to the tool whose description promises it is now
// searchable, and document_open on the returned id fails with "not catalogued".
func newDocumentIngestService(deps documentServiceDeps) *documents.Service {
	return &documents.Service{
		Jobs:     documents.NewPostgresJobStore(deps.pool),
		Catalog:  documents.NewPostgresCatalogStore(deps.pool),
		MaxBytes: deps.maxBytes,
	}
}

// documentMaxBytes is the per-file ingest ceiling. Left at zero, documents.Service falls
// back to DefaultMaxIngestBytes (50 MiB), half the configured runtime ceiling.
func documentMaxBytes(cfg *config.Config) int64 {
	if cfg == nil {
		return 0
	}
	return int64(cfg.AssetMaxDocumentBytes)
}
