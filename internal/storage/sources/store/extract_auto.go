package source

import (
	"context"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/storage/sources/markitdown"
)

const markitdownExtractorVersion = "markitdown_v1"

// ExtractUploadedSource extracts a non-PDF source to markdown using the
// markitdown sidecar. PDF sources go through the OCR path (internal/ocr)
// before reaching this function — callers that pass KindPDF here will
// get an error.
//
// Wave 2.9 replaced the previous ExtractGo / SandboxExtractor switch with
// a single markitdown HTTP call. The dispatch lives entirely in the
// sidecar, which inspects the data: URI MIME type and routes to the right
// converter (xlsx → openpyxl, docx → python-docx, html → readabilipy,
// epub → ebooklib, zip → recursive walk, etc.).
func ExtractUploadedSource(ctx context.Context, converter markitdown.Converter, in ExtractInput) (ExtractResult, error) {
	if in.Source == nil {
		return ExtractResult{}, fmt.Errorf("source: nil source")
	}
	if converter == nil {
		return ExtractResult{}, fmt.Errorf("source: markitdown converter not configured")
	}
	switch in.Source.Kind {
	case KindText, KindMarkdown, KindJSON, KindCSV,
		KindXLSX, KindDOCX, KindPPTX, KindEPUB, KindHTML, KindZIP:
		// All handled by markitdown.
	case KindImage:
		// Gated to Wave 2.9.5 (OCRBackend). DetectUploadFormat rejects the
		// upload, but defend in depth at the extract boundary too.
		return ExtractResult{}, fmt.Errorf("source: image extraction not supported yet (Wave 2.9.5)")
	default:
		return ExtractResult{}, fmt.Errorf("source: no markitdown route for kind %s", in.Source.Kind)
	}

	res, err := converter.Convert(ctx, markitdown.ConvertInput{
		Bytes:    in.Bytes,
		MimeType: in.Source.MimeType,
		Filename: in.Source.Filename,
	})
	if err != nil {
		return ExtractResult{}, fmt.Errorf("source: markitdown: %w", err)
	}

	md := strings.TrimRight(res.Markdown, "\n") + "\n"
	return ExtractResult{
		Markdown: md,
		Metadata: ExtractionMeta{
			ExtractorName:    extractorNameForKind(in.Source.Kind),
			ExtractorVersion: markitdownExtractorVersion,
			TextBytes:        len([]byte(md)),
			Warnings:         res.Warnings,
		},
	}, nil
}

func extractorNameForKind(k Kind) string {
	switch k {
	case KindText:
		return "markitdown_text"
	case KindMarkdown:
		return "markitdown_md"
	case KindJSON:
		return "markitdown_json"
	case KindCSV:
		return "markitdown_csv"
	case KindXLSX:
		return "markitdown_xlsx"
	case KindDOCX:
		return "markitdown_docx"
	case KindPPTX:
		return "markitdown_pptx"
	case KindEPUB:
		return "markitdown_epub"
	case KindHTML:
		return "markitdown_html"
	case KindZIP:
		return "markitdown_zip"
	default:
		return "markitdown"
	}
}
