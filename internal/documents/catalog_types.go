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
	DocumentScopeThread  DocumentScope = "thread"
	DocumentScopeLibrary DocumentScope = "library"
)

// DocumentStatus is the logical document lifecycle state.
type DocumentStatus string

const (
	DocumentStatusDraft      DocumentStatus = "draft"
	DocumentStatusQueued     DocumentStatus = "queued"
	DocumentStatusProcessing DocumentStatus = "processing"
	DocumentStatusReady      DocumentStatus = "ready"
	DocumentStatusFailed     DocumentStatus = "failed"
	DocumentStatusDeleting   DocumentStatus = "deleting"
	DocumentStatusDeleted    DocumentStatus = "deleted"
	DocumentStatusArchived   DocumentStatus = "archived"
)

// Document is the domain view of one logical, versioned document.
type Document struct {
	ID              string         `json:"id"`
	IdentityID      string         `json:"identity_id"`
	Scope           DocumentScope  `json:"scope"`
	Title           string         `json:"title"`
	Tags            []string       `json:"tags"`
	Metadata        map[string]any `json:"metadata"`
	ActiveVersionID string         `json:"active_version_id,omitempty"`
	Status          DocumentStatus `json:"status"`
	CreatedAt       time.Time      `json:"created_at,omitempty"`
	UpdatedAt       time.Time      `json:"updated_at,omitempty"`
	DeletedAt       time.Time      `json:"deleted_at,omitempty"`
}

// DocumentSummary is returned by catalog list calls.
type DocumentSummary = Document

// DocumentVersion summarizes an immutable content version.
type DocumentVersion struct {
	ID                 string    `json:"id"`
	DocumentID         string    `json:"document_id"`
	AssetID            string    `json:"asset_id,omitempty"`
	VersionNumber      int       `json:"version_number"`
	Status             string    `json:"status"`
	SHA1               string    `json:"sha1,omitempty"`
	SHA256             string    `json:"sha256"`
	ContentType        string    `json:"content_type"`
	SizeBytes          int64     `json:"size_bytes"`
	StorageObjectID    string    `json:"storage_object_id"`
	ChunkingConfigHash string    `json:"chunking_config_hash,omitempty"`
	PipelineConfigHash string    `json:"pipeline_config_hash,omitempty"`
	CreatedAt          time.Time `json:"created_at,omitempty"`
	UpdatedAt          time.Time `json:"updated_at,omitempty"`
}

// DocumentDetail returns a document with related control-plane records.
type DocumentDetail struct {
	Document Document          `json:"document"`
	Versions []DocumentVersion `json:"versions"`
}

// CreateDocumentRequest creates a logical document before or alongside its first version.
type CreateDocumentRequest struct {
	IdentityID string
	Scope      DocumentScope
	Title      string
	Tags       []string
	Metadata   map[string]any
	Status     DocumentStatus
}

// UpdateDocumentRequest updates version-independent document metadata.
type UpdateDocumentRequest struct {
	IdentityID      string
	DocumentID      string
	Scope           DocumentScope
	Title           string
	Tags            []string
	Metadata        map[string]any
	ActiveVersionID string
	Status          DocumentStatus
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
