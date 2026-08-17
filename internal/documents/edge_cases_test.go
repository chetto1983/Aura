package documents

import (
	"errors"
	"os"
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

func TestContentHashesReaderPropagatesReadError(t *testing.T) {
	_, err := ContentHashesReader(failingReader{err: errors.New("disk read failed")})
	if err == nil || !strings.Contains(err.Error(), "disk read failed") {
		t.Fatalf("want read error, got %v", err)
	}
}

type failingReader struct{ err error }

func (r failingReader) Read([]byte) (int, error) { return 0, r.err }

func writeNamedTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := t.TempDir() + string(os.PathSeparator) + name
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
