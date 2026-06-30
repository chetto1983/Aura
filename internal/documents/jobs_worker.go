package documents

import (
	"context"
	"fmt"
	"time"
)

const (
	defaultIngestionWorkerID      = "aura-ingestion-worker"
	defaultIngestionLeaseDuration = 5 * time.Minute
	defaultIngestionBatchSize     = 10
	defaultIngestionRetryBackoff  = time.Minute

	ingestionJobStatusQueued     = "queued"
	ingestionJobStatusSucceeded  = "succeeded"
	ingestionJobStatusDeadLetter = "dead_letter"
)

// IngestionJobQueue claims and updates durable ingestion jobs.
type IngestionJobQueue interface {
	Claim(ctx context.Context, req ClaimIngestionJobsRequest) ([]IngestionJob, error)
	UpdateStatus(ctx context.Context, id, status, stage, code, message string) (IngestionJob, error)
	Retry(ctx context.Context, id, stage, code, message string, nextAttemptAt time.Time) (IngestionJob, error)
}

// IngestionJobHandler processes one claimed durable ingestion job.
type IngestionJobHandler interface {
	HandleIngestionJob(ctx context.Context, job IngestionJob) error
}

// IngestionJobHandlerFunc adapts a function to IngestionJobHandler.
type IngestionJobHandlerFunc func(context.Context, IngestionJob) error

// HandleIngestionJob calls f(ctx, job).
func (f IngestionJobHandlerFunc) HandleIngestionJob(ctx context.Context, job IngestionJob) error {
	return f(ctx, job)
}

// IngestionJobWorker claims durable ingestion jobs and dispatches by job type.
type IngestionJobWorker struct {
	Store         IngestionJobQueue
	Handlers      map[string]IngestionJobHandler
	WorkerID      string
	LeaseDuration time.Duration
	BatchSize     int
	RetryBackoff  time.Duration
	Clock         Clock
}

// ProcessOnce claims one batch and persists each job's terminal or retry state.
func (w *IngestionJobWorker) ProcessOnce(ctx context.Context) (int, error) {
	if w == nil || w.Store == nil {
		return 0, fmt.Errorf("ingestion job worker has no store")
	}
	jobs, err := w.Store.Claim(ctx, ClaimIngestionJobsRequest{
		WorkerID:      w.workerID(),
		LeaseDuration: w.leaseDuration(),
		BatchSize:     w.batchSize(),
	})
	if err != nil {
		return 0, err
	}
	processed := 0
	for _, job := range jobs {
		if err := w.processJob(ctx, job); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}

func (w *IngestionJobWorker) processJob(ctx context.Context, job IngestionJob) error {
	handler := w.Handlers[job.JobType]
	if handler == nil {
		_, err := w.Store.UpdateStatus(ctx, job.ID, ingestionJobStatusDeadLetter, job.Stage, "handler_missing", fmt.Sprintf("no ingestion handler for job type %q", job.JobType))
		return err
	}
	if err := handler.HandleIngestionJob(ctx, job); err != nil {
		return w.recordHandlerFailure(ctx, job, err)
	}
	_, err := w.Store.UpdateStatus(ctx, job.ID, ingestionJobStatusSucceeded, job.Stage, "", "")
	return err
}

func (w *IngestionJobWorker) recordHandlerFailure(ctx context.Context, job IngestionJob, cause error) error {
	message := cause.Error()
	if job.MaxAttempts > 0 && job.AttemptCount >= job.MaxAttempts {
		_, err := w.Store.UpdateStatus(ctx, job.ID, ingestionJobStatusDeadLetter, job.Stage, "handler_failed", message)
		return err
	}
	_, err := w.Store.Retry(ctx, job.ID, job.Stage, "handler_failed", message, w.now().Add(w.retryBackoff()))
	return err
}

func (w *IngestionJobWorker) workerID() string {
	if w.WorkerID != "" {
		return w.WorkerID
	}
	return defaultIngestionWorkerID
}

func (w *IngestionJobWorker) leaseDuration() time.Duration {
	if w.LeaseDuration > 0 {
		return w.LeaseDuration
	}
	return defaultIngestionLeaseDuration
}

func (w *IngestionJobWorker) batchSize() int {
	if w.BatchSize > 0 {
		return w.BatchSize
	}
	return defaultIngestionBatchSize
}

func (w *IngestionJobWorker) retryBackoff() time.Duration {
	if w.RetryBackoff > 0 {
		return w.RetryBackoff
	}
	return defaultIngestionRetryBackoff
}

func (w *IngestionJobWorker) now() time.Time {
	if w.Clock != nil {
		return w.Clock().UTC()
	}
	return time.Now().UTC()
}
