package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/documents"
)

func TestRuntimeAssetProcessingJobWorkerProcessesClaimedAsset(t *testing.T) {
	store := &fakeRuntimeIngestionJobQueue{
		claimed: []documents.IngestionJob{{
			ID:           "job-1",
			JobType:      assetProcessJobType,
			Status:       "running",
			Stage:        "accepted",
			AttemptCount: 1,
			MaxAttempts:  3,
			Payload: map[string]any{
				"asset_id":    "asset-1",
				"identity_id": "identity-1",
			},
		}},
	}
	processor := &recordingAssetProcessor{}
	worker := newRuntimeProcessingJobWorker(store, processor, nil, 3)

	processed, err := worker.ProcessOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 {
		t.Fatalf("processed = %d", processed)
	}
	if processor.assetID != "asset-1" || processor.identityID != "identity-1" {
		t.Fatalf("processor got identity=%q asset=%q", processor.identityID, processor.assetID)
	}
	if store.claimReq.BatchSize != 3 || store.claimReq.WorkerID == "" {
		t.Fatalf("claim request = %#v", store.claimReq)
	}
	if len(store.statuses) != 1 || store.statuses[0].status != "succeeded" {
		t.Fatalf("status updates = %#v", store.statuses)
	}
}

func TestRuntimeProcessingJobWorkerProcessesEmbeddingJob(t *testing.T) {
	store := &fakeRuntimeIngestionJobQueue{
		claimed: []documents.IngestionJob{{
			ID:           "job-1",
			JobType:      documents.IngestionJobTypeDocumentEmbed,
			Status:       "running",
			Stage:        "embedding",
			AttemptCount: 1,
			MaxAttempts:  3,
		}},
	}
	handler := &recordingIngestionHandler{}
	worker := newRuntimeProcessingJobWorker(store, &recordingAssetProcessor{}, handler, 2)

	processed, err := worker.ProcessOnce(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if processed != 1 || handler.called != 1 {
		t.Fatalf("processed=%d handler.called=%d", processed, handler.called)
	}
	if len(store.statuses) != 1 || store.statuses[0].status != "succeeded" {
		t.Fatalf("status updates = %#v", store.statuses)
	}
}

func TestRuntimeIngestionWorkerStartStopProcesses(t *testing.T) {
	processor := &recordingRuntimeIngestionProcessor{done: make(chan struct{})}
	worker := newRuntimeIngestionWorker(processor, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	worker.Start(ctx)
	select {
	case <-processor.done:
	case <-time.After(time.Second):
		t.Fatal("worker did not process")
	}
	worker.Stop()
}

type fakeRuntimeIngestionJobQueue struct {
	claimed  []documents.IngestionJob
	claimReq documents.ClaimIngestionJobsRequest
	statuses []fakeRuntimeIngestionStatus
	retries  []fakeRuntimeIngestionRetry
}

func (f *fakeRuntimeIngestionJobQueue) Claim(_ context.Context, req documents.ClaimIngestionJobsRequest) ([]documents.IngestionJob, error) {
	f.claimReq = req
	return append([]documents.IngestionJob(nil), f.claimed...), nil
}

func (f *fakeRuntimeIngestionJobQueue) UpdateStatus(_ context.Context, id, status, stage, code, message string) (documents.IngestionJob, error) {
	f.statuses = append(f.statuses, fakeRuntimeIngestionStatus{
		id: id, status: status, stage: stage, code: code, message: message,
	})
	return documents.IngestionJob{ID: id, Status: status, Stage: stage, ErrorCode: code, ErrorMessage: message}, nil
}

func (f *fakeRuntimeIngestionJobQueue) Retry(_ context.Context, id, stage, code, message string, nextAttemptAt time.Time) (documents.IngestionJob, error) {
	f.retries = append(f.retries, fakeRuntimeIngestionRetry{
		id: id, stage: stage, code: code, message: message, nextAttemptAt: nextAttemptAt,
	})
	return documents.IngestionJob{ID: id, Status: "queued", Stage: stage, ErrorCode: code, ErrorMessage: message, NextAttemptAt: nextAttemptAt}, nil
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

type recordingRuntimeIngestionProcessor struct {
	once sync.Once
	done chan struct{}
}

func (p *recordingRuntimeIngestionProcessor) ProcessOnce(context.Context) (int, error) {
	p.once.Do(func() { close(p.done) })
	return 0, nil
}

type recordingIngestionHandler struct {
	called int
}

func (h *recordingIngestionHandler) HandleIngestionJob(context.Context, documents.IngestionJob) error {
	h.called++
	return nil
}
