package api

import "time"

// SourceSummary is one row of GET /sources. It omits high-volume fields
// (mime_type, sha256, size_bytes, ocr_model, error) that the table view
// doesn't need.
type SourceSummary struct {
	ID                string    `json:"id"`
	Kind              string    `json:"kind"`
	Filename          string    `json:"filename"`
	Status            string    `json:"status"`
	CreatedAt         time.Time `json:"created_at"`
	PageCount         int       `json:"page_count,omitempty"`
	WikiPages         []string  `json:"wiki_pages,omitempty"`
	MaterializedPages []string  `json:"materialized_pages,omitempty"`
}

// SourceDetail is the response of GET /sources/{id}.
type SourceDetail struct {
	ID                string    `json:"id"`
	Kind              string    `json:"kind"`
	Filename          string    `json:"filename"`
	MimeType          string    `json:"mime_type,omitempty"`
	SHA256            string    `json:"sha256"`
	SizeBytes         int64     `json:"size_bytes"`
	CreatedAt         time.Time `json:"created_at"`
	Status            string    `json:"status"`
	OCRModel          string    `json:"ocr_model,omitempty"`
	PageCount         int       `json:"page_count,omitempty"`
	WikiPages         []string  `json:"wiki_pages,omitempty"`
	MaterializedPages []string  `json:"materialized_pages,omitempty"`
	Error             string    `json:"error,omitempty"`
}

// SourceOCR is the response of GET /sources/{id}/ocr.
type SourceOCR struct {
	Markdown string `json:"markdown"`
}

// SourceMarkdown is the response of GET /sources/{id}/markdown. File is the
// on-disk generated markdown artifact ("ocr.md" or "extract.md").
type SourceMarkdown struct {
	Markdown string `json:"markdown"`
	File     string `json:"file"`
}

// BackupObject is one entry in GET /backups.
type BackupObject struct {
	Key          string    `json:"key"`
	Category     string    `json:"category"`
	Timestamp    string    `json:"timestamp,omitempty"`
	SizeBytes    int64     `json:"size_bytes"`
	LastModified time.Time `json:"last_modified"`
}

// BackupListResponse is the body of GET /backups.
type BackupListResponse struct {
	Bucket  string         `json:"bucket"`
	Objects []BackupObject `json:"objects"`
}

// BackupExportObject is one upload in a backup export response.
type BackupExportObject struct {
	Category  string `json:"category"`
	Key       string `json:"key"`
	SizeBytes int64  `json:"size_bytes"`
	Files     int    `json:"files"`
}

// BackupExportResponse is the body of POST /backups/export.
type BackupExportResponse struct {
	Bucket    string               `json:"bucket"`
	Timestamp string               `json:"timestamp"`
	Objects   []BackupExportObject `json:"objects"`
}
