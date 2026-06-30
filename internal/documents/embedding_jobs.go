package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

const IngestionJobTypeDocumentEmbed = "document_embed"

// IngestionJobCreator persists a new durable ingestion job.
type IngestionJobCreator interface {
	Create(context.Context, CreateIngestionJobRequest) (IngestionJob, error)
}

// DurableEmbeddingQueue persists extracted documents for asynchronous embedding workers.
type DurableEmbeddingQueue struct {
	Jobs  IngestionJobCreator
	Clock Clock
}

// Enqueue persists a document_embed job instead of spawning a process goroutine.
func (q *DurableEmbeddingQueue) Enqueue(ctx context.Context, doc ExtractedDocument) error {
	if q == nil || q.Jobs == nil {
		return fmt.Errorf("durable embedding queue has no job store")
	}
	payload, err := extractedDocumentIngestionPayload(doc)
	if err != nil {
		return err
	}
	_, err = q.Jobs.Create(ctx, CreateIngestionJobRequest{
		JobType:        IngestionJobTypeDocumentEmbed,
		Status:         ingestionJobStatusQueued,
		IdempotencyKey: IngestionJobTypeDocumentEmbed + ":" + doc.ID + ":" + doc.ContentHash,
		Stage:          "embedding",
		MaxAttempts:    5,
		NextAttemptAt:  q.now(),
		Payload:        payload,
	})
	return err
}

func (q *DurableEmbeddingQueue) now() time.Time {
	if q.Clock != nil {
		return q.Clock().UTC()
	}
	return time.Now().UTC()
}

// EmbeddingJobHandler processes a durable document_embed job.
type EmbeddingJobHandler struct {
	Worker *EmbeddingWorker
}

// HandleIngestionJob decodes the extracted document payload and embeds it.
func (h *EmbeddingJobHandler) HandleIngestionJob(ctx context.Context, job IngestionJob) error {
	if h == nil || h.Worker == nil {
		return fmt.Errorf("embedding job handler has no worker")
	}
	doc, err := extractedDocumentFromIngestionPayload(job.Payload)
	if err != nil {
		return err
	}
	return h.Worker.Process(ctx, doc)
}

func extractedDocumentIngestionPayload(doc ExtractedDocument) (map[string]any, error) {
	data, err := json.Marshal(doc)
	if err != nil {
		return nil, fmt.Errorf("embedding job payload: %w", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		return nil, fmt.Errorf("embedding job payload: %w", err)
	}
	return payload, nil
}

func extractedDocumentFromIngestionPayload(payload map[string]any) (ExtractedDocument, error) {
	if payload == nil {
		return ExtractedDocument{}, fmt.Errorf("embedding job payload is empty")
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return ExtractedDocument{}, fmt.Errorf("embedding job payload: %w", err)
	}
	var doc ExtractedDocument
	if err := json.Unmarshal(data, &doc); err != nil {
		return ExtractedDocument{}, fmt.Errorf("embedding job payload: %w", err)
	}
	if doc.ID == "" {
		return ExtractedDocument{}, fmt.Errorf("embedding job payload missing document id")
	}
	return doc, nil
}
