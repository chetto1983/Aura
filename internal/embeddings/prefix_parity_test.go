package embeddings

import (
	"os"
	"path/filepath"
	"regexp"
	"testing"
)

// The document prefix is part of every stored vector, and it is written twice: here and in
// the Python ingest worker. Until now only a comment in chunk.py said they must agree.
func TestPythonDocumentPrefixMatchesGo(t *testing.T) {
	source, err := os.ReadFile(filepath.Join("..", "..", "services", "ingest", "chunk.py"))
	if err != nil {
		t.Fatalf("read services/ingest/chunk.py: %v", err)
	}
	match := regexp.MustCompile(`(?m)^EMBED_DOC_PREFIX = "([^"]*)"\s*$`).FindSubmatch(source)
	if match == nil {
		t.Fatal(`services/ingest/chunk.py no longer declares EMBED_DOC_PREFIX = "..." on one line`)
	}
	if got := string(match[1]); got != UntitledDocumentPrefix {
		t.Fatalf("Python EMBED_DOC_PREFIX = %q, Go UntitledDocumentPrefix = %q: stored vectors would differ by process", got, UntitledDocumentPrefix)
	}
}
