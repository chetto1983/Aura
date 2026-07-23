package documents

import (
	"context"
	"fmt"
	"time"
)

const (
	defaultEmbeddingBatchSize = 32
	defaultEmbeddingRetries   = 3
)

// EmbeddingIndexer stores generated embeddings for indexed chunks.
type EmbeddingIndexer interface {
	UpsertEmbeddings(ctx context.Context, documentID string, chunks []EmbeddedChunk, status JobStatus, embeddedCount int) (int, error)
}

// EmbeddingWorker asynchronously embeds document chunks after sparse indexing.
type EmbeddingWorker struct {
	Jobs       JobStore
	Generator  EmbeddingGenerator
	Indexer    EmbeddingIndexer
	BatchSize  int
	MaxRetries int
	Backoff    time.Duration
}

// Enqueue processes embedding through the worker implementation.
func (w *EmbeddingWorker) Enqueue(ctx context.Context, doc ExtractedDocument) error {
	return w.Process(ctx, doc)
}

// Process embeds and indexes one extracted document synchronously.
func (w *EmbeddingWorker) Process(ctx context.Context, doc ExtractedDocument) (err error) {
	started := time.Now()
	defer func() {
		outcome := "success"
		if err != nil {
			outcome = "error"
		}
		recordIngestionEmbedDuration(ctx, time.Since(started).Seconds(), outcome)
	}()
	if w.Generator == nil {
		return fmt.Errorf("embedding worker has no generator")
	}
	if w.Indexer == nil {
		return fmt.Errorf("embedding worker has no indexer")
	}
	if len(doc.Chunks) == 0 {
		return nil
	}
	job, hasJob := Job{}, false
	if w.Jobs != nil {
		if got, err := w.Jobs.GetByDocumentID(ctx, doc.ID); err == nil {
			job, hasJob = got, true
			job, _ = w.Jobs.UpdateProgress(ctx, job.ID, JobEmbedding, job.SparseChunks, job.EmbeddedChunks)
		}
	}
	batchSize := w.BatchSize
	if batchSize <= 0 {
		batchSize = defaultEmbeddingBatchSize
	}
	embeddedTotal := 0
	for start := 0; start < len(doc.Chunks); start += batchSize {
		end := min(start+batchSize, len(doc.Chunks))
		texts := make([]string, 0, end-start)
		for _, chunk := range doc.Chunks[start:end] {
			texts = append(texts, chunk.Text)
		}
		embeddings, err := w.embedWithRetry(ctx, texts)
		if err != nil {
			if hasJob {
				_, _ = w.Jobs.UpdateStatus(ctx, job.ID, JobSearchable, err.Error())
			}
			return err
		}
		embedded := make([]EmbeddedChunk, 0, len(embeddings))
		for j, embedding := range embeddings {
			embedded = append(embedded, EmbeddedChunk{ID: doc.Chunks[start+j].ID, Embedding: embedding})
		}
		status := JobEmbedding
		if end == len(doc.Chunks) {
			status = JobComplete
		}
		n, err := w.Indexer.UpsertEmbeddings(ctx, doc.ID, embedded, status, embeddedTotal+len(embedded))
		if err != nil {
			if hasJob {
				_, _ = w.Jobs.UpdateStatus(ctx, job.ID, JobSearchable, err.Error())
			}
			return err
		}
		embeddedTotal += n
		if hasJob {
			job, _ = w.Jobs.UpdateProgress(ctx, job.ID, status, job.SparseChunks, embeddedTotal)
		}
	}
	return nil
}

func (w *EmbeddingWorker) embedWithRetry(ctx context.Context, texts []string) ([][]float64, error) {
	retries := w.MaxRetries
	if retries <= 0 {
		retries = defaultEmbeddingRetries
	}
	backoff := w.Backoff
	if backoff <= 0 {
		backoff = 100 * time.Millisecond
	}
	var last error
	for attempt := 0; attempt < retries; attempt++ {
		embeddings, err := w.Generator.Embed(ctx, texts)
		if err == nil {
			return embeddings, nil
		}
		last = err
		timer := time.NewTimer(backoff * time.Duration(1<<attempt))
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		}
	}
	return nil, last
}
