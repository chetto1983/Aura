package documents

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxDocumentTags     = 32
	maxDocumentTagRunes = 64
)

// DocumentScope controls where a logical document is visible.
type DocumentScope string

const (
	// DocumentScopeThread limits a document to one conversation thread.
	DocumentScopeThread DocumentScope = "thread"
	// DocumentScopeLibrary makes a document visible across the user's library.
	DocumentScopeLibrary DocumentScope = "library"
)

// Document is the domain view of one logical, versioned document.
type Document struct {
	ID                 string         `json:"id"`
	IdentityID         string         `json:"identity_id"`
	SourceKind         string         `json:"source_kind"`
	SourceKey          string         `json:"source_key"`
	SearchDocumentID   string         `json:"search_document_id"`
	PipelineGeneration int64          `json:"pipeline_generation"`
	Scope              DocumentScope  `json:"scope"`
	Title              string         `json:"title"`
	Tags               []string       `json:"tags"`
	Metadata           map[string]any `json:"metadata"`
	ActiveVersionID    string         `json:"active_version_id,omitempty"`
	Status             DocumentStatus `json:"status"`
	CreatedAt          time.Time      `json:"created_at"`
	UpdatedAt          time.Time      `json:"updated_at"`
	DeletedAt          time.Time      `json:"deleted_at"`
	// ActiveSizeBytes and ActiveContentType denormalize the active version's
	// storage facts onto list rows so a document catalog entry is self-sufficient
	// (size + kind) without an N+1 detail fetch. They are populated only by
	// ListDocuments; detail/create/update responses leave them zero (omitempty).
	ActiveSizeBytes   int64  `json:"active_size_bytes,omitempty"`
	ActiveContentType string `json:"active_content_type,omitempty"`
}

// DocumentSummary is returned by catalog list calls.
type DocumentSummary = Document

// DocumentVersion summarizes an immutable content version.
type DocumentVersion struct {
	ID                 string                `json:"id"`
	IdentityID         string                `json:"identity_id"`
	DocumentID         string                `json:"document_id"`
	SearchDocumentID   string                `json:"search_document_id"`
	PipelineGeneration int64                 `json:"pipeline_generation"`
	AssetID            string                `json:"asset_id,omitempty"`
	VersionNumber      int                   `json:"version_number"`
	Status             DocumentVersionStatus `json:"status"`
	SHA1               string                `json:"sha1,omitempty"`
	SHA256             string                `json:"sha256"`
	ContentType        string                `json:"content_type"`
	SizeBytes          int64                 `json:"size_bytes"`
	StorageObjectID    string                `json:"storage_object_id"`
	ChunkingConfigHash string                `json:"chunking_config_hash,omitempty"`
	PipelineConfigHash string                `json:"pipeline_config_hash,omitempty"`
	CreatedAt          time.Time             `json:"created_at"`
	UpdatedAt          time.Time             `json:"updated_at"`
}

// DocumentVersionRecord is returned after recording an asset as a logical document version.
//
// ReplayedActive reports that these bytes were already on record AND that the version
// carrying them is the document's published one, so the work behind it — conversion,
// embedding, projection — is done and need not be repeated. It deliberately does NOT
// expose bare "these bytes are known": a version can hold them while still processing,
// and a caller who skipped on that would mark an unindexed document searchable.
type DocumentVersionRecord struct {
	Document       Document        `json:"document"`
	Version        DocumentVersion `json:"version"`
	ReplayedActive bool            `json:"replayed_active"`
}

// DocumentDetail returns a document with related control-plane records.
type DocumentDetail struct {
	Document Document          `json:"document"`
	Versions []DocumentVersion `json:"versions"`
}

// CreateDocumentRequest creates a logical document before or alongside its first version.
type CreateDocumentRequest struct {
	IdentityID         string
	SourceKind         string
	SourceKey          string
	SearchDocumentID   string
	PipelineGeneration int64
	Scope              DocumentScope
	Title              string
	Tags               []string
	Metadata           map[string]any
	Status             DocumentStatus
}

// UpdateDocumentRequest updates version-independent document metadata.
type UpdateDocumentRequest struct {
	IdentityID         string
	DocumentID         string
	Scope              DocumentScope
	Title              string
	Tags               []string
	Metadata           map[string]any
	ActiveVersionID    string
	Status             DocumentStatus
	PipelineGeneration int64
}

// ListDocumentsRequest filters the operator document catalog.
type ListDocumentsRequest struct {
	IdentityID string
	Scope      DocumentScope
	Query      string
	Tag        string
	Limit      int
	Offset     int
}

// RecordAssetVersionRequest records a processed asset as a logical document version.
type RecordAssetVersionRequest struct {
	IdentityID         string
	AssetID            string
	SourceKind         string
	SourceKey          string
	Scope              DocumentScope
	Title              string
	FileName           string
	MIMEType           string
	SizeBytes          int64
	ObjectBucket       string
	ObjectKey          string
	ObjectETag         string
	SearchDocumentID   string
	JobID              string
	SparseChunks       int
	SHA1               string
	SHA256             string
	VersionNumber      int
	DocumentStatus     DocumentStatus
	VersionStatus      DocumentVersionStatus
	StorageKind        string
	RetentionClass     string
	ChunkingConfigHash string
	PipelineConfigHash string
	PipelineGeneration int64
	Metadata           map[string]any
}

// NormalizeTags canonicalizes operator-supplied document tags for stable
// storage, filtering, and display.
func NormalizeTags(tags []string) ([]string, error) {
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		normalized := strings.ToLower(strings.Join(strings.Fields(tag), " "))
		if normalized == "" {
			continue
		}
		if utf8.RuneCountInString(normalized) > maxDocumentTagRunes {
			return nil, fmt.Errorf("document tag %q exceeds %d characters", normalized, maxDocumentTagRunes)
		}
		seen[normalized] = struct{}{}
	}
	if len(seen) == 0 {
		return nil, nil
	}
	if len(seen) > maxDocumentTags {
		return nil, fmt.Errorf("document has %d tags; maximum is %d", len(seen), maxDocumentTags)
	}
	out := make([]string, 0, len(seen))
	for tag := range seen {
		out = append(out, tag)
	}
	sort.Strings(out)
	return out, nil
}
