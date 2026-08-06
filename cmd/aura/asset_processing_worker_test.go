package main

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/identity"
)

const runtimeWorkerIdentityID = "10000000-0000-0000-0000-000000000001"

// testJobLease stands in for the production value, which is now derived from the pipeline
// stage lease rather than chosen at the worker. Its exact length is irrelevant to these
// tests — that the two horizons AGREE is what matters, and
// TestRuntimeProcessingJobWorkerLeaseMatchesStageLease asserts it.
const testJobLease = time.Minute

func TestRuntimeAssetProcessingJobWorkerProcessesClaimedAsset(t *testing.T) {
	store := &fakeRuntimeIngestionJobQueue{
		claimed: []documents.IngestionJob{{
			ID:              "job-1",
			IdentityID:      runtimeWorkerIdentityID,
			JobType:         assetProcessJobType,
			Status:          "running",
			Stage:           "accepted",
			AttemptCount:    1,
			MaxAttempts:     3,
			LeaseGeneration: 1,
			Payload: map[string]any{
				"asset_id":    "asset-1",
				"identity_id": runtimeWorkerIdentityID,
			},
		}},
	}
	processor := &recordingAssetProcessor{}
	worker := newRuntimeProcessingJobWorker(store, processor, runtimeWorkerIdentityID, "worker-3", testJobLease)

	processed, err := worker.ProcessOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d", processed)
	}
	if processor.assetID != "asset-1" || processor.identityID != runtimeWorkerIdentityID {
		t.Fatalf("processor got identity=%q asset=%q", processor.identityID, processor.assetID)
	}
	if store.claimReq.BatchSize != 1 || store.claimReq.WorkerID != "worker-3" ||
		store.claimReq.IdentityID != runtimeWorkerIdentityID {
		t.Fatalf("claim request = %#v", store.claimReq)
	}
	if len(store.statuses) != 1 || store.statuses[0].status != "succeeded" {
		t.Fatalf("status updates = %#v", store.statuses)
	}
}

// TestRuntimeProcessingJobWorkerDeadLettersRetiredEmbeddingJobs pins what happens to
// document_embed rows still queued from before chunk embedding was removed: they must
// dead-letter with handler_missing, not vanish.
func TestRuntimeProcessingJobWorkerDeadLettersRetiredEmbeddingJobs(t *testing.T) {
	store := &fakeRuntimeIngestionJobQueue{
		claimed: []documents.IngestionJob{{
			ID:              "job-1",
			IdentityID:      runtimeWorkerIdentityID,
			JobType:         "document_embed",
			Status:          "running",
			Stage:           "embedding",
			AttemptCount:    1,
			MaxAttempts:     3,
			LeaseGeneration: 1,
		}},
	}
	worker := newRuntimeProcessingJobWorker(
		store, &recordingAssetProcessor{}, runtimeWorkerIdentityID, "worker-2", testJobLease,
	)

	processed, err := worker.ProcessOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d", processed)
	}
	if len(store.statuses) != 1 || store.statuses[0].status != "dead_letter" {
		t.Fatalf("status updates = %#v", store.statuses)
	}
}

// TestRuntimeProcessingJobWorkerLeaseMatchesStageLease pins the crash-recovery defect that
// dead-lettered a healthy document on 2026-08-06.
//
// The job lease was a hardcoded 5 minutes while the pipeline STAGE lease is
// AURA_DOCUMENT_PIPELINE_LEASE_SEC (1200s / 20 minutes). When a worker died mid-convert the
// job freed itself 15 minutes before its own stage did, so every reclaim failed instantly
// with "no pipeline stage is claimable" AND burned an attempt — all five were consumed
// inside the stage-lease window and the document dead-lettered with nothing wrong with it.
//
// A job cannot progress without claiming its stage, so reclaiming it earlier buys nothing.
// The lease now comes from the same config the stage lease does. This fails against the old
// hardcoded value, which ignored whatever it was handed.
func TestRuntimeProcessingJobWorkerLeaseMatchesStageLease(t *testing.T) {
	stageLease := time.Duration(config.DefaultDocumentPipelineLeaseSec) * time.Second
	worker := newRuntimeProcessingJobWorker(
		&fakeRuntimeIngestionJobQueue{}, &recordingAssetProcessor{},
		runtimeWorkerIdentityID, "worker-lease", stageLease,
	)
	if worker.LeaseDuration != stageLease {
		t.Fatalf("job lease = %s, want the stage lease %s", worker.LeaseDuration, stageLease)
	}
	if worker.LeaseDuration < stageLease {
		t.Fatalf(
			"job lease %s is shorter than the stage lease %s — every crash mid-stage will "+
				"burn all retries on \"no pipeline stage is claimable\"",
			worker.LeaseDuration, stageLease,
		)
	}
}

