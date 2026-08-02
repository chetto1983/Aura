package documents

import (
	"errors"
	"strings"
	"testing"
)

// --- ids.go error/edge paths ---

func TestContentHashPathOpensFileAndHashesContent(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "payload")
	got, err := ContentHashPath(path)
	if err != nil {
		t.Fatal(err)
	}
	// sha256("payload") is stable and deterministic.
	want := "239f59ed55e737c77147cf55ad0c1b030b6d7ee748a7426952f9b852d5a935e5"
	if got != want {
		t.Fatalf("hash = %q, want %q", got, want)
	}
}

func TestContentHashPathReturnsErrorForMissingFile(t *testing.T) {
	_, err := ContentHashPath(t.TempDir() + "/does-not-exist.pdf")
	if err == nil {
		t.Fatal("want error for missing file")
	}
}

func TestContentHashReaderPropagatesReadError(t *testing.T) {
	_, err := ContentHashReader(failingReader{err: errors.New("disk read failed")})
	if err == nil || !strings.Contains(err.Error(), "disk read failed") {
		t.Fatalf("want read error, got %v", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

// --- service.go normalizeIngestRequest MIME fallback ---

func TestServiceDefaultsMIMEToOctetStreamForUnknownExtension(t *testing.T) {
	// A .docx file with an extension mime.TypeByExtension may not know on a bare runner;
	// drive the octet-stream fallback through an extension with no MIME registration while
	// keeping it in the supported set is not possible, so assert via normalizeIngestRequest
	// directly for an unsupported-but-named path the helper still normalizes.
	got := normalizeIngestRequest(IngestRequest{}, "/tmp/data.unknownext", 7)
	if got.MIMEType != "application/octet-stream" {
		t.Fatalf("mime fallback = %q, want application/octet-stream", got.MIMEType)
	}
	if got.SourceID != "cli" || got.SourceKind != "local" || got.FileName != "data.unknownext" {
		t.Fatalf("normalized request = %#v", got)
	}
	if got.OriginalPath != "/tmp/data.unknownext" || got.SizeBytes != 7 {
		t.Fatalf("normalized request = %#v", got)
	}
}

func TestServicePreservesProvidedIngestRequestFields(t *testing.T) {
	got := normalizeIngestRequest(IngestRequest{
		SourceID:     "telegram",
		SourceKind:   "chat",
		OriginalPath: "/orig/path.pdf",
		FileName:     "custom.pdf",
		MIMEType:     "application/pdf",
	}, "/tmp/path.pdf", 99)
	if got.SourceID != "telegram" || got.SourceKind != "chat" || got.OriginalPath != "/orig/path.pdf" {
		t.Fatalf("normalized request mutated provided fields: %#v", got)
	}
	if got.FileName != "custom.pdf" || got.MIMEType != "application/pdf" || got.SizeBytes != 99 {
		t.Fatalf("normalized request = %#v", got)
	}
}
