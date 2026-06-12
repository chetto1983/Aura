// Package documents implements Aura's document ingestion domain. Postgres owns
// job state, Neo4j owns searchable document/chunk data, and extractor sidecars
// own file-format parsing.
package documents

import "time"

// JobStatus is the durable lifecycle state for a document ingestion job.
type JobStatus string

// Document ingestion job statuses.
const (
	JobAccepted   JobStatus = "accepted"
	JobExtracting JobStatus = "extracting"
	JobSearchable JobStatus = "searchable"
	JobEmbedding  JobStatus = "embedding"
	JobComplete   JobStatus = "complete"
	JobFailed     JobStatus = "failed"
	JobRefused    JobStatus = "refused"
	JobCanceled   JobStatus = "canceled"
)

// IngestRequest describes one file ingestion request before extraction.
type IngestRequest struct {
	SourceID     string
	SourceKind   string
	OriginalPath string
	FileName     string
	MIMEType     string
	SizeBytes    int64
}

// Job is the API-facing view of a persisted document ingestion job.
type Job struct {
	ID             string    `json:"id"`
	SourceID       string    `json:"source_id"`
	SourceKind     string    `json:"source_kind"`
	DocumentID     string    `json:"document_id"`
	ContentHash    string    `json:"content_hash"`
	OriginalPath   string    `json:"original_path"`
	FileName       string    `json:"file_name"`
	MIMEType       string    `json:"mime_type"`
	SizeBytes      int64     `json:"size_bytes"`
	Status         JobStatus `json:"status"`
	SparseChunks   int       `json:"sparse_chunks"`
	EmbeddedChunks int       `json:"embedded_chunks"`
	Error          string    `json:"error,omitempty"`
	CreatedAt      time.Time `json:"created_at,omitempty"`
	UpdatedAt      time.Time `json:"updated_at,omitempty"`
	SearchableAt   time.Time `json:"searchable_at,omitempty"`
	CompletedAt    time.Time `json:"completed_at,omitempty"`
}

// CreateJobParams carries the fields needed to create a document ingestion job.
type CreateJobParams struct {
	SourceID     string
	SourceKind   string
	DocumentID   string
	ContentHash  string
	OriginalPath string
	FileName     string
	MIMEType     string
	SizeBytes    int64
	Status       JobStatus
}

// ExtractedDocument is the normalized, chunked representation sent to indexing.
type ExtractedDocument struct {
	ID          string
	SourceID    string
	SourceKind  string
	FileName    string
	MIMEType    string
	SizeBytes   int64
	ContentHash string
	Title       string
	Chunks      []Chunk
	CreatedAt   time.Time
}

// Chunk is one searchable text unit derived from an extracted document.
type Chunk struct {
	ID          string
	DocumentID  string
	SourceID    string
	ContentHash string
	ChunkHash   string
	ChunkIndex  int
	ChunkCount  int
	Kind        string
	Text        string
	Locator     Locator
	HeadingPath []string
}

// Locator identifies where a chunk came from inside the source document.
type Locator struct {
	Page      int    `json:"page,omitempty"`
	Sheet     string `json:"sheet,omitempty"`
	RowStart  int    `json:"row_start,omitempty"`
	RowEnd    int    `json:"row_end,omitempty"`
	Section   string `json:"section,omitempty"`
	Paragraph int    `json:"paragraph,omitempty"`
}

// ExtractorStats summarizes the structure and size of an extractor response.
type ExtractorStats struct {
	Pages      int `json:"pages,omitempty"`
	Sheets     int `json:"sheets,omitempty"`
	Sections   int `json:"sections,omitempty"`
	Chunks     int `json:"chunks,omitempty"`
	Characters int `json:"characters,omitempty"`
}

// ExtractorResponse is the normalized JSON payload returned by the extractor sidecar.
type ExtractorResponse struct {
	Title    string           `json:"title"`
	MIMEType string           `json:"mime_type"`
	Stats    ExtractorStats   `json:"stats"`
	Chunks   []ExtractedChunk `json:"chunks"`
}

// ExtractedChunk is one raw chunk returned by the extractor sidecar.
type ExtractedChunk struct {
	Kind        string   `json:"kind"`
	Text        string   `json:"text"`
	Locator     Locator  `json:"locator"`
	HeadingPath []string `json:"heading_path"`
}

// SearchRequest describes a sparse document search query.
type SearchRequest struct {
	Query      string
	DocumentID string
	Limit      int
}

// SearchHit is one ranked result from document search.
type SearchHit struct {
	DocumentID  string   `json:"document_id"`
	ChunkID     string   `json:"chunk_id"`
	FileName    string   `json:"file_name"`
	Score       float64  `json:"score"`
	Text        string   `json:"text"`
	Locator     Locator  `json:"locator"`
	HeadingPath []string `json:"heading_path"`
}

// EmbeddedChunk carries a chunk id with its generated vector embedding.
type EmbeddedChunk struct {
	ID        string
	Embedding []float64
}
