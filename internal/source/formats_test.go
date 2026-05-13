package source

import "testing"

func TestDetectUploadFormatAcceptsSupportedFormats(t *testing.T) {
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
		{"memo.docx", "application/vnd.openxmlformats-officedocument.wordprocessingml.document", KindDOCX, "original.docx"},
		// Wave 2.9 — markitdown adds these formats.
		{"deck.pptx", "application/vnd.openxmlformats-officedocument.presentationml.presentation", KindPPTX, "original.pptx"},
		{"book.epub", "application/epub+zip", KindEPUB, "original.epub"},
		{"page.html", "text/html", KindHTML, "original.html"},
		{"page.htm", "text/html", KindHTML, "original.html"},
		{"bundle.zip", "application/zip", KindZIP, "original.zip"},
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

// Image formats are registered in the table but rejected at the upload
// boundary until Wave 2.9.5 wires an OCRBackend. This gate lives in
// DetectUploadFormat — flip it when 2.9.5 lands.
func TestDetectUploadFormatRejectsImagesUntil295(t *testing.T) {
	for _, name := range []string{"photo.png", "scan.jpg", "image.jpeg", "snap.webp"} {
		t.Run(name, func(t *testing.T) {
			if _, err := DetectUploadFormat(name, "image/png"); err == nil {
				t.Fatalf("DetectUploadFormat(%q) error = nil, want rejection (Wave 2.9.5 gate)", name)
			}
		})
	}
}

func TestDetectUploadFormatRejectsUnsupported(t *testing.T) {
	for _, name := range []string{"audio.mp3", "video.mp4", "binary.exe", "noext"} {
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
