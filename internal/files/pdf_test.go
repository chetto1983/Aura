package files

import (
	"bytes"
	"strings"
	"testing"
)

func TestSanitizePDFFilename(t *testing.T) {
	cases := []struct{ in, want string }{
		{"report", "report.pdf"},
		{"report.pdf", "report.pdf"},
		{"path/to/file", "file.pdf"},
		{`C:\Users\evil.pdf`, "evil.pdf"},
		{"", "document.pdf"},
		{".pdf", "document.pdf"},
	}
	for _, tc := range cases {
		if got := SanitizePDFFilename(tc.in); got != tc.want {
			t.Errorf("SanitizePDFFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildPDF_HappyPath(t *testing.T) {
	body, name, err := BuildPDF(PDFSpec{
		Filename: "demo",
		Title:    "Quarterly Report",
		Blocks: []PDFBlock{
			{Kind: "paragraph", Text: "Executive summary follows."},
			{Kind: "heading", Level: 2, Text: "Highlights"},
			{Kind: "bullet", Text: "Revenue up 12%"},
			{Kind: "bullet", Text: "Two new customers signed"},
			{Kind: "table", Rows: [][]string{
				{"month", "revenue"},
				{"jan", "100"},
				{"feb", "120"},
			}},
		},
	})
	if err != nil {
		t.Fatalf("BuildPDF: %v", err)
	}
	if name != "demo.pdf" {
		t.Errorf("filename = %q, want demo.pdf", name)
	}
	// PDF magic bytes — every well-formed PDF starts with %PDF-1.x.
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Errorf("body does not start with %%PDF-: %q", string(body[:min(8, len(body))]))
	}
	if !bytes.HasSuffix(bytes.TrimSpace(body), []byte("%%EOF")) {
		t.Errorf("body does not end with %%%%EOF")
	}
}

func TestBuildPDF_TitleOnly(t *testing.T) {
	body, _, err := BuildPDF(PDFSpec{Filename: "x", Title: "Just a title"})
	if err != nil {
		t.Fatalf("BuildPDF: %v", err)
	}
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Error("title-only spec did not produce a valid PDF")
	}
}

func TestBuildPDF_RejectsEmpty(t *testing.T) {
	if _, _, err := BuildPDF(PDFSpec{Filename: "x"}); err == nil {
		t.Error("expected error for empty spec")
	}
}

func TestBuildPDF_BlockCap(t *testing.T) {
	blocks := make([]PDFBlock, MaxPDFBlocks+1)
	for i := range blocks {
		blocks[i] = PDFBlock{Kind: "paragraph", Text: "x"}
	}
	if _, _, err := BuildPDF(PDFSpec{Filename: "x", Title: "t", Blocks: blocks}); err == nil {
		t.Error("expected cap error")
	}
}

func TestBuildPDF_TableRowCap(t *testing.T) {
	rows := make([][]string, MaxPDFTableRows+1)
	for i := range rows {
		rows[i] = []string{"a"}
	}
	_, _, err := BuildPDF(PDFSpec{
		Filename: "x",
		Blocks:   []PDFBlock{{Kind: "table", Rows: rows}},
	})
	if err == nil {
		t.Error("expected cap error for too many rows")
	}
}

func TestBuildPDF_UnknownBlockKind(t *testing.T) {
	_, _, err := BuildPDF(PDFSpec{
		Filename: "x",
		Blocks:   []PDFBlock{{Kind: "rainbow", Text: "noop"}},
	})
	if err == nil {
		t.Error("expected error for unknown block kind")
	}
}

func TestBuildPDF_HeadingLevelClamped(t *testing.T) {
	// Levels outside 1..6 must clamp without erroring.
	_, _, err := BuildPDF(PDFSpec{
		Filename: "x",
		Blocks: []PDFBlock{
			{Kind: "heading", Level: 0, Text: "Top"},
			{Kind: "heading", Level: 99, Text: "Tiny"},
		},
	})
	if err != nil {
		t.Errorf("BuildPDF clamping: %v", err)
	}
}

func TestLatin1Sanitize(t *testing.T) {
	cases := []struct{ in, want string }{
		{"hello", "hello"},
		{"“quoted”", `"quoted"`},         // curly doubles → straight
		{"can’t", "can't"},               // curly single → straight
		{"em—dash", "em-dash"},           // em-dash → hyphen
		{"en–dash", "en-dash"},           // en-dash → hyphen
		{"and…", "and..."},               // ellipsis → three dots
		{"non breaking", "non breaking"}, // NBSP → space
		{"emoji \U0001F600", "emoji ?"},  // out-of-range → ?
		{"Latin-1 café", "Latin-1 café"}, // accented chars in cp1252 stay
	}
	for _, tc := range cases {
		got := latin1Sanitize(tc.in)
		if got != tc.want {
			t.Errorf("latin1Sanitize(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildPDF_NonLatin1ContentSurvives(t *testing.T) {
	// Curly quotes and an em-dash in user-supplied text would normally
	// trip fpdf's Latin-1 encoder. Sanitization should make this safe.
	body, _, err := BuildPDF(PDFSpec{
		Filename: "x",
		Title:    "It’s a “smoke” test — ok?",
	})
	if err != nil {
		t.Fatalf("BuildPDF: %v", err)
	}
	if !bytes.HasPrefix(body, []byte("%PDF-")) {
		t.Errorf("body did not produce valid PDF after sanitization")
	}
	// Find a run of text that would have only existed if curly chars
	// were converted; check it's present somewhere in the stream-ish.
	if !strings.Contains(string(body), "smoke") {
		t.Logf("note: 'smoke' text not found in raw PDF stream; PDF compresses content streams so this is informational only")
	}
}
