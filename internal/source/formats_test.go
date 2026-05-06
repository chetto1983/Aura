package source

import (
	"strings"
	"testing"
)

func TestDetectUploadFormatAcceptsV12Formats(t *testing.T) {
	cases := []struct {
		name string
		mime string
		kind Kind
		raw  string
	}{
		{"paper.pdf", "application/pdf", KindPDF, "original.pdf"},
		{"notes.txt", "text/plain", KindText, "original.txt"},
		{"daily.md", "text/markdown", KindMarkdown, "original.md"},
		{"data.json", "application/json", KindJSON, "original.json"},
		{"budget.csv", "text/csv", KindCSV, "original.csv"},
		{"sheet.xlsx", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", KindXLSX, "original.xlsx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := DetectUploadFormat(tc.name, tc.mime)
			if err != nil {
				t.Fatalf("DetectUploadFormat() error = %v", err)
			}
			if got.Kind != tc.kind || got.OriginalName != tc.raw {
				t.Fatalf("format = (%s, %s), want (%s, %s)", got.Kind, got.OriginalName, tc.kind, tc.raw)
			}
		})
	}
}

func TestDetectUploadFormatRejectsDeferredDOCX(t *testing.T) {
	_, err := DetectUploadFormat("memo.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
	if err == nil || !strings.Contains(err.Error(), "unsupported file type") {
		t.Fatalf("err = %v, want unsupported file type for deferred DOCX", err)
	}
}

func TestDetectUploadFormatRejectsUnsupported(t *testing.T) {
	for _, name := range []string{"image.png", "deck.pptx", "archive.zip", "audio.mp3"} {
		t.Run(name, func(t *testing.T) {
			if _, err := DetectUploadFormat(name, "application/octet-stream"); err == nil {
				t.Fatalf("DetectUploadFormat(%q) error = nil, want rejection", name)
			}
		})
	}
}

func TestExtractionStatusesAreValid(t *testing.T) {
	for _, status := range []Status{StatusStored, StatusExtracting, StatusExtractComplete, StatusIngested, StatusFailed} {
		t.Run(string(status), func(t *testing.T) {
			if !ValidStatus(status) {
				t.Fatalf("ValidStatus(%q) = false", status)
			}
		})
	}
}
