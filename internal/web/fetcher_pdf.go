package web

import (
	"context"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"strings"

	"github.com/chetto1983/aura/internal/pdftext"
)

// fetcher_pdf.go is the PDF lane of web_fetch. Datasheets, manuals and decrees live
// in PDFs, and refusing the type sent the agent to answer from search snippets: on
// 2026-10-07 the RS datasheet of a Siemens servo motor answered 200 application/pdf
// and died at the allowlist, while the pdftotext already in the aura image read its
// weight, inertia and currents in 34 ms (prd.md §12).
//
// The body arrives through the same SSRF gate, size cap and deadline as every other
// lane; pdftotext reads it from a private temporary file, because poppler opens a PDF
// by seeking and a pipe cannot seek.

// renderPDF extracts every page's text, cut at maxBytes. A PDF with no text (a
// scan), bytes that are not a PDF and a host without the extractor are each a
// distinct extraction_failed, so the model can tell "try another source" from
// "this source is empty".
func renderPDF(ctx context.Context, body []byte, finalURL *url.URL, maxBytes int) (Page, error) {
	text, capped, err := extractPDFBody(ctx, body, maxBytes)
	if err != nil {
		return Page{}, pdfError(ctx, err)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return Page{}, &WebError{Code: CodeExtractionFailed, Reason: ReasonNoPDFText,
			Message: "the PDF holds no machine-readable text; it is probably a scan"}
	}
	page := Page{URL: finalURL.String(), ContentMD: text}
	if capped {
		page.Warning = WarningTruncated
	}
	return page, nil
}

func extractPDFBody(ctx context.Context, body []byte, maxBytes int) (string, bool, error) {
	file, err := os.CreateTemp("", "aura-web-*.pdf")
	if err != nil {
		return "", false, err
	}
	defer func() { _ = os.Remove(file.Name()) }()
	_, writeErr := file.Write(body)
	if closeErr := file.Close(); writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		return "", false, writeErr
	}
	return pdftext.Extract(ctx, file.Name(), 0, maxBytes)
}

func pdfError(ctx context.Context, err error) *WebError {
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return &WebError{Code: CodeExtractionFailed, Reason: ReasonNoPDFExtractor,
			Message: "no PDF text extractor is installed on this host"}
	case ctx.Err() != nil:
		return &WebError{Code: CodeTimeout, Message: "fetch timed out"}
	}
	return &WebError{Code: CodeExtractionFailed, Reason: ReasonUnreadablePDF,
		Message: "the response could not be read as a PDF"}
}
