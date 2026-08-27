package documents

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

const testIngestionIdentityID = "20000000-0000-0000-0000-000000000001"

func TestIngestionJobWorkerMarksSucceeded(t *testing.T) {
	store := &fakeIngestionJobQueue{
		claimed: []IngestionJob{{
			ID:           "job-1",
			JobType:      "asset_process",
			Status:       "running",
			Stage:        "accepted",
			AttemptCount: 1,
			MaxAttempts:  3,
		}},
	}
	handled := false
	worker := &IngestionJobWorker{
		Store:         store,
		IdentityID:    testIngestionIdentityID,
		WorkerID:      "worker-1",
		LeaseDuration: 2 * time.Minute,
		BatchSize:     7,
		Handlers: map[string]IngestionJobHandler{
			"asset_process": IngestionJobHandlerFunc(func(_ context.Context, job IngestionJob) error {
				handled = true
				if job.ID != "job-1" {
					t.Fatalf("handled job id = %q", job.ID)
				}
				return nil
			}),
		},
	}

	processed, err := worker.ProcessOnce(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || !handled {
		t.Fatalf("processed=%d handled=%v", processed, handled)
	}
	if store.claimReq.IdentityID != testIngestionIdentityID || store.claimReq.WorkerID != "worker-1" ||
		store.claimReq.LeaseDuration != 2*time.Minute || store.claimReq.BatchSize != 7 {
		t.Fatalf("claim request = %#v", store.claimReq)
	}
	if len(store.statuses) != 1 {
		t.Fatalf("status updates = %#v", store.statuses)
	}
	got := store.statuses[0]
	if got.id != "job-1" || got.status != "succeeded" || got.stage != "accepted" || got.code != "" || got.message != "" {
		t.Fatalf("success update = %#v", got)
	}
	if got.identityID != testIngestionIdentityID || got.workerID != "worker-1" || got.leaseGeneration != 1 {
		t.Fatalf("success fence = %#v", got)
	}
	if len(store.retries) != 0 {
		t.Fatalf("unexpected retries = %#v", store.retries)
	}
}

// TestIngestionJobWorkerScopesClaimToItsOwnJobType pins the fix for a real
// cross-worker race found by driving the live delegation tracer (2026-08-27):
// a worker constructed with JobType=="" claims ANY due row regardless of
// type (Claim's job_type filter is NULL, matching everything), so an
// asset-processing worker whose Handlers map only knows "asset_process" can
// win the race for a "swarm_delegation" row and dead-letter it with
// handler_missing before the delegation claim loop ever sees it. Setting
// JobType must propagate into the store's claim request so each typed
// worker only ever competes for its own rows.
func TestIngestionJobWorkerScopesClaimToItsOwnJobType(t *testing.T) {
	store := &fakeIngestionJobQueue{}
	worker := &IngestionJobWorker{
		Store:      store,
		IdentityID: testIngestionIdentityID,
		WorkerID:   "worker-1",
		JobType:    "asset_process",
		Handlers:   map[string]IngestionJobHandler{},
	}

	if _, err := worker.ProcessOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if store.claimReq.JobType != "asset_process" {
		t.Fatalf("claim request JobType = %q, want %q", store.claimReq.JobType, "asset_process")
	}
}

func TestIngestionJobWorkerIncludesEventInAtomicTransition(t *testing.T) {
	store := &fakeIngestionJobQueue{
		claimed: []IngestionJob{{
			ID:           "10000000-0000-0000-0000-000000000001",
			JobType:      "asset_process",
			Status:       "running",
			Stage:        "accepted",
			AttemptCount: 1,
			MaxAttempts:  3,
		}},
	}
	worker := &IngestionJobWorker{
		Store:      store,
		IdentityID: testIngestionIdentityID,
		WorkerID:   "worker-1",
		Handlers: map[string]IngestionJobHandler{
			"asset_process": IngestionJobHandlerFunc(func(context.Context, IngestionJob) error { return nil }),
		},
	}

	if _, err := worker.ProcessOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(store.statuses) != 1 {
		t.Fatalf("transitions = %#v", store.statuses)
	}
	got := store.statuses[0]
	if got.id != "10000000-0000-0000-0000-000000000001" || got.eventType != "ingestion_job.succeeded" {
		t.Fatalf("atomic transition event = %#v", got)
	}
	if got.detail["stage"] != "accepted" || got.detail["job_type"] != "asset_process" {
		t.Fatalf("event detail = %#v", got.detail)
	}
}

func TestIngestionJobWorkerDoesNotDoubleCompleteAtomicActivation(t *testing.T) {
	store := &fakeIngestionJobQueue{claimed: []IngestionJob{{
		ID: "job-1", IdentityID: testIngestionIdentityID, JobType: "asset_process",
		Status: "running", Stage: "accepted", AttemptCount: 1, MaxAttempts: 3,
		LockedBy: "worker-1", LeaseGeneration: 1,
	}}}
	worker := &IngestionJobWorker{
		Store: store, IdentityID: testIngestionIdentityID, WorkerID: "worker-1",
		Handlers: map[string]IngestionJobHandler{
			"asset_process": IngestionJobHandlerFunc(func(context.Context, IngestionJob) error {
				return ErrIngestionJobCompletionCommitted
			}),
		},
	}

	processed, err := worker.ProcessOnce(t.Context())
	if err != nil || processed != 1 {
		t.Fatalf("processed=%d error=%v", processed, err)
	}
	if len(store.statuses) != 0 || len(store.retries) != 0 {
		t.Fatalf("atomic completion was written twice: statuses=%#v retries=%#v", store.statuses, store.retries)
	}
}

func TestIngestionJobWorkerRetriesHandlerFailure(t *testing.T) {
	now := time.Date(2026, 6, 30, 12, 0, 0, 0, time.UTC)
	store := &fakeIngestionJobQueue{
		claimed: []IngestionJob{{
			ID:           "job-1",
			JobType:      "asset_process",
			Status:       "running",
			Stage:        "extracting",
			AttemptCount: 2,
			MaxAttempts:  3,
		}},
	}
	worker := &IngestionJobWorker{
		Store:        store,
		IdentityID:   testIngestionIdentityID,
		WorkerID:     "worker-1",
		RetryBackoff: time.Minute,
		RetryJitter:  func(cap time.Duration) time.Duration { return cap / 2 },
		Clock:        func() time.Time { return now },
		Handlers: map[string]IngestionJobHandler{
			"asset_process": IngestionJobHandlerFunc(func(context.Context, IngestionJob) error {
				return errors.New("extractor down")
			}),
		},
	}

	processed, err := worker.ProcessOnce(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d", processed)
	}
	if len(store.retries) != 1 {
		t.Fatalf("retries = %#v", store.retries)
	}
	got := store.retries[0]
	if got.id != "job-1" || got.stage != "extracting" || got.code != "handler_failed" || got.message != "extractor down" {
		t.Fatalf("retry update = %#v", got)
	}
	if !got.nextAttemptAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("next attempt = %s", got.nextAttemptAt)
	}
	if got.identityID != testIngestionIdentityID || got.workerID != "worker-1" || got.leaseGeneration != 1 {
		t.Fatalf("retry fence = %#v", got)
	}
	if len(store.statuses) != 0 {
		t.Fatalf("unexpected terminal statuses = %#v", store.statuses)
	}
}

