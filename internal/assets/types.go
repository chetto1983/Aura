// Package assets persists multimodal asset lifecycle state.
package assets

import (
	"io"
	"time"
)

// Status is the durable lifecycle state for a multimodal asset.
type Status string

// Asset lifecycle statuses.
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
	CreatedAt         time.Time      `json:"created_at,omitempty"`
	UploadedAt        time.Time      `json:"uploaded_at,omitempty"`
	AcceptedAt        time.Time      `json:"accepted_at,omitempty"`
	ProcessedAt       time.Time      `json:"processed_at,omitempty"`
	SearchableAt      time.Time      `json:"searchable_at,omitempty"`
	CompletedAt       time.Time      `json:"completed_at,omitempty"`
	DeletedAt         time.Time      `json:"deleted_at,omitempty"`
	UpdatedAt         time.Time      `json:"updated_at,omitempty"`
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
	ChatID     int64
	MessageID  int
	FileID     string
	FileName   string
	MIMEType   string
	Modality   Modality
	SizeBytes  int64
	Reader     io.Reader
}
