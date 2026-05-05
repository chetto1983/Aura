// Package source manages immutable user-provided sources and generated files.
//
// Each source is keyed by a sha256-derived ID and stored under
// <wiki>/raw/<id>/ with an immutable original.<ext> plus a mutable
// source.json metadata file. OCR output and ingest results live alongside
// (ocr.md, ocr.json) but are written by other packages.
//
// Layout (PDR §4):
//
//	wiki/
//	  raw/
//	    src_<sha16>/
//	      original.pdf
//	      source.json
//	      ocr.md         (written by internal/ocr)
//	      ocr.json       (written by internal/ocr)
//	      assets/        (optional)
package source

import "time"

// Kind classifies a stored source.
type Kind string

const (
	KindPDF      Kind = "pdf"
	KindText     Kind = "text"
	KindMarkdown Kind = "markdown"
	KindJSON     Kind = "json"
	KindCSV      Kind = "csv"
	KindURL      Kind = "url"
	// KindXLSX covers XLSX files in the source store. Generated spreadsheets
	// are written as ingested artifacts; uploaded spreadsheets are normalized
	// before ingest during v1.2.
	KindXLSX Kind = "xlsx"
	// KindDOCX covers DOCX files in the source store. Generated documents are
	// written as ingested artifacts; uploaded documents are normalized before
	// ingest during v1.2.
	KindDOCX Kind = "docx"
	// KindPDFGen is an Aura-generated PDF (slice 15c). Distinct from
	// KindPDF (which marks user-uploaded PDFs that get OCR'd) so the
	// LLM never tries to ingest_source a doc that has no ocr.md and so
	// the dashboard can hide OCR-only actions cleanly.
	KindPDFGen Kind = "pdf_generated"
	// KindSandboxArtifact is a file emitted by execute_code under
	// /tmp/aura_out. It is persisted as ingested evidence but does not enter
	// the OCR pipeline because the file type is arbitrary.
	KindSandboxArtifact Kind = "sandbox_artifact"
)

// Status tracks where a source is in the OCR/ingest pipeline.
type Status string

const (
	StatusStored          Status = "stored"
	StatusExtracting      Status = "extracting"
	StatusOCRComplete     Status = "ocr_complete"
	StatusExtractComplete Status = "extract_complete"
	StatusIngested        Status = "ingested"
	StatusFailed          Status = "failed"
)

type ExtractionMeta struct {
	ExtractorName    string   `json:"extractor_name,omitempty"`
	ExtractorVersion string   `json:"extractor_version,omitempty"`
	TextBytes        int      `json:"text_bytes,omitempty"`
	PageCount        int      `json:"page_count,omitempty"`
	SheetCount       int      `json:"sheet_count,omitempty"`
	RowCount         int      `json:"row_count,omitempty"`
	Warnings         []string `json:"warnings,omitempty"`
}

// Source is the metadata record persisted as source.json. Field order matches
// PDR §4 for human readability of the on-disk file.
type Source struct {
	ID        string          `json:"id"`
	Kind      Kind            `json:"kind"`
	Filename  string          `json:"filename"`
	MimeType  string          `json:"mime_type"`
	SHA256    string          `json:"sha256"`
	SizeBytes int64           `json:"size_bytes"`
	CreatedAt time.Time       `json:"created_at"`
	Status    Status          `json:"status"`
	OCRModel  string          `json:"ocr_model,omitempty"`
	PageCount int             `json:"page_count,omitempty"`
	Extract   *ExtractionMeta `json:"extract,omitempty"`
	WikiPages []string        `json:"wiki_pages,omitempty"`
	Error     string          `json:"error,omitempty"`
}
