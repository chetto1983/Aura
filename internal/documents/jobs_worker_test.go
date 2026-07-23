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
	if store.claimReq.WorkerID != "worker-1" || store.claimReq.LeaseDuration != 2*time.Minute || store.claimReq.BatchSize != 7 {
		t.Fatalf("claim request = %#v", store.claimReq)
	}
	if len(store.statuses) != 1 {
		t.Fatalf("status updates = %#v", store.statuses)
	}
	got := store.statuses[0]
	if got.id != "job-1" || got.status != "succeeded" || got.stage != "accepted" || got.code != "" || got.message != "" {
		t.Fatalf("success update = %#v", got)
	}
	if len(store.retries) != 0 {
		t.Fatalf("unexpected retries = %#v", store.retries)
	}
}

func TestIngestionJobWorkerAppendsTransitionEvent(t *testing.T) {
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
	events := &recordingIngestionEventStore{}
	worker := &IngestionJobWorker{
		Store:    store,
		Events:   events,
		WorkerID: "worker-1",
		Handlers: map[string]IngestionJobHandler{
			"asset_process": IngestionJobHandlerFunc(func(context.Context, IngestionJob) error { return nil }),
		},
	}

	if _, err := worker.ProcessOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(events.appended) != 1 {
		t.Fatalf("events = %#v", events.appended)
	}
	got := events.appended[0]
	if got.EntityType != "ingestion_job" || got.EntityID != "10000000-0000-0000-0000-000000000001" ||
		got.JobID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("event identity = %#v", got)
	}
	if got.FromStatus != "running" || got.ToStatus != "succeeded" || got.EventType != "ingestion_job.succeeded" {
		t.Fatalf("event transition = %#v", got)
	}
	if got.Detail["stage"] != "accepted" || got.Detail["job_type"] != "asset_process" {
		t.Fatalf("event detail = %#v", got.Detail)
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
		WorkerID:     "worker-1",
		RetryBackoff: time.Minute,
		Clock:        func() time.Time { return now },
		Handlers: map[string]IngestionJobHandler{
			"asset_process": IngestionJobHandlerFunc(func(context.Context, IngestionJob) error {
				return errors.New("neo4j down")
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
	if got.id != "job-1" || got.stage != "extracting" || got.code != "handler_failed" || got.message != "neo4j down" {
		t.Fatalf("retry update = %#v", got)
	}
	if !got.nextAttemptAt.Equal(now.Add(time.Minute)) {
		t.Fatalf("next attempt = %s", got.nextAttemptAt)
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
		Store:    store,
		WorkerID: "worker-1",
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
	worker := &IngestionJobWorker{Store: store, WorkerID: "worker-1", Handlers: map[string]IngestionJobHandler{}}
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
		Store:    store,
		WorkerID: "worker-1",
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
	worker := &IngestionJobWorker{Store: store, WorkerID: "worker-1", QueueDepth: depth, Handlers: map[string]IngestionJobHandler{}}

	if _, err := worker.ProcessOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	if depth.calls != 1 || depth.lastStatus != ingestionJobStatusQueued {
		t.Fatalf("queue depth source calls = %d lastStatus = %q", depth.calls, depth.lastStatus)
	}
}

func TestIngestionJobWorkerSkipsQueueDepthWhenSourceMissingOrErroring(t *testing.T) {
	store := &fakeIngestionJobQueue{}
	worker := &IngestionJobWorker{Store: store, WorkerID: "worker-1", Handlers: map[string]IngestionJobHandler{}}
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

type fakeQueueDepthSource struct {
	calls      int
	lastStatus string
	count      int64
	err        error
}

func (f *fakeQueueDepthSource) CountByStatus(_ context.Context, status string) (int64, error) {
	f.calls++
	f.lastStatus = status
	if f.err != nil {
		return 0, f.err
	}
	return f.count, nil
}

type fakeIngestionJobQueue struct {
	claimed  []IngestionJob
	claimReq ClaimIngestionJobsRequest
	statuses []fakeIngestionJobStatus
	retries  []fakeIngestionJobRetry
}

func (f *fakeIngestionJobQueue) Claim(_ context.Context, req ClaimIngestionJobsRequest) ([]IngestionJob, error) {
	f.claimReq = req
	return append([]IngestionJob(nil), f.claimed...), nil
}

func (f *fakeIngestionJobQueue) UpdateStatus(_ context.Context, id, status, stage, code, message string) (IngestionJob, error) {
	f.statuses = append(f.statuses, fakeIngestionJobStatus{id: id, status: status, stage: stage, code: code, message: message})
	return IngestionJob{ID: id, Status: status, Stage: stage, ErrorCode: code, ErrorMessage: message}, nil
}

func (f *fakeIngestionJobQueue) Retry(_ context.Context, id, stage, code, message string, nextAttemptAt time.Time) (IngestionJob, error) {
	f.retries = append(f.retries, fakeIngestionJobRetry{
		id: id, stage: stage, code: code, message: message, nextAttemptAt: nextAttemptAt,
	})
	return IngestionJob{ID: id, Status: "queued", Stage: stage, ErrorCode: code, ErrorMessage: message, NextAttemptAt: nextAttemptAt}, nil
}

type fakeIngestionJobStatus struct {
	id      string
	status  string
	stage   string
	code    string
	message string
}

type fakeIngestionJobRetry struct {
	id            string
	stage         string
	code          string
	message       string
	nextAttemptAt time.Time
}

type recordingIngestionEventStore struct {
	appended []AppendIngestionEventRequest
}

func (s *recordingIngestionEventStore) Append(_ context.Context, req AppendIngestionEventRequest) (IngestionEvent, error) {
	s.appended = append(s.appended, req)
	return IngestionEvent{ID: int64(len(s.appended))}, nil
}