func TestNewRuntimeProcessingJobWorkerWiresQueueDepthWhenStoreSupportsIt(t *testing.T) {
	store := &fakeRuntimeIngestionJobQueueWithDepth{}
	worker := newRuntimeProcessingJobWorker(
		store, &recordingAssetProcessor{}, runtimeWorkerIdentityID, "worker-1", testJobLease,
	)
	if worker.QueueDepth == nil {
		t.Fatal("QueueDepth should be wired when the store implements IngestionQueueDepthSource")
	}
}

func TestNewRuntimeProcessingJobWorkerLeavesQueueDepthNilWhenUnsupported(t *testing.T) {
	store := &fakeRuntimeIngestionJobQueue{}
	worker := newRuntimeProcessingJobWorker(
		store, &recordingAssetProcessor{}, runtimeWorkerIdentityID, "worker-1", testJobLease,
	)
	if worker.QueueDepth != nil {
		t.Fatal("QueueDepth must stay nil (fail-soft) when the store lacks CountByStatus")
	}
}

type fakeRuntimeIngestionJobQueueWithDepth struct {
	fakeRuntimeIngestionJobQueue
}

func (f *fakeRuntimeIngestionJobQueueWithDepth) CountByStatus(context.Context, string, string) (int64, error) {
	return 0, nil
}