func TestIngestionJobWorkerDeadLettersAfterMaxAttempts(t *testing.T) {
	store := &fakeIngestionJobQueue{
		claimed: []IngestionJob{{
			ID:           "job-1",
			JobType:      "asset_process",
			Status:       "running",
			Stage:        "embedding",
			AttemptCount: 3,
			MaxAttempts:  3,
		}},
	}
	worker := &IngestionJobWorker{
		Store:      store,
		IdentityID: testIngestionIdentityID,
		WorkerID:   "worker-1",
		Handlers: map[string]IngestionJobHandler{
			"asset_process": IngestionJobHandlerFunc(func(context.Context, IngestionJob) error {
				return errors.New("embedding sidecar unavailable")
			}),
		},
	}

	processed, err := worker.ProcessOnce(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d", processed)
	}
	if len(store.statuses) != 1 {
		t.Fatalf("status updates = %#v", store.statuses)
	}
	got := store.statuses[0]
	if got.id != "job-1" || got.status != "dead_letter" || got.stage != "embedding" ||
		got.code != "handler_failed" || got.message != "embedding sidecar unavailable" {
		t.Fatalf("dead-letter update = %#v", got)
	}
	if len(store.retries) != 0 {
		t.Fatalf("unexpected retries = %#v", store.retries)
	}
}

func captureWarnLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &logs
}

func TestIngestionJobWorkerLogsWarnAndDeadLettersWhenHandlerMissing(t *testing.T) {
	store := &fakeIngestionJobQueue{
		claimed: []IngestionJob{{
			ID:           "job-1",
			JobType:      "unknown_type",
			Status:       "running",
			Stage:        "accepted",
			AttemptCount: 1,
			MaxAttempts:  3,
		}},
	}
	worker := &IngestionJobWorker{Store: store, IdentityID: testIngestionIdentityID, WorkerID: "worker-1", Handlers: map[string]IngestionJobHandler{}}
	logs := captureWarnLogs(t)

	processed, err := worker.ProcessOnce(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d", processed)
	}
	if len(store.statuses) != 1 || store.statuses[0].status != ingestionJobStatusDeadLetter || store.statuses[0].code != "handler_missing" {
		t.Fatalf("status updates = %#v", store.statuses)
	}
	logged := logs.String()
	if !strings.Contains(logged, "documents: ingestion job dead-lettered") ||
		!strings.Contains(logged, "job_id=job-1") ||
		!strings.Contains(logged, "code=handler_missing") ||
		!strings.Contains(logged, "attempts=1") {
		t.Fatalf("missing handler_missing warn log: %q", logged)
	}
}

func TestIngestionJobWorkerLogsWarnAndDeadLettersAfterMaxAttempts(t *testing.T) {
	store := &fakeIngestionJobQueue{
		claimed: []IngestionJob{{
			ID:           "job-1",
			JobType:      "asset_process",
			Status:       "running",
			Stage:        "embedding",
			AttemptCount: 3,
			MaxAttempts:  3,
		}},
	}
	worker := &IngestionJobWorker{
		Store:      store,
		IdentityID: testIngestionIdentityID,
		WorkerID:   "worker-1",
		Handlers: map[string]IngestionJobHandler{
			"asset_process": IngestionJobHandlerFunc(func(context.Context, IngestionJob) error {
				return errors.New("embedding sidecar unavailable")
			}),
		},
	}
	logs := captureWarnLogs(t)

	processed, err := worker.ProcessOnce(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d", processed)
	}
	logged := logs.String()
	if !strings.Contains(logged, "documents: ingestion job dead-lettered") ||
		!strings.Contains(logged, "code=handler_failed") ||
		!strings.Contains(logged, "attempts=3") ||
		!strings.Contains(logged, "embedding sidecar unavailable") {
		t.Fatalf("missing handler_failed warn log: %q", logged)
	}
}

func TestIngestionJobWorkerRetrySchedulingDoesNotLogWarn(t *testing.T) {
	store := &fakeIngestionJobQueue{
		claimed: []IngestionJob{{
			ID:           "job-1",
			JobType:      "asset_process",
			Status:       "running",
			Stage:        "extracting",
			AttemptCount: 1,
			MaxAttempts:  3,
		}},
	}
	worker := &IngestionJobWorker{
		Store:        store,
		IdentityID:   testIngestionIdentityID,
		WorkerID:     "worker-1",
		RetryBackoff: time.Minute,
		Handlers: map[string]IngestionJobHandler{
			"asset_process": IngestionJobHandlerFunc(func(context.Context, IngestionJob) error {
				return errors.New("transient failure")
			}),
		},
	}
	logs := captureWarnLogs(t)

	if _, err := worker.ProcessOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if logs.Len() != 0 {
		t.Fatalf("retry-scheduled path must not warn-log: %q", logs.String())
	}
	if len(store.retries) != 1 {
		t.Fatalf("retries = %#v", store.retries)
	}
}

