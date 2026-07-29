// Package assets persists multimodal asset lifecycle state.
package assets

import (
	"io"
	"time"
)

// Status is the durable lifecycle state for a multimodal asset.
type Status string

// Asset lifecycle statuses.
//
// The full 12-state set is a DEFERRED lifecycle from
// docs/superpowers/plans/2026-06-18-industrial-multimodal-asset-pipeline.md.
// Production currently emits only StatusPresigned and StatusUploaded; the
// remaining states are intentionally retained for the unbuilt asset-upload
// pipeline, so future deadcode/audit runs should treat them as known-deferred
// rather than dead. Do NOT delete any of these constants.
//
// This assets.Status lifecycle is DISTINCT from internal/documents.JobStatus
// (the wired document-ingest lifecycle: JobEmbedding, JobCanceled, etc.): the
// two share some string values ("embedding"/"canceled") but are different types
// in different packages and must not be conflated.
const (
	StatusCreated    Status = "created"
	StatusPresigned  Status = "presigned"
	StatusUploaded   Status = "uploaded"
	StatusAccepted   Status = "accepted"
	StatusProcessing Status = "processing"
	StatusSearchable Status = "searchable"
	StatusEmbedding  Status = "embedding"
	StatusComplete   Status = "complete"
	StatusFailed     Status = "failed"
	StatusRefused    Status = "refused"
	StatusDeleted    Status = "deleted"
	StatusCanceled   Status = "canceled"
)

// Modality describes the asset's content type class.
type Modality string

// Supported asset modalities.
const (
	ModalityDocument Modality = "document"
	ModalityImage    Modality = "image"
	ModalityAudio    Modality = "audio"
	ModalityUnknown  Modality = "unknown"
)

// Scope describes where the asset is visible.
type Scope string

// Supported asset scopes.
const (
	ScopeThread  Scope = "thread"
	ScopeLibrary Scope = "library"
)

// SourceKind identifies where an asset upload was initiated.
type SourceKind string

// Supported asset source kinds.
const (
	SourceWeb      SourceKind = "web"
	SourceTelegram SourceKind = "telegram"
	SourceCLI      SourceKind = "cli"
	// SourceAgent marks an asset ingested from an agent-produced deliverable (send_file →
	// Garage, WEBART-01/D-06): first-class and distinguishable from human uploads.
	SourceAgent SourceKind = "agent"
)

// Asset is the API-facing view of a persisted multimodal asset record.
type Asset struct {
	ID                string         `json:"id"`
	IdentityID        string         `json:"identity_id"`
	SourceKind        SourceKind     `json:"source_kind"`
	SourceRef         string         `json:"source_ref"`
	ThreadID          string         `json:"thread_id"`
	Scope             Scope          `json:"scope"`
	Modality          Modality       `json:"modality"`
	Status            Status         `json:"status"`
	FileName          string         `json:"file_name"`
	MIMEType          string         `json:"mime_type"`
	DeclaredSizeBytes int64          `json:"declared_size_bytes"`
	SizeBytes         int64          `json:"size_bytes"`
	ContentHash       string         `json:"content_hash"`
	ObjectBucket      string         `json:"object_bucket"`
	ObjectKey         string         `json:"object_key"`
	ObjectETag        string         `json:"object_etag"`
	DocumentID        string         `json:"document_id"`
	Summary           string         `json:"summary"`
	Metadata          map[string]any `json:"metadata"`
	ErrorCode         string         `json:"error_code"`
	ErrorMessage      string         `json:"error_message"`
	// No omitempty on the timestamps: encoding/json ignores it for struct values, so
	// it never suppressed anything and a zero time has always been serialised as
	// "0001-01-01T00:00:00Z". Dropping it is byte-identical; omitzero would instead
	// change the response shape for every not-yet-reached lifecycle stage.
	CreatedAt    time.Time `json:"created_at"`
	UploadedAt   time.Time `json:"uploaded_at"`
	AcceptedAt   time.Time `json:"accepted_at"`
	ProcessedAt  time.Time `json:"processed_at"`
	SearchableAt time.Time `json:"searchable_at"`
	CompletedAt  time.Time `json:"completed_at"`
	DeletedAt    time.Time `json:"deleted_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// CreateRequest carries the fields needed to create a presigned asset record.
type CreateRequest struct {
	IdentityID        string
	SourceKind        SourceKind
	SourceRef         string
	ThreadID          string
	Scope             Scope
	Modality          Modality
	FileName          string
	MIMEType          string
	DeclaredSizeBytes int64
	ObjectBucket      string
	ObjectKey         string
	Metadata          map[string]any
}

// Result carries processing output for an asset.
type Result struct {
	Status     Status
	DocumentID string
	Summary    string
	Metadata   map[string]any
}

// TelegramIngestRequest carries one Telegram media stream into the shared asset
// pipeline.
type TelegramIngestRequest struct {
	IdentityID string
	ThreadID   string
	ChatID     int64
	MessageID  int
	FileID     string
	FileName   string
	MIMEType   string
	Modality   Modality
	SizeBytes  int64
	Reader     io.Reader
}

// AgentIngestRequest carries one agent-produced deliverable (send_file host file) into the
// shared asset pipeline as a delivery-only, owned thread asset (WEBART-01/D-06). It mirrors
// TelegramIngestRequest minus the Telegram source-reference fields (there is no ChatID /
// MessageID / FileID / SourceRef for an agent delivery). The caller opens the host file and
// closes the Reader after ingest.
type AgentIngestRequest struct {
	IdentityID string
	ThreadID   string
	FileName   string
	MIMEType   string
	Modality   Modality
	SizeBytes  int64
	Reader     io.Reader
}
