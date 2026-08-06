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
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	runtimeIngestionWorkerID = "aura-asset-processing"
	runtimeDeleteWorkerID    = "aura-document-delete"
	// Deletion runs one consumer per pass, independent of the ingestion batch size.
	// A delete pass is a handful of tombstones plus object HEADs, so it does not need
	// ingestion's width, and inheriting it would multiply the appliance's concurrent
	// load on a shared 16-core host for no throughput gain.
	runtimeDeleteWorkerWidth          = 1
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
	router *usersandbox.SandboxRouter,
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
				store, accepted, identityID, workerID, cfg.DocumentPipeline.LeaseDuration,
			)
		},
	}
	// Deletion gets its OWN loop rather than sharing ingestion's tick. Ingestion's
	// ProcessOnce blocks until every slot returns, and a slot runs a Docling convert
	// inline for up to AURA_DOCUMENT_CONVERT_TIMEOUT_SEC; sharing a loop let one large
	// upload postpone every erasure claim for that long, which is a retention breach,
	// not just latency.
	workers := []*runtimeIngestionWorker{
		newRuntimeIngestionWorker(ingestion, runtimeIngestionPollInterval),
	}
	if deletion, ok := newRuntimeDurableDeleteTenantProcessor(cfg, pool, accepted, router); ok {
		workers = append(workers, newRuntimeIngestionWorker(deletion, runtimeIngestionPollInterval))
	} else {
		// Never fail silently: without this the whole erasure plane can be absent because
		// of a nil router or an unexpected processor type, with nothing in the log.
		slog.Warn("aura serve: durable document delete plane not wired",
			"reason", "delete worker dependencies unavailable")
	}
	return &runtimeProcessingWorkers{workers: workers}
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
		Store:      store,
		IdentityID: identityID,
		WorkerID:   workerID,
		// The JOB lease must never be shorter than the STAGE lease this job's work runs
		// under, and it is therefore derived from the same config rather than chosen here.
		//
		// It was a hardcoded 5 minutes against a 20-minute stage lease
		// (AURA_DOCUMENT_PIPELINE_LEASE_SEC, 1200s), and that gap is a guaranteed
		// dead-letter. Measured on 2026-08-06: the container was recreated mid-convert, the
		// job lease expired at +5min and was reclaimed, but the convert stage stayed leased
		// to the dead worker until +20min — so every reclaim failed instantly with
		// "no pipeline stage is claimable" AND consumed an attempt. All 5 attempts burned
		// inside the stage-lease window and a perfectly good document dead-lettered.
		//
		// Reclaiming a job earlier than its stage buys nothing: the job cannot make progress
		// without claiming the stage. Equal horizons mean one reclaim, one attempt, success.
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