func TestIngestionJobWorkerRecordsQueueDepthWhenSourceProvided(t *testing.T) {
	store := &fakeIngestionJobQueue{}
	depth := &fakeQueueDepthSource{count: 5}
	worker := &IngestionJobWorker{Store: store, IdentityID: testIngestionIdentityID, WorkerID: "worker-1", QueueDepth: depth, Handlers: map[string]IngestionJobHandler{}}

	if _, err := worker.ProcessOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if depth.calls != 1 || depth.identityID != testIngestionIdentityID || depth.lastStatus != ingestionJobStatusQueued {
		t.Fatalf("queue depth source = %#v", depth)
	}
}

func TestIngestionJobWorkerSkipsQueueDepthWhenSourceMissingOrErroring(t *testing.T) {
	store := &fakeIngestionJobQueue{}
	worker := &IngestionJobWorker{Store: store, IdentityID: testIngestionIdentityID, WorkerID: "worker-1", Handlers: map[string]IngestionJobHandler{}}
	if _, err := worker.ProcessOnce(t.Context()); err != nil {
		t.Fatal(err)
	}

	failing := &fakeQueueDepthSource{err: errors.New("count query failed")}
	worker.QueueDepth = failing
	if _, err := worker.ProcessOnce(t.Context()); err != nil {
		t.Fatalf("queue depth query failure must stay fail-soft, got err = %v", err)
	}
	if failing.calls != 1 {
		t.Fatalf("failing source calls = %d", failing.calls)
	}
}

func TestIngestionJobWorkerFailsClosedWithoutIdentity(t *testing.T) {
	worker := &IngestionJobWorker{Store: &fakeIngestionJobQueue{}, WorkerID: "worker-1"}
	if _, err := worker.ProcessOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "no identity") {
		t.Fatalf("ProcessOnce error = %v", err)
	}
}

func TestIngestionJobWorkerRejectsCrossIdentityClaim(t *testing.T) {
	store := &fakeIngestionJobQueue{claimed: []IngestionJob{{
		ID: "job-1", IdentityID: "30000000-0000-0000-0000-000000000001",
		JobType: "asset_process", Status: "running", LeaseGeneration: 7,
	}}}
	worker := &IngestionJobWorker{Store: store, IdentityID: testIngestionIdentityID, WorkerID: "worker-1"}
	if _, err := worker.ProcessOnce(t.Context()); err == nil || !strings.Contains(err.Error(), "unexpected identity") {
		t.Fatalf("ProcessOnce error = %v", err)
	}
	if len(store.statuses) != 0 || len(store.retries) != 0 {
		t.Fatalf("cross-owner job was mutated: statuses=%#v retries=%#v", store.statuses, store.retries)
	}
}

func TestIngestionJobWorkerHeartbeatLeaseLostCancelsHandlerAndDoesNotTransition(t *testing.T) {
	store := &fakeIngestionJobQueue{
		claimed: []IngestionJob{{
			ID: "job-1", JobType: "asset_process", Status: "running", Stage: "convert",
			AttemptCount: 1, MaxAttempts: 3, LeaseGeneration: 9,
		}},
		heartbeatErr: ErrIngestionJobLeaseLost,
	}
	worker := &IngestionJobWorker{
		Store: store, IdentityID: testIngestionIdentityID, WorkerID: "worker-1",
		LeaseDuration: 30 * time.Millisecond,
		Handlers: map[string]IngestionJobHandler{
			"asset_process": IngestionJobHandlerFunc(func(ctx context.Context, _ IngestionJob) error {
				<-ctx.Done()
				return ctx.Err()
			}),
		},
	}
	processed, err := worker.ProcessOnce(t.Context())
	if !errors.Is(err, ErrIngestionJobLeaseLost) || processed != 0 {
		t.Fatalf("ProcessOnce = (%d, %v)", processed, err)
	}
	if len(store.heartbeats) != 1 {
		t.Fatalf("heartbeats = %#v", store.heartbeats)
	}
	got := store.heartbeats[0]
	if got.IdentityID != testIngestionIdentityID || got.WorkerID != "worker-1" || got.LeaseGeneration != 9 {
		t.Fatalf("heartbeat fence = %#v", got)
	}
	if len(store.statuses) != 0 || len(store.retries) != 0 {
		t.Fatalf("lease-lost job was mutated: statuses=%#v retries=%#v", store.statuses, store.retries)
	}
}

