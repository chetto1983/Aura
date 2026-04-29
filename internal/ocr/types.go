// Package ocr is a Mistral Document AI OCR client.
//
// PDR §3 makes Mistral OCR the canonical PDF extractor for Aura. The client
// posts to <base_url>/ocr with a base64-encoded PDF in a data URL, and
// returns the parsed page list plus the raw response JSON for archival.
//
// What this package does NOT do:
//   - Persist anything (the caller writes ocr.md / ocr.json next to the
//     source via internal/source.Store.Path).
//   - Touch source status (the caller flips it to ocr_complete via
//     internal/source.Store.Update).
//
// Keeping those concerns out lets the package be tested in isolation against
// a fake Mistral server.
package ocr

// OCRRequest is the wire body for POST /ocr.
//
// Field set is verified against https://docs.mistral.ai/capabilities/document_ai/basic_ocr/
// (TableFormat, ExtractHeader, ExtractFooter, IncludeImageBase64 are all wire-level
// parameters; they are not Aura-only rendering hints).
type OCRRequest struct {
	Model              string   `json:"model"`
	Document           Document `json:"document"`
	IncludeImageBase64 bool     `json:"include_image_base64,omitempty"`
	// TableFormat controls how Mistral renders tables. "" = server default,
	// "markdown" or "html" are accepted.
	TableFormat string `json:"table_format,omitempty"`
	// ExtractHeader / ExtractFooter, when true, ask Mistral to detach the
	// running header/footer text into Page.Header / Page.Footer instead of
	// inlining it into Page.Markdown.
	ExtractHeader bool `json:"extract_header,omitempty"`
	ExtractFooter bool `json:"extract_footer,omitempty"`
}

// Document references the input PDF. Aura always uses inline base64 data URLs
// (PDR §3 "MVP uses base64 PDF upload"). Public URL / uploaded-file paths are
// left for a later slice.
type Document struct {
	Type        string `json:"type"`         // "document_url"
	DocumentURL string `json:"document_url"` // "data:application/pdf;base64,<b64>"
}

// OCRResponse is the parsed shape returned by /ocr. Fields not relevant to
// markdown extraction (images, tables, dimensions, hyperlinks) are tolerated
// via json.RawMessage on Page so unknown fields never break us.
type OCRResponse struct {
	Model     string `json:"model,omitempty"`
	Pages     []Page `json:"pages"`
	UsageInfo *Usage `json:"usage_info,omitempty"`
}

// Page captures the parts of a Mistral OCR page Aura needs. Index is 0-based
// per the API; rendering converts to 1-based for human display.
type Page struct {
	Index    int    `json:"index"`
	Markdown string `json:"markdown"`
	Header   string `json:"header,omitempty"`
	Footer   string `json:"footer,omitempty"`
}

// Usage is Mistral's per-call usage report.
type Usage struct {
	PagesProcessed int   `json:"pages_processed"`
	DocSizeBytes   int64 `json:"doc_size_bytes"`
}
