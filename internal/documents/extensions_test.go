package documents

import (
	"slices"
	"testing"
)

// TestSupportedDocumentExts proves the exported allowlist mirrors the internal source-of-truth
// set exactly — the assertion the assets upload layer relies on to catch ingest/upload allowlist
// drift (the bug that silently 400'd pptx/html uploads).
func TestSupportedDocumentExts(t *testing.T) {
	got := SupportedDocumentExts()
	if len(got) != len(supportedDocumentExt) {
		t.Fatalf("SupportedDocumentExts len = %d, want %d", len(got), len(supportedDocumentExt))
	}
	for _, ext := range got {
		if _, ok := supportedDocumentExt[ext]; !ok {
			t.Fatalf("SupportedDocumentExts returned %q, not in the source-of-truth set", ext)
		}
	}
	for _, want := range []string{".pdf", ".docx", ".xlsx", ".pptx", ".html"} {
		if !slices.Contains(got, want) {
			t.Fatalf("SupportedDocumentExts missing %q", want)
		}
	}
}

func TestIsSupportedDocument(t *testing.T) {
	cases := []struct {
		fileName string
		want     bool
	}{
		// Widened allowlist (single source of truth in extensions.go).
		{"report.pdf", true},
		{"slides.pptx", true},
		{"page.html", true},
		{"page.htm", true},
		{"data.csv", true},
		{"sheet.xlsx", true},
		{"macro.xlsm", true},
		{"memo.docx", true},
		{"notes.md", true},
		{"notes.markdown", true},
		{"notes.txt", true},
		{"config.json", true},
		{"feed.xml", true},
		{"book.epub", true},
		{"photo.png", true},
		{"photo.jpg", true},
		{"photo.jpeg", true},
		{"anim.gif", true},
		{"photo.webp", true},
		// Case-insensitive over the extension.
		{"REPORT.PDF", true},
		{"Slides.Pptx", true},
		// Rejected: not in the allowlist, or no extension.
		{"malware.exe", false},
		{"archive.zip", false},
		{"binary.bin", false},
		{"noext", false},
		{"", false},
		{".pdf", true}, // a bare extension still resolves via filepath.Ext
	}
	for _, tc := range cases {
		if got := isSupportedDocument(tc.fileName); got != tc.want {
			t.Errorf("isSupportedDocument(%q) = %v, want %v", tc.fileName, got, tc.want)
		}
	}
}