func TestRuntimeTenantIngestionProcessorEnumeratesOwnersAndSkipsDeactivated(t *testing.T) {
	recorder := &tenantProcessorRecorder{}
	processor := &runtimeTenantIngestionProcessor{
		identities: fakeRuntimeIdentityLister{identities: []identity.Identity{
			{ID: "identity-a"}, {ID: "identity-disabled", Deactivated: true}, {ID: "identity-b"},
		}},
		width: 2,
		worker: func(identityID, workerID string) runtimeIngestionProcessor {
			return tenantRecordingProcessor{recorder: recorder, identityID: identityID, workerID: workerID}
		},
	}
	processed, err := processor.ProcessOnce(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 4 {
		t.Fatalf("processed = %d, want 4 owner-scoped slots", processed)
	}
	recorder.mu.Lock()
	defer recorder.mu.Unlock()
	if recorder.calls["identity-a"] != 2 || recorder.calls["identity-b"] != 2 ||
		recorder.calls["identity-disabled"] != 0 {
		t.Fatalf("identity calls = %#v", recorder.calls)
	}
	if len(recorder.workerIDs) == 0 || len(recorder.workerIDs) > 2 {
		t.Fatalf("worker ids = %#v, want at most fixed width 2", recorder.workerIDs)
	}
}

func TestRuntimeTenantIngestionProcessorPropagatesEnumerationAndWorkerErrors(t *testing.T) {
	want := errors.New("identity store down")
	processor := &runtimeTenantIngestionProcessor{
		identities: fakeRuntimeIdentityLister{err: want},
		worker:     func(string, string) runtimeIngestionProcessor { return &recordingRuntimeIngestionProcessor{} },
	}
	if _, err := processor.ProcessOnce(t.Context()); !errors.Is(err, want) {
		t.Fatalf("enumeration error = %v, want %v", err, want)
	}

	workerErr := errors.New("claim failed")
	processor = &runtimeTenantIngestionProcessor{
		identities: fakeRuntimeIdentityLister{identities: []identity.Identity{{ID: "identity-a"}}},
		width:      1,
		worker: func(string, string) runtimeIngestionProcessor {
			return errorRuntimeIngestionProcessor{err: workerErr}
		},
	}
	if _, err := processor.ProcessOnce(t.Context()); !errors.Is(err, workerErr) {
		t.Fatalf("worker error = %v, want %v", err, workerErr)
	}
}

func TestRuntimeIngestionWorkerStartStopProcesses(t *testing.T) {
	processor := &recordingRuntimeIngestionProcessor{done: make(chan struct{})}
	worker := newRuntimeIngestionWorker(processor, time.Hour)
	ctx := t.Context()

	worker.Start(ctx)
	select {
	case <-processor.done:
	case <-time.After(time.Second):
		t.Fatal("worker did not process")
	}
	worker.Stop()
}

type fakeRuntimeIngestionJobQueue struct {
	mu       sync.Mutex
	claimed  []documents.IngestionJob
	claimReq documents.ClaimIngestionJobsRequest
	statuses []fakeRuntimeIngestionStatus
	retries  []fakeRuntimeIngestionRetry
}

func (f *fakeRuntimeIngestionJobQueue) Claim(_ context.Context, req documents.ClaimIngestionJobsRequest) ([]documents.IngestionJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.claimReq = req
	return append([]documents.IngestionJob(nil), f.claimed...), nil
}

func (f *fakeRuntimeIngestionJobQueue) UpdateStatus(
	_ context.Context, req documents.TransitionIngestionJobRequest,
) (documents.IngestionJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.statuses = append(f.statuses, fakeRuntimeIngestionStatus{
		id: req.JobID, status: req.Status, stage: req.Stage, code: req.ErrorCode, message: req.ErrorMessage,
	})
	return documents.IngestionJob{
		ID: req.JobID, IdentityID: req.IdentityID, Status: req.Status, Stage: req.Stage,
		ErrorCode: req.ErrorCode, ErrorMessage: req.ErrorMessage,
	}, nil
}

func (f *fakeRuntimeIngestionJobQueue) Retry(
	_ context.Context, req documents.RetryIngestionJobRequest,
) (documents.IngestionJob, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.retries = append(f.retries, fakeRuntimeIngestionRetry{
		id: req.JobID, stage: req.Stage, code: req.ErrorCode,
		message: req.ErrorMessage, nextAttemptAt: req.NextAttemptAt,
	})
	return documents.IngestionJob{
		ID: req.JobID, IdentityID: req.IdentityID, Status: "queued", Stage: req.Stage,
		ErrorCode: req.ErrorCode, ErrorMessage: req.ErrorMessage, NextAttemptAt: req.NextAttemptAt,
	}, nil
}

func (f *fakeRuntimeIngestionJobQueue) Heartbeat(
	_ context.Context, req documents.HeartbeatIngestionJobRequest,
) (documents.IngestionJob, error) {
	return documents.IngestionJob{
		ID: req.JobID, IdentityID: req.IdentityID, Status: "running",
		LeaseGeneration: req.LeaseGeneration,
	}, nil
}

type fakeRuntimeIngestionStatus struct {
	id      string
	status  string
	stage   string
	code    string
	message string
}

type fakeRuntimeIngestionRetry struct {
	id            string
	stage         string
	code          string
	message       string
	nextAttemptAt time.Time
}

// blockingRuntimeIngestionProcessor models an ingestion pass wedged inside a long
// Docling conversion. Every wait is ctx-aware so a tick racing test cancellation
// cannot re-enter and leak the goroutine past Stop into goleak.
type blockingRuntimeIngestionProcessor struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (p *blockingRuntimeIngestionProcessor) ProcessOnce(ctx context.Context) (int, error) {
	p.once.Do(func() { close(p.entered) })
	select {
	case <-p.release:
		return 0, nil
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}

// Deletion is an erasure obligation, so a long ingestion pass starving it is a
// retention breach rather than a latency problem. Proven daemon-free: no Docker,
// no Postgres, only the loop composition.
func TestRuntimeProcessingWorkersDoNotStarveDeletion(t *testing.T) {
	ingestion := &blockingRuntimeIngestionProcessor{
		entered: make(chan struct{}), release: make(chan struct{}),
	}
	deletion := &recordingRuntimeIngestionProcessor{done: make(chan struct{})}
	workers := &runtimeProcessingWorkers{workers: []*runtimeIngestionWorker{
		newRuntimeIngestionWorker(ingestion, 10*time.Millisecond),
		newRuntimeIngestionWorker(deletion, 10*time.Millisecond),
	}}

	workers.Start(t.Context())
	t.Cleanup(func() { close(ingestion.release); workers.Stop() })

	select {
	case <-ingestion.entered:
	case <-time.After(2 * time.Second):
		t.Fatal("ingestion pass never started")
	}
	select {
	case <-deletion.done:
	case <-time.After(2 * time.Second):
		t.Fatal("deletion was starved by an in-flight ingestion pass")
	}
}

type recordingRuntimeIngestionProcessor struct {
	once sync.Once
	done chan struct{}
}

func (p *recordingRuntimeIngestionProcessor) ProcessOnce(context.Context) (int, error) {
	p.once.Do(func() { close(p.done) })
	return 0, nil
}

type fakeRuntimeIdentityLister struct {
	identities []identity.Identity
	err        error
}

func (f fakeRuntimeIdentityLister) ListIdentities(context.Context) ([]identity.Identity, error) {
	return append([]identity.Identity(nil), f.identities...), f.err
}

type tenantProcessorRecorder struct {
	mu        sync.Mutex
	calls     map[string]int
	workerIDs map[string]struct{}
}

type tenantRecordingProcessor struct {
	recorder   *tenantProcessorRecorder
	identityID string
	workerID   string
}

func (p tenantRecordingProcessor) ProcessOnce(context.Context) (int, error) {
	p.recorder.mu.Lock()
	defer p.recorder.mu.Unlock()
	if p.recorder.calls == nil {
		p.recorder.calls = make(map[string]int)
	}
	p.recorder.calls[p.identityID]++
	if p.recorder.workerIDs == nil {
		p.recorder.workerIDs = make(map[string]struct{})
	}
	p.recorder.workerIDs[p.workerID] = struct{}{}
	return 1, nil
}

type errorRuntimeIngestionProcessor struct{ err error }

func (p errorRuntimeIngestionProcessor) ProcessOnce(context.Context) (int, error) {
	return 0, p.err
}
