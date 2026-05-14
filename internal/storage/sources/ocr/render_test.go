package ocr

import (
	"strings"
	"testing"
)

func TestRenderMarkdownPDRLayout(t *testing.T) {
	resp := OCRResponse{
		Model: "mistral-ocr-2512+1",
		Pages: []Page{
			{Index: 0, Markdown: "# Title\n\nIntro paragraph."},
			{Index: 1, Markdown: "Body of page two."},
		},
	}
	got := RenderMarkdown(RenderMeta{SourceID: "src_abcdef0123456789", Filename: "paper.pdf"}, resp)

	wants := []string{
		"# Source OCR: paper.pdf",
		"Source ID: src_abcdef0123456789",
		"Model: mistral-ocr-2512+1",
		"## Page 1",
		"# Title",
		"Intro paragraph.",
		"## Page 2",
		"Body of page two.",
	}
	for _, w := range wants {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
	// Page 2 must come after Page 1.
	p1 := strings.Index(got, "## Page 1")
	p2 := strings.Index(got, "## Page 2")
	if p1 < 0 || p2 < 0 || p2 <= p1 {
		t.Errorf("page order wrong, p1=%d p2=%d", p1, p2)
	}
}

func TestRenderMarkdownMetaModelOverride(t *testing.T) {
	resp := OCRResponse{Model: "from-response", Pages: []Page{{Index: 0, Markdown: "x"}}}
	got := RenderMarkdown(RenderMeta{SourceID: "src_0", Filename: "a.pdf", Model: "explicit"}, resp)
	if !strings.Contains(got, "Model: explicit") {
		t.Errorf("explicit model not used: %s", got)
	}
	if strings.Contains(got, "from-response") {
		t.Errorf("response model leaked when override present: %s", got)
	}
}

func TestRenderMarkdownEmptyPagesKept(t *testing.T) {
	resp := OCRResponse{
		Pages: []Page{
			{Index: 0, Markdown: "first"},
			{Index: 1, Markdown: ""},
			{Index: 2, Markdown: "third"},
		},
	}
	got := RenderMarkdown(RenderMeta{SourceID: "src_0", Filename: "a.pdf"}, resp)
	for _, w := range []string{"## Page 1", "## Page 2", "## Page 3", "first", "third"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestRenderMarkdownAllZeroIndexFallback(t *testing.T) {
	// Defensive case: some servers return index=0 for every page.
	resp := OCRResponse{
		Pages: []Page{
			{Index: 0, Markdown: "a"},
			{Index: 0, Markdown: "b"},
			{Index: 0, Markdown: "c"},
		},
	}
	got := RenderMarkdown(RenderMeta{SourceID: "src_0", Filename: "a.pdf"}, resp)
	for _, w := range []string{"## Page 1", "## Page 2", "## Page 3"} {
		if !strings.Contains(got, w) {
			t.Errorf("missing %q in:\n%s", w, got)
		}
	}
}

func TestRenderMarkdownMissingFilename(t *testing.T) {
	got := RenderMarkdown(RenderMeta{SourceID: "src_0"}, OCRResponse{Pages: []Page{{Markdown: "x"}}})
	if !strings.Contains(got, "# Source OCR: (unknown)") {
		t.Errorf("missing filename placeholder: %s", got)
	}
}

func TestRenderMarkdownInlinesTablePlaceholders(t *testing.T) {
	resp := OCRResponse{
		Pages: []Page{{
			Index:    0,
			Markdown: "Intro paragraph.\n\n[tbl-0.md](tbl-0.md)\n\nTrailing line.",
			Tables: []Table{
				{ID: "tbl-0.md", Content: "| col | val |\n| --- | --- |\n| a | 1 |", Format: "markdown"},
			},
		}},
	}
	got := RenderMarkdown(RenderMeta{SourceID: "src_0", Filename: "x.pdf"}, resp)
	if strings.Contains(got, "[tbl-0.md](tbl-0.md)") {
		t.Errorf("placeholder still present:\n%s", got)
	}
	if !strings.Contains(got, "| col | val |") {
		t.Errorf("table content missing:\n%s", got)
	}
	// Sanity: surrounding text preserved.
	if !strings.Contains(got, "Intro paragraph.") || !strings.Contains(got, "Trailing line.") {
		t.Errorf("surrounding text dropped:\n%s", got)
	}
}

func TestRenderMarkdownEmitsHeaderAndFooter(t *testing.T) {
	resp := OCRResponse{
		Pages: []Page{{
			Index:    0,
			Markdown: "body content",
			Header:   "REG. IMPR. CUNEO 01954950042",
			Footer:   "page footer text",
		}},
	}
	got := RenderMarkdown(RenderMeta{SourceID: "src_0", Filename: "x.pdf"}, resp)
	if !strings.Contains(got, "*Header:* REG. IMPR. CUNEO 01954950042") {
		t.Errorf("header missing:\n%s", got)
	}
	if !strings.Contains(got, "*Footer:* page footer text") {
		t.Errorf("footer missing:\n%s", got)
	}
	// Header must appear before body, footer after.
	hPos := strings.Index(got, "*Header:*")
	bPos := strings.Index(got, "body content")
	fPos := strings.Index(got, "*Footer:*")
	if !(hPos > 0 && hPos < bPos && bPos < fPos) {
		t.Errorf("ordering wrong: h=%d b=%d f=%d", hPos, bPos, fPos)
	}
}

func TestRenderMarkdownIgnoresEmptyTables(t *testing.T) {
	// Defensive: an empty Table entry should not crash or insert blanks.
	resp := OCRResponse{
		Pages: []Page{{
			Index:    0,
			Markdown: "body",
			Tables:   []Table{{ID: "", Content: ""}, {ID: "tbl-x", Content: ""}},
		}},
	}
	got := RenderMarkdown(RenderMeta{SourceID: "src_0", Filename: "x.pdf"}, resp)
	if !strings.Contains(got, "body") {
		t.Errorf("body lost:\n%s", got)
	}
}

func TestRenderMarkdownMultipleTables(t *testing.T) {
	resp := OCRResponse{
		Pages: []Page{{
			Index:    0,
			Markdown: "[tbl-0.md](tbl-0.md)\n\nMiddle.\n\n[tbl-1.md](tbl-1.md)",
			Tables: []Table{
				{ID: "tbl-0.md", Content: "TABLE-A"},
				{ID: "tbl-1.md", Content: "TABLE-B"},
			},
		}},
	}
	got := RenderMarkdown(RenderMeta{SourceID: "src_0", Filename: "x.pdf"}, resp)
	if !strings.Contains(got, "TABLE-A") || !strings.Contains(got, "TABLE-B") {
		t.Errorf("tables missing:\n%s", got)
	}
	if strings.Contains(got, "[tbl-0.md]") || strings.Contains(got, "[tbl-1.md]") {
		t.Errorf("placeholders not substituted:\n%s", got)
	}
}
