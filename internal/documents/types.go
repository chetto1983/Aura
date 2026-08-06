// Package documents implements Aura's versioned document pipeline. Garage owns
// immutable bytes, Postgres owns lifecycle truth, and ArcadeDB is a rebuildable
// passage-retrieval projection.
package documents

import "time"

// JobStatus is the durable lifecycle state for a document ingestion job.
type JobStatus string

// Candidate registration stops at accepted; only activation publishes ready, and
// activation settles aura.documents, not this ledger. So a job this store owns only
// ever moves accepted → failed — there is no success transition to wait for, and a
// row sitting at accepted after a completed ingest is the designed resting state,
// not a stall.
//
// JobSearchable and JobComplete are no longer written by anything. They are kept
// because the store's SQL keys `searchable_at`/`completed_at` off those literals and
// older rows carry them, so this is the one place each string is named rather than
// typed twice. Do NOT reintroduce a writer without moving the whole ledger: the
// convergence deliberately made this table a registration record.
const (
	JobAccepted   JobStatus = "accepted"
	JobSearchable JobStatus = "searchable"
	JobComplete   JobStatus = "complete"
	JobFailed     JobStatus = "failed"
)

// IngestRequest describes one file ingestion request.
type IngestRequest struct {
	SourceID     string
	SourceKind   string
	OriginalPath string
	FileName     string
	MIMEType     string
	SizeBytes    int64
	// IdentityID is the owning Aura identity. It is what scopes the catalog row,
	// and therefore what makes the document findable by its owner and nobody else.
	// Empty ⇒ no catalog row is written.
	IdentityID string
}

// Job is the API-facing view of a persisted document ingestion job.
type Job struct {
	ID                 string    `json:"id"`
	IdentityID         string    `json:"identity_id"`
	AssetID            string    `json:"asset_id,omitempty"`
	CatalogDocumentID  string    `json:"catalog_document_id,omitempty"`
	VersionID          string    `json:"version_id,omitempty"`
	PipelineGeneration int64     `json:"pipeline_generation"`
	SourceID           string    `json:"source_id"`
	SourceKind         string    `json:"source_kind"`
	DocumentID         string    `json:"document_id"`
	ContentHash        string    `json:"content_hash"`
	OriginalPath       string    `json:"original_path"`
	FileName           string    `json:"file_name"`
	MIMEType           string    `json:"mime_type"`
	SizeBytes          int64     `json:"size_bytes"`
	Status             JobStatus `json:"status"`
	SparseChunks       int       `json:"sparse_chunks"`
	EmbeddedChunks     int       `json:"embedded_chunks"`
	Error              string    `json:"error,omitempty"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	SearchableAt       time.Time `json:"searchable_at"`
	CompletedAt        time.Time `json:"completed_at"`
}

// CreateJobParams carries the fields needed to create a document ingestion job.
type CreateJobParams struct {
	IdentityID         string
	AssetID            string
	CatalogDocumentID  string
	VersionID          string
	PipelineGeneration int64
	SourceID           string
	SourceKind         string
	DocumentID         string
	ContentHash        string
	OriginalPath       string
	FileName           string
	MIMEType           string
	SizeBytes          int64
	Status             JobStatus
}