type fakeQueueDepthSource struct {
	calls      int
	identityID string
	lastStatus string
	count      int64
	err        error
}

func (f *fakeQueueDepthSource) CountByStatus(_ context.Context, identityID, status string) (int64, error) {
	f.calls++
	f.identityID = identityID
	f.lastStatus = status
	if f.err != nil {
		return 0, f.err
	}
	return f.count, nil
}

type fakeIngestionJobQueue struct {
	claimed      []IngestionJob
	claimReq     ClaimIngestionJobsRequest
	statuses     []fakeIngestionJobStatus
	retries      []fakeIngestionJobRetry
	heartbeats   []HeartbeatIngestionJobRequest
	heartbeatErr error
}

func (f *fakeIngestionJobQueue) Claim(_ context.Context, req ClaimIngestionJobsRequest) ([]IngestionJob, error) {
	f.claimReq = req
	jobs := append([]IngestionJob(nil), f.claimed...)
	for i := range jobs {
		if jobs[i].IdentityID == "" {
			jobs[i].IdentityID = req.IdentityID
		}
		if jobs[i].LeaseGeneration == 0 {
			jobs[i].LeaseGeneration = 1
		}
	}
	return jobs, nil
}

func (f *fakeIngestionJobQueue) UpdateStatus(_ context.Context, req TransitionIngestionJobRequest) (IngestionJob, error) {
	f.statuses = append(f.statuses, fakeIngestionJobStatus{
		id: req.JobID, identityID: req.IdentityID, workerID: req.WorkerID,
		leaseGeneration: req.LeaseGeneration, status: req.Status, stage: req.Stage,
		code: req.ErrorCode, message: req.ErrorMessage, eventType: req.EventType,
		detail: req.EventDetail,
	})
	return IngestionJob{ID: req.JobID, IdentityID: req.IdentityID, Status: req.Status, Stage: req.Stage}, nil
}

func (f *fakeIngestionJobQueue) Retry(_ context.Context, req RetryIngestionJobRequest) (IngestionJob, error) {
	f.retries = append(f.retries, fakeIngestionJobRetry{
		id: req.JobID, identityID: req.IdentityID, workerID: req.WorkerID,
		leaseGeneration: req.LeaseGeneration, stage: req.Stage, code: req.ErrorCode,
		message: req.ErrorMessage, nextAttemptAt: req.NextAttemptAt,
	})
	return IngestionJob{ID: req.JobID, IdentityID: req.IdentityID, Status: "queued", Stage: req.Stage}, nil
}

func (f *fakeIngestionJobQueue) Heartbeat(_ context.Context, req HeartbeatIngestionJobRequest) (IngestionJob, error) {
	f.heartbeats = append(f.heartbeats, req)
	if f.heartbeatErr != nil {
		return IngestionJob{}, f.heartbeatErr
	}
	return IngestionJob{ID: req.JobID, IdentityID: req.IdentityID, LeaseGeneration: req.LeaseGeneration}, nil
}

type fakeIngestionJobStatus struct {
	id              string
	identityID      string
	workerID        string
	leaseGeneration int64
	status          string
	stage           string
	code            string
	message         string
	eventType       string
	detail          map[string]any
}

type fakeIngestionJobRetry struct {
	id              string
	identityID      string
	workerID        string
	leaseGeneration int64
	stage           string
	code            string
	message         string
	nextAttemptAt   time.Time
}
