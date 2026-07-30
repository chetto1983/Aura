package documents

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

const (
	defaultEmbeddingBatchSize = 32
	defaultEmbeddingRetries   = 3

	// defaultEmbeddingBatchBytes caps ONE request to the embedder, because counting
	// chunks says nothing about how big the request is. A spreadsheet chunks into ~8 KB
	// rows, so a full 32-chunk batch asked for ~67k tokens of a sidecar that had 4096 and
	// answered HTTP 400 five times, dead-lettered, and left a 118-chunk document with zero
	// embeddings and a status of "ready". ~24 KB is roughly 6k tokens: comfortable inside
	// a sidecar started with -c 32768, and still 20+ prose chunks per call.
	defaultEmbeddingBatchBytes = 24000
)

// EmbeddingIndexer stores generated embeddings for indexed chunks.
type EmbeddingIndexer interface {
	UpsertEmbeddings(ctx context.Context, documentID string, chunks []EmbeddedChunk, status JobStatus, embeddedCount int) (int, error)
}

// DocumentStatusSetter narrows the catalog to the one lifecycle field the embedding
// worker owns: ready only after all vectors land, failed when they cannot.
//
// It exists because a document was marked "ready" the moment its bytes arrived, and
// nothing ever revisited that. When embedding then failed for good, the catalog still said
// ready, the cockpit still said ready, and the only symptom reached the operator as an
// agent answering a question about their own spreadsheet with unrelated rows. Optional:
// a nil setter keeps the old behaviour, so callers that do not wire it are unaffected.
//
// The id is the SEARCH document id (doc_<hex>) — the only one the worker holds. Naming it
// is not pedantry: the first implementation took the catalog's uuid instead, so every call
// failed to parse and the guarantee above never once fired in production.
type DocumentStatusSetter interface {
	SetSearchDocumentStatus(ctx context.Context, searchDocumentID string, status DocumentStatus, reason string) error
}

// EmbeddingWorker asynchronously embeds document chunks after sparse indexing.
type EmbeddingWorker struct {
	Jobs      JobStore
	Generator EmbeddingGenerator
	Indexer   EmbeddingIndexer
	// Catalog is optional; when set, it follows the terminal embedding outcome.
	Catalog DocumentStatusSetter
	// BatchSize caps chunks per request; BatchBytes caps their combined size. Whichever
	// binds first wins, and the byte cap is the one that matters for wide rows.
	BatchSize  int
	BatchBytes int
	MaxRetries int
	Backoff    time.Duration
}

func (w *EmbeddingWorker) batchByteBudget() int {
	if w.BatchBytes > 0 {
		return w.BatchBytes
	}
	return defaultEmbeddingBatchBytes
}

// batchEnd returns the exclusive end of the batch starting at start: at most maxChunks
// chunks and at most maxBytes of text. It always advances by at least one chunk, so a
// single chunk larger than the whole budget is still attempted alone rather than looping
// forever — the sidecar rejects it with a message naming the size, which is a far better
// failure than silence.
func batchEnd(chunks []Chunk, start, maxChunks, maxBytes int) int {
	end, bytes := start, 0
	for end < len(chunks) && end-start < maxChunks {
		size := len(chunks[end].Text)
		if end > start && bytes+size > maxBytes {
			break
		}
		bytes += size
		end++
	}
	return end
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
	for start := 0; start < len(doc.Chunks); {
		end := batchEnd(doc.Chunks, start, batchSize, w.batchByteBudget())
		texts := make([]string, 0, end-start)
		for _, chunk := range doc.Chunks[start:end] {
			texts = append(texts, chunk.Text)
		}
		embeddings, err := w.embedWithRetry(ctx, texts)
		if err != nil {
			if hasJob {
				_, _ = w.Jobs.UpdateStatus(ctx, job.ID, JobSearchable, err.Error())
			}
			w.markDocumentDegraded(ctx, doc.ID, err)
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
			w.markDocumentDegraded(ctx, doc.ID, err)
			return err
		}
		embeddedTotal += n
		if hasJob {
			job, _ = w.Jobs.UpdateProgress(ctx, job.ID, status, job.SparseChunks, embeddedTotal)
		}
		start = end
	}
	return w.markDocumentReady(ctx, doc.ID)
}

func (w *EmbeddingWorker) markDocumentReady(ctx context.Context, searchDocumentID string) error {
	if w.Catalog == nil {
		return nil
	}
	if err := w.Catalog.SetSearchDocumentStatus(
		context.WithoutCancel(ctx), searchDocumentID, DocumentStatusReady, "",
	); err != nil {
		return fmt.Errorf("mark document ready: %w", err)
	}
	return nil
}

// markDocumentDegraded records that this document's semantic half never landed. Failing to
// record it is swallowed on purpose: the embedding error is what the caller must see, and
// losing the status update on top of it would only replace one message with a worse one.
// Best-effort, but never silent — the WARN carries the cause either way.
func (w *EmbeddingWorker) markDocumentDegraded(ctx context.Context, searchDocumentID string, cause error) {
	reason := fmt.Sprintf("embedding failed, semantic search unavailable for this document: %v", cause)
	slog.Warn("documents: embedding failed; marking document not ready",
		"document_id", searchDocumentID, "err", cause)
	if w.Catalog == nil {
		return
	}
	// WithoutCancel: the caller's ctx is usually already cancelled by the failure that got
	// us here, and a status nobody wrote is exactly the silence this fixes.
	if err := w.Catalog.SetSearchDocumentStatus(context.WithoutCancel(ctx), searchDocumentID, DocumentStatusFailed, reason); err != nil {
		slog.Warn("documents: could not mark document failed", "document_id", searchDocumentID, "err", err)
	}
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
