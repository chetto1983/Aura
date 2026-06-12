package documents

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestEmbeddingWorkerRetriesBatch(t *testing.T) {
	doc := testDocumentWithChunks(t, 1)
	gen := &fakeEmbeddingGenerator{failures: 1}
	worker := &EmbeddingWorker{
		Generator:  gen,
		Indexer:    &fakeEmbeddingIndexer{},
		BatchSize:  1,
		MaxRetries: 2,
		Backoff:    time.Nanosecond,
	}
	if err := worker.Process(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	if gen.calls != 2 {
		t.Fatalf("generator calls = %d", gen.calls)
	}
}

func TestEmbeddingWorkerRecordsPartialProgress(t *testing.T) {
	doc := testDocumentWithChunks(t, 3)
	jobs := newFakeJobStore()
	job, err := jobs.Create(t.Context(), CreateJobParams{
		SourceID:    doc.SourceID,
		DocumentID:  doc.ID,
		ContentHash: doc.ContentHash,
		FileName:    doc.FileName,
		Status:      JobSearchable,
	})
	if err != nil {
		t.Fatal(err)
	}
	indexer := &fakeEmbeddingIndexer{failAt: 2}
	worker := &EmbeddingWorker{
		Jobs:       jobs,
		Generator:  &fakeEmbeddingGenerator{},
		Indexer:    indexer,
		BatchSize:  2,
		MaxRetries: 1,
		Backoff:    time.Nanosecond,
	}
	err = worker.Process(t.Context(), doc)
	if err == nil || !strings.Contains(err.Error(), "index failed") {
		t.Fatalf("want index failed, got %v", err)
	}
	updated, _ := jobs.Get(t.Context(), job.ID)
	if updated.Status != JobSearchable {
		t.Fatalf("status = %s", updated.Status)
	}
	if updated.EmbeddedChunks != 2 {
		t.Fatalf("embedded chunks = %d", updated.EmbeddedChunks)
	}
}

func TestEmbeddingWorkerMarksCompleteAfterAllChunks(t *testing.T) {
	doc := testDocumentWithChunks(t, 3)
	jobs := newFakeJobStore()
	job, err := jobs.Create(t.Context(), CreateJobParams{
		SourceID:    doc.SourceID,
		DocumentID:  doc.ID,
		ContentHash: doc.ContentHash,
		FileName:    doc.FileName,
		Status:      JobSearchable,
	})
	if err != nil {
		t.Fatal(err)
	}
	worker := &EmbeddingWorker{
		Jobs:       jobs,
		Generator:  &fakeEmbeddingGenerator{},
		Indexer:    &fakeEmbeddingIndexer{},
		BatchSize:  2,
		MaxRetries: 1,
		Backoff:    time.Nanosecond,
	}
	if err := worker.Process(t.Context(), doc); err != nil {
		t.Fatal(err)
	}
	updated, _ := jobs.Get(t.Context(), job.ID)
	if updated.Status != JobComplete {
		t.Fatalf("status = %s", updated.Status)
	}
	if updated.EmbeddedChunks != 3 {
		t.Fatalf("embedded chunks = %d", updated.EmbeddedChunks)
	}
}

type fakeEmbeddingGenerator struct {
	calls    int
	failures int
}

func (f *fakeEmbeddingGenerator) Embed(_ context.Context, texts []string) ([][]float64, error) {
	f.calls++
	if f.calls <= f.failures {
		return nil, errors.New("embed failed")
	}
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = []float64{1, 2}
	}
	return out, nil
}

type fakeEmbeddingIndexer struct {
	calls  int
	failAt int
}

func (f *fakeEmbeddingIndexer) UpsertEmbeddings(_ context.Context, _ string, chunks []EmbeddedChunk, _ JobStatus, _ int) (int, error) {
	f.calls++
	if f.failAt > 0 && f.calls == f.failAt {
		return 0, errors.New("index failed")
	}
	return len(chunks), nil
}
