package source

import (
	"strings"
	"testing"
)

func TestExtractFromOCRMarkdownPreservesPDFEvidence(t *testing.T) {
	src := &Source{ID: "src_0123456789abcdef", Kind: KindPDF, Filename: "paper.pdf", SHA256: strings.Repeat("a", 64)}
	res := ExtractFromOCRMarkdown(src, "# Source OCR: paper.pdf\n\n## Page 1\n\nImportant daily memory fact.")
	if !strings.Contains(res.Markdown, "Important daily memory fact") {
		t.Fatalf("markdown = %q", res.Markdown)
	}
	if res.Metadata.ExtractorName != "mistral_ocr_adapter" || res.Metadata.PageCount != 1 {
		t.Fatalf("metadata = %+v", res.Metadata)
	}
}
