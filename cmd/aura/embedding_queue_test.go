package main

import (
	"context"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/documents"
)

func TestRuntimeEmbeddingQueueEnqueuesDurableJob(t *testing.T) {
	now := time.Date(2026, 6, 30, 15, 0, 0, 0, time.UTC)
	store := &recordingIngestionJobCreator{}
	queue := runtimeEmbeddingQueue{
		store: store,
		now:   func() time.Time { return now },
	}
	doc := documents.ExtractedDocument{
		ID:          "doc-1",
		SourceID:    "asset-1",
		SourceKind:  "asset",
		FileName:    "manual.pdf",
		MIMEType:    "application/pdf",
		ContentHash: "sha256",
		Chunks: []documents.Chunk{{
			ID:         "chunk-1",
			DocumentID: "doc-1",
			Text:       "hello",
		}},
	}

	if err := queue.Enqueue(context.Background(), doc); err != nil {
		t.Fatal(err)
	}
	if store.req.JobType != documents.IngestionJobTypeDocumentEmbed || store.req.Stage != "embedding" {
		t.Fatalf("job request = %#v", store.req)
	}
	if !store.req.NextAttemptAt.Equal(now) {
		t.Fatalf("next attempt = %s", store.req.NextAttemptAt)
	}
}
