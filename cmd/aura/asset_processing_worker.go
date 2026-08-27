package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identity"
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

type runtimeIdentityLister interface {
	ListIdentities(context.Context) ([]identity.Identity, error)
}

type runtimeIdentityProcessorFactory func(identityID, workerID string) runtimeIngestionProcessor

// runtimeTenantIngestionProcessor enumerates typed identity rows outside RLS,
// then hands each claim/mutation to an owner-scoped ingestion worker. The task
// queue is round-robin and its fixed consumer count is the actual global worker
// width, including when one identity owns every pending job.
type runtimeTenantIngestionProcessor struct {
	identities     runtimeIdentityLister
	worker         runtimeIdentityProcessorFactory
	width          int
	workerIDPrefix string
}

type runtimeIngestionWorker struct {
	processor runtimeIngestionProcessor
	interval  time.Duration

	wg   sync.WaitGroup
	stop chan struct{}
	once sync.Once
}

func newRuntimeAssetProcessingWorker(
	cfg *config.Config,
	pool *pgxpool.Pool,
	accepted acceptedAssetProcessor,
	batchSize int,
) *runtimeProcessingWorkers {
	if pool == nil || accepted == nil {
		return &runtimeProcessingWorkers{}
	}
	store := documents.NewPostgresIngestionJobStore(pool)
	ingestion := &runtimeTenantIngestionProcessor{
		identities: identity.New(pool),
		width:      batchSize,
		worker: func(identityID, workerID string) runtimeIngestionProcessor {
			return newRuntimeProcessingJobWorker(
				store, accepted, identityID, workerID, cfg.AssetProcessingLeaseDuration,
			)
		},
	}
	// Erasure no longer has a loop here. It used to get its OWN, so a long conversion
	// could not postpone an erasure claim into a retention breach -- but the convert is
	// gone with the in-process pipeline, and removing a document is now the reconciler's
	// job: CocoIndex deletes the rows projecting an object once the object leaves the
	// bucket, in the same pass that would have added it.
	//
	// This is a CONTRACT CHANGE, not a cleanup. PRD amendment #114 names durable erasure as
	// a guarantee and #118 keeps it "in full", so erasure moving from a fenced, retrying,
	// multi-store proof to eventual reconciliation has to be amended into the PRD and
	// proven by #115's delete / Garage-absence / post-delete-non-recall checks.
	return &runtimeProcessingWorkers{workers: []*runtimeIngestionWorker{
		newRuntimeIngestionWorker(ingestion, runtimeIngestionPollInterval),
	}}
}

// runtimeProcessingWorkers drives independent polling loops so a long-running pass in
// one plane cannot starve another. Stop fans out and joins concurrently, so the drain
// bound is one worker's timeout rather than their sum.
type runtimeProcessingWorkers struct {
	workers []*runtimeIngestionWorker
}

func (w *runtimeProcessingWorkers) Start(ctx context.Context) {
	if w == nil {
		return
	}
	for _, worker := range w.workers {
		worker.Start(ctx)
	}
}

func (w *runtimeProcessingWorkers) Stop() {
	if w == nil {
		return
	}
	var stopping sync.WaitGroup
	for _, worker := range w.workers {
		stopping.Go(worker.Stop)
	}
	stopping.Wait()
}

// newRuntimeProcessingJobWorker dispatches durable ingestion jobs. Asset processing is
// the only job type left: the document_embed type went away with chunk embedding, and
// any row still queued under it dead-letters with handler_missing rather than silently
// disappearing.
func newRuntimeProcessingJobWorker(
	store documents.IngestionJobQueue,
	assets acceptedAssetProcessor,
	identityID string,
	workerID string,
	leaseDuration time.Duration,
) *documents.IngestionJobWorker {
	handlers := map[string]documents.IngestionJobHandler{
		assetProcessJobType: runtimeAssetProcessHandler{assets: assets},
	}
	worker := &documents.IngestionJobWorker{
		Store:         store,
		IdentityID:    identityID,
		JobType:       assetProcessJobType,
		WorkerID:      workerID,
		LeaseDuration: leaseDuration,
		BatchSize:     1,
		RetryBackoff:  time.Minute,
		Handlers:      handlers,
	}
	if depthSource, ok := store.(documents.IngestionQueueDepthSource); ok {
		worker.QueueDepth = depthSource
	}
	return worker
}

func (p *runtimeTenantIngestionProcessor) ProcessOnce(ctx context.Context) (int, error) {
	if p == nil || p.identities == nil || p.worker == nil {
		return 0, fmt.Errorf("tenant ingestion processor is not configured")
	}
	identities, err := p.identities.ListIdentities(ctx)
	if err != nil {
		return 0, err
	}
	active := make([]identity.Identity, 0, len(identities))
	for _, candidate := range identities {
		if !candidate.Deactivated && candidate.ID != "" {
			active = append(active, candidate)
		}
	}
	if len(active) == 0 {
		return 0, nil
	}
	width := p.width
	if width <= 0 {
		width = 1
	}
	workerIDPrefix := p.workerIDPrefix
	if workerIDPrefix == "" {
		workerIDPrefix = runtimeIngestionWorkerID
	}
	tasks := make(chan string)
	type outcome struct {
		count int
		err   error
	}
	outcomes := make(chan outcome, width)
	var workers sync.WaitGroup
	for slot := 0; slot < width; slot++ {
		workerID := fmt.Sprintf("%s-%d", workerIDPrefix, slot+1)
		workers.Go(func() {
			count := 0
			var errs []error
			for identityID := range tasks {
				processed, processErr := p.worker(identityID, workerID).ProcessOnce(ctx)
				count += processed
				if processErr != nil {
					errs = append(errs, fmt.Errorf("identity %s: %w", identityID, processErr))
				}
			}
			outcomes <- outcome{count: count, err: errors.Join(errs...)}
		})
	}
	for round := 0; round < width; round++ {
		for _, candidate := range active {
			tasks <- candidate.ID
		}
	}
	close(tasks)
	workers.Wait()
	close(outcomes)
	processed := 0
	var errs []error
	for result := range outcomes {
		processed += result.count
		if result.err != nil {
			errs = append(errs, result.err)
		}
	}
	return processed, errors.Join(errs...)
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
