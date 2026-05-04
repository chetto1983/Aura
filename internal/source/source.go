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
	KindPDF  Kind = "pdf"
	KindText Kind = "text"
	KindURL  Kind = "url"
	// KindXLSX is an Aura-generated spreadsheet (slice 15a). Persisted in
	// the same raw/<id>/ layout but never OCR'd; status stays "ingested"
	// because there's no compile step to run.
	KindXLSX Kind = "xlsx"
	// KindDOCX is an Aura-generated Word document (slice 15b). Same
	// generated-artifact lifecycle as KindXLSX.
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
	StatusStored      Status = "stored"
	StatusOCRComplete Status = "ocr_complete"
	StatusIngested    Status = "ingested"
	StatusFailed      Status = "failed"
)

// Source is the metadata record persisted as source.json. Field order matches
// PDR §4 for human readability of the on-disk file.
type Source struct {
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	Filename  string    `json:"filename"`
	MimeType  string    `json:"mime_type"`
	SHA256    string    `json:"sha256"`
	SizeBytes int64     `json:"size_bytes"`
	CreatedAt time.Time `json:"created_at"`
	Status    Status    `json:"status"`
	OCRModel  string    `json:"ocr_model,omitempty"`
	PageCount int       `json:"page_count,omitempty"`
	WikiPages []string  `json:"wiki_pages,omitempty"`
	Error     string    `json:"error,omitempty"`
}
