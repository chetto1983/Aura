package documents

import (
	"context"
	"errors"
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
