package files

import (
	"archive/zip"
	"bytes"
	"io"
	"strings"
	"testing"
)

// readDOCXPart unzips body and returns the named part's content; helper
// for tests that want to assert on the underlying OOXML markup.
func readDOCXPart(t *testing.T, body []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		t.Fatalf("zip.NewReader: %v", err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, err := f.Open()
			if err != nil {
				t.Fatalf("open %s: %v", name, err)
			}
			defer rc.Close()
			b, err := io.ReadAll(rc)
			if err != nil {
				t.Fatalf("read %s: %v", name, err)
			}
			return string(b)
		}
	}
	t.Fatalf("part %q not found in docx; have %d parts", name, len(zr.File))
	return ""
}

func TestSanitizeDOCXFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"report", "report.docx"},
		{"report.docx", "report.docx"},
		{"REPORT.DOCX", "REPORT.DOCX"},
		{"path/to/file", "file.docx"},
		{`C:\Users\evil.docx`, "evil.docx"},
		{"", "document.docx"},
		{".docx", "document.docx"},
	}
	for _, tc := range cases {
		if got := SanitizeDOCXFilename(tc.in); got != tc.want {
			t.Errorf("SanitizeDOCXFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildDOCX_HappyPath(t *testing.T) {
	spec := DOCXSpec{
		Filename: "demo",
		Title:    "Quarterly Report",
		Blocks: []DOCXBlock{
			{Kind: "paragraph", Text: "Executive summary follows."},
			{Kind: "heading", Level: 2, Text: "Highlights"},
			{Kind: "bullet", Text: "Revenue up 12%"},
			{Kind: "bullet", Text: "Two new customers signed"},
			{Kind: "heading", Level: 2, Text: "Numbers"},
			{Kind: "table", Rows: [][]string{
				{"month", "revenue"},
				{"jan", "100"},
				{"feb", "120"},
			}},
		},
	}
	body, name, err := BuildDOCX(spec)
	if err != nil {
		t.Fatalf("BuildDOCX: %v", err)
	}
	if name != "demo.docx" {
		t.Errorf("filename = %q, want demo.docx", name)
	}
	if len(body) == 0 {
		t.Fatal("empty body")
	}

	// All three required parts must exist.
	doc := readDOCXPart(t, body, "word/document.xml")
	_ = readDOCXPart(t, body, "_rels/.rels")
	_ = readDOCXPart(t, body, "[Content_Types].xml")

	// Spot-check that the title and a body paragraph survived.
	if !strings.Contains(doc, "Quarterly Report") {
		t.Errorf("title missing from document.xml")
	}
	if !strings.Contains(doc, "Executive summary follows.") {
		t.Errorf("paragraph missing from document.xml")
	}
	// Bullet rendered with prefix.
	if !strings.Contains(doc, "• Revenue up 12%") {
		t.Errorf("bullet missing or unprefixed")
	}
	// Table cells round-tripped.
	if !strings.Contains(doc, "<w:tbl>") || !strings.Contains(doc, "feb") {
		t.Errorf("table missing or empty cells")
	}
}

func TestBuildDOCX_XMLEscape(t *testing.T) {
	body, _, err := BuildDOCX(DOCXSpec{
		Filename: "x",
		Blocks: []DOCXBlock{
			{Kind: "paragraph", Text: `tag <script> & "quote" 'apos'`},
		},
	})
	if err != nil {
		t.Fatalf("BuildDOCX: %v", err)
	}
	doc := readDOCXPart(t, body, "word/document.xml")
	// Reserved chars must not appear raw in <w:t>.
	if strings.Contains(doc, "<script>") {
		t.Errorf("raw <script> tag leaked into document.xml")
	}
	// Must contain the escaped form so consumers render the literal.
	if !strings.Contains(doc, "&lt;script&gt;") {
		t.Errorf("expected escaped <script>: %s", doc)
	}
}

func TestBuildDOCX_RejectsEmpty(t *testing.T) {
	if _, _, err := BuildDOCX(DOCXSpec{Filename: "x"}); err == nil {
		t.Error("expected error for empty spec")
	}
}

func TestBuildDOCX_TitleOnly(t *testing.T) {
	body, _, err := BuildDOCX(DOCXSpec{Filename: "x", Title: "Just a title"})
	if err != nil {
		t.Fatalf("BuildDOCX: %v", err)
	}
	doc := readDOCXPart(t, body, "word/document.xml")
	if !strings.Contains(doc, "Just a title") {
		t.Errorf("title missing")
	}
}

func TestBuildDOCX_BlockCap(t *testing.T) {
	blocks := make([]DOCXBlock, MaxDOCXBlocks+1)
	for i := range blocks {
		blocks[i] = DOCXBlock{Kind: "paragraph", Text: "x"}
	}
	if _, _, err := BuildDOCX(DOCXSpec{Filename: "x", Title: "t", Blocks: blocks}); err == nil {
		t.Error("expected cap error")
	}
}

func TestBuildDOCX_TableRowCap(t *testing.T) {
	rows := make([][]string, MaxDOCXTableRows+1)
	for i := range rows {
		rows[i] = []string{"a"}
	}
	_, _, err := BuildDOCX(DOCXSpec{
		Filename: "x",
		Blocks:   []DOCXBlock{{Kind: "table", Rows: rows}},
	})
	if err == nil {
		t.Error("expected cap error for too many rows")
	}
}

func TestBuildDOCX_UnknownBlockKind(t *testing.T) {
	_, _, err := BuildDOCX(DOCXSpec{
		Filename: "x",
		Blocks:   []DOCXBlock{{Kind: "rainbow", Text: "noop"}},
	})
	if err == nil {
		t.Error("expected error for unknown block kind")
	}
}

func TestBuildDOCX_HeadingLevelClamped(t *testing.T) {
	// Level 0 → 1, level 99 → 6. Should not error.
	body, _, err := BuildDOCX(DOCXSpec{
		Filename: "x",
		Blocks: []DOCXBlock{
			{Kind: "heading", Level: 0, Text: "Top"},
			{Kind: "heading", Level: 99, Text: "Tiny"},
		},
	})
	if err != nil {
		t.Fatalf("BuildDOCX: %v", err)
	}
	doc := readDOCXPart(t, body, "word/document.xml")
	if !strings.Contains(doc, "Heading1") {
		t.Errorf("level 0 not clamped to Heading1")
	}
	if !strings.Contains(doc, "Heading6") {
		t.Errorf("level 99 not clamped to Heading6")
	}
}
