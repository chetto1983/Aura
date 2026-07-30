package documents

import (
	"context"
	"strings"
	"testing"
	"time"
)

type recordingCatalog struct {
	calls  int
	id     string
	status DocumentStatus
	reason string
	err    error
}

func (c *recordingCatalog) SetDocumentStatus(_ context.Context, id string, status DocumentStatus, reason string) error {
	c.calls++
	c.id, c.status, c.reason = id, status, reason
	return c.err
}

func degradedWorker(gen EmbeddingGenerator, idx EmbeddingIndexer, cat DocumentStatusSetter) *EmbeddingWorker {
	return &EmbeddingWorker{
		Generator: gen, Indexer: idx, Catalog: cat,
		MaxRetries: 1, Backoff: time.Millisecond,
	}
}

func oneChunkDoc() ExtractedDocument {
	return ExtractedDocument{ID: "doc-1", Chunks: []Chunk{{ID: "c1", Text: "hello"}}}
}

// The failure this pins reached a real operator and never announced itself. A spreadsheet
// ingested, 118 chunks landed, every embedding call returned HTTP 400, the job
// dead-lettered after 5 attempts — and the document sat in the catalog as "ready". The
// first and only symptom was the agent answering a question about that spreadsheet with
// unrelated rows. A status that outlives the capability it promises is worse than an error.
func TestEmbeddingFailureMarksTheDocumentNotReady(t *testing.T) {
	t.Parallel()

	t.Run("a failed embed call marks the document", func(t *testing.T) {
		t.Parallel()
		cat := &recordingCatalog{}
		w := degradedWorker(&fakeEmbeddingGenerator{failures: 99}, &fakeEmbeddingIndexer{}, cat)

		if err := w.Process(context.Background(), oneChunkDoc()); err == nil {
			t.Fatal("Process returned nil on a failing embedder")
		}
		if cat.calls != 1 {
			t.Fatalf("SetDocumentStatus called %d times, want 1", cat.calls)
		}
		if cat.id != "doc-1" || cat.status != DocumentStatusFailed {
			t.Fatalf("marked %q as %q, want doc-1 as %q", cat.id, cat.status, DocumentStatusFailed)
		}
		if !strings.Contains(cat.reason, "semantic search unavailable") {
			t.Fatalf("reason = %q, want it to say what the operator lost", cat.reason)
		}
	})

	t.Run("a failed index write marks it too", func(t *testing.T) {
		t.Parallel()
		// Embedding succeeded but the vectors never reached the graph: same outcome for
		// the operator, so the same honesty is owed.
		cat := &recordingCatalog{}
		w := degradedWorker(&fakeEmbeddingGenerator{}, &fakeEmbeddingIndexer{failAt: 1}, cat)

		if err := w.Process(context.Background(), oneChunkDoc()); err == nil {
			t.Fatal("Process returned nil on a failing indexer")
		}
		if cat.calls != 1 || cat.status != DocumentStatusFailed {
			t.Fatalf("calls=%d status=%q, want 1 call marking it failed", cat.calls, cat.status)
		}
	})

	t.Run("success leaves the status alone", func(t *testing.T) {
		t.Parallel()
		cat := &recordingCatalog{}
		w := degradedWorker(&fakeEmbeddingGenerator{}, &fakeEmbeddingIndexer{}, cat)

		if err := w.Process(context.Background(), oneChunkDoc()); err != nil {
			t.Fatalf("Process = %v, want nil", err)
		}
		if cat.calls != 0 {
			t.Fatalf("SetDocumentStatus called %d times on success, want 0", cat.calls)
		}
	})

	t.Run("the embedding error survives a catalog that also fails", func(t *testing.T) {
		t.Parallel()
		// Replacing the real cause with a bookkeeping error would hide the very thing the
		// operator has to fix.
		cat := &recordingCatalog{err: context.DeadlineExceeded}
		w := degradedWorker(&fakeEmbeddingGenerator{failures: 99}, &fakeEmbeddingIndexer{}, cat)

		err := w.Process(context.Background(), oneChunkDoc())
		if err == nil || !strings.Contains(err.Error(), "embed failed") {
			t.Fatalf("Process = %v, want the embedding cause, not the catalog's", err)
		}
	})

	t.Run("a nil catalog is not a crash", func(t *testing.T) {
		t.Parallel()
		w := degradedWorker(&fakeEmbeddingGenerator{failures: 99}, &fakeEmbeddingIndexer{}, nil)
		if err := w.Process(context.Background(), oneChunkDoc()); err == nil {
			t.Fatal("Process returned nil on a failing embedder")
		}
	})
}
