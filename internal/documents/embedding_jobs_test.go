package documents

import (
	"context"
	"reflect"
	"testing"
	"time"
)

func TestDurableEmbeddingQueueEnqueuesExtractedDocument(t *testing.T) {
	doc := testDocumentWithChunks(t, 2)
	now := time.Date(2026, 6, 30, 14, 0, 0, 0, time.UTC)
	store := &recordingIngestionCreator{}
	queue := &DurableEmbeddingQueue{
		Jobs:  store,
		Clock: func() time.Time { return now },
	}

	if err := queue.Enqueue(t.Context(), doc); err != nil {
		t.Fatal(err)
	}

	req := store.req
	if req.JobType != IngestionJobTypeDocumentEmbed || req.Status != "queued" || req.Stage != "embedding" {
		t.Fatalf("job lifecycle request = %#v", req)
	}
	if req.IdempotencyKey != "document_embed:"+doc.ID+":"+doc.ContentHash || req.MaxAttempts != 5 || !req.NextAttemptAt.Equal(now) {
		t.Fatalf("job retry request = %#v", req)
	}
	got, err := extractedDocumentFromIngestionPayload(req.Payload)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, doc) {
		t.Fatalf("payload document = %#v, want %#v", got, doc)
	}
}

func TestEmbeddingJobHandlerProcessesPayload(t *testing.T) {
	doc := testDocumentWithChunks(t, 2)
	payload, err := extractedDocumentIngestionPayload(doc)
	if err != nil {
		t.Fatal(err)
	}
	indexer := &fakeEmbeddingIndexer{}
	handler := &EmbeddingJobHandler{
		Worker: &EmbeddingWorker{
			Generator:  &fakeEmbeddingGenerator{},
			Indexer:    indexer,
			BatchSize:  1,
			MaxRetries: 1,
			Backoff:    time.Nanosecond,
		},
	}

	if err := handler.HandleIngestionJob(t.Context(), IngestionJob{
		JobType: IngestionJobTypeDocumentEmbed,
		Payload: payload,
	}); err != nil {
		t.Fatal(err)
	}
	if indexer.calls != 2 {
		t.Fatalf("indexer calls = %d, want 2 batches", indexer.calls)
	}
}

type recordingIngestionCreator struct {
	req CreateIngestionJobRequest
}

func (s *recordingIngestionCreator) Create(_ context.Context, req CreateIngestionJobRequest) (IngestionJob, error) {
	s.req = req
	return IngestionJob{ID: "job-1"}, nil
}
