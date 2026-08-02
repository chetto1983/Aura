package main

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	runtimeIngestionWorkerID          = "aura-asset-processing"
	runtimeIngestionPollInterval      = time.Second
	runtimeIngestionWorkerStopTimeout = 30 * time.Second
)

type runtimeIngestionProcessor interface {
	ProcessOnce(context.Context) (int, error)
}

type runtimeIngestionWorker struct {
	processor runtimeIngestionProcessor
	interval  time.Duration

	wg   sync.WaitGroup
	stop chan struct{}
	once sync.Once
}

func newRuntimeAssetProcessingWorker(cfg *config.Config, pool *pgxpool.Pool, assets acceptedAssetProcessor, batchSize int) *runtimeIngestionWorker {
	if pool == nil || assets == nil {
		return newRuntimeIngestionWorker(nil, 0)
	}
	return newRuntimeIngestionWorker(
		newRuntimeProcessingJobWorker(
			documents.NewPostgresIngestionJobStore(pool),
			documents.NewPostgresIngestionEventStore(pool),
			assets,
			batchSize,
		),
		runtimeIngestionPollInterval,
	)
}

// newRuntimeProcessingJobWorker dispatches durable ingestion jobs. Asset processing is
// the only job type left: the document_embed type went away with chunk embedding, and
// any row still queued under it dead-letters with handler_missing rather than silently
// disappearing.
func newRuntimeProcessingJobWorker(store documents.IngestionJobQueue, events documents.IngestionEventStore, assets acceptedAssetProcessor, batchSize int) *documents.IngestionJobWorker {
	if batchSize <= 0 {
		batchSize = 1
	}
	handlers := map[string]documents.IngestionJobHandler{
		assetProcessJobType: runtimeAssetProcessHandler{assets: assets},
	}
	worker := &documents.IngestionJobWorker{
		Store:         store,
		Events:        events,
		WorkerID:      runtimeIngestionWorkerID,
		LeaseDuration: 5 * time.Minute,
		BatchSize:     batchSize,
		RetryBackoff:  time.Minute,
		Handlers:      handlers,
	}
	if depthSource, ok := store.(documents.IngestionQueueDepthSource); ok {
		worker.QueueDepth = depthSource
	}
	return worker
}

func newRuntimeIngestionWorker(processor runtimeIngestionProcessor, interval time.Duration) *runtimeIngestionWorker {
	return &runtimeIngestionWorker{
		processor: processor,
		interval:  interval,
		stop:      make(chan struct{}),
	}
}

func (w *runtimeIngestionWorker) Start(ctx context.Context) {
	if w == nil || w.processor == nil || w.interval <= 0 {
		return
	}
	w.wg.Go(func() {
		ticker := time.NewTicker(w.interval)
		defer ticker.Stop()
		w.processOnce(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-w.stop:
				return
			case <-ticker.C:
				w.processOnce(ctx)
			}
		}
	})
}

func (w *runtimeIngestionWorker) Stop() {
	if w == nil {
		return
	}
	w.once.Do(func() { close(w.stop) })
	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(runtimeIngestionWorkerStopTimeout):
	}
}

func (w *runtimeIngestionWorker) processOnce(ctx context.Context) {
	if _, err := w.processor.ProcessOnce(ctx); err != nil {
		slog.Warn("aura serve: asset processing worker", "err", err)
	}
}
