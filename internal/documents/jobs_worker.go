package documents

import (
	"context"
	"fmt"
	"log/slog"
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

	ingestionJobOutcomeRetryScheduled = "retry_scheduled"
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

// IngestionQueueDepthSource reports the current durable queued-job backlog
// for the queue-depth gauge. Optional: a nil source skips the gauge (fail-soft).
type IngestionQueueDepthSource interface {
	CountByStatus(ctx context.Context, status string) (int64, error)
}

// IngestionJobWorker claims durable ingestion jobs and dispatches by job type.
type IngestionJobWorker struct {
	Store         IngestionJobQueue
	Events        IngestionEventStore
	Handlers      map[string]IngestionJobHandler
	WorkerID      string
	LeaseDuration time.Duration
	BatchSize     int
	RetryBackoff  time.Duration
	Clock         Clock

	// QueueDepth is optional; when set, ProcessOnce samples the current queued
	// backlog once per pass and records it to the ingestion_queue_depth gauge.
	QueueDepth IngestionQueueDepthSource
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
	w.recordQueueDepth(ctx)
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
		message := fmt.Sprintf("no ingestion handler for job type %q", job.JobType)
		if _, err := w.Store.UpdateStatus(ctx, job.ID, ingestionJobStatusDeadLetter, job.Stage, "handler_missing", message); err != nil {
			return err
		}
		slog.Warn("documents: ingestion job dead-lettered", "job_id", job.ID, "job_type", job.JobType,
			"stage", job.Stage, "code", "handler_missing", "attempts", job.AttemptCount)
		recordIngestionJobOutcome(ctx, ingestionJobStatusDeadLetter)
		return w.appendTransitionEvent(ctx, job, ingestionJobStatusDeadLetter, "ingestion_job.dead_letter", "handler_missing", message)
	}
	if err := handler.HandleIngestionJob(ctx, job); err != nil {
		return w.recordHandlerFailure(ctx, job, err)
	}
	if _, err := w.Store.UpdateStatus(ctx, job.ID, ingestionJobStatusSucceeded, job.Stage, "", ""); err != nil {
		return err
	}
	recordIngestionJobOutcome(ctx, ingestionJobStatusSucceeded)
	return w.appendTransitionEvent(ctx, job, ingestionJobStatusSucceeded, "ingestion_job.succeeded", "", "")
}

func (w *IngestionJobWorker) recordHandlerFailure(ctx context.Context, job IngestionJob, cause error) error {
	message := cause.Error()
	if job.MaxAttempts > 0 && job.AttemptCount >= job.MaxAttempts {
		if _, err := w.Store.UpdateStatus(ctx, job.ID, ingestionJobStatusDeadLetter, job.Stage, "handler_failed", message); err != nil {
			return err
		}
		slog.Warn("documents: ingestion job dead-lettered", "job_id", job.ID, "job_type", job.JobType,
			"stage", job.Stage, "code", "handler_failed", "attempts", job.AttemptCount, "err", message)
		recordIngestionJobOutcome(ctx, ingestionJobStatusDeadLetter)
		return w.appendTransitionEvent(ctx, job, ingestionJobStatusDeadLetter, "ingestion_job.dead_letter", "handler_failed", message)
	}
	if _, err := w.Store.Retry(ctx, job.ID, job.Stage, "handler_failed", message, w.now().Add(w.retryBackoff())); err != nil {
		return err
	}
	recordIngestionJobOutcome(ctx, ingestionJobOutcomeRetryScheduled)
	return w.appendTransitionEvent(ctx, job, ingestionJobStatusQueued, "ingestion_job.retry_scheduled", "handler_failed", message)
}

// recordQueueDepth samples the queued-job backlog and records the gauge. It
// is fail-soft: a missing source or a count error simply skips the point,
// never affecting ProcessOnce's return value.
func (w *IngestionJobWorker) recordQueueDepth(ctx context.Context) {
	if w.QueueDepth == nil {
		return
	}
	depth, err := w.QueueDepth.CountByStatus(ctx, ingestionJobStatusQueued)
	if err != nil {
		return
	}
	recordIngestionQueueDepth(ctx, depth)
}

func (w *IngestionJobWorker) appendTransitionEvent(ctx context.Context, job IngestionJob, toStatus, eventType, code, message string) error {
	if w.Events == nil {
		return nil
	}
	detail := map[string]any{
		"job_type": job.JobType,
		"stage":    job.Stage,
	}
	if code != "" {
		detail["error_code"] = code
	}
	_, err := w.Events.Append(ctx, AppendIngestionEventRequest{
		EntityType: "ingestion_job",
		EntityID:   job.ID,
		JobID:      job.ID,
		FromStatus: job.Status,
		ToStatus:   toStatus,
		EventType:  eventType,
		Message:    message,
		Detail:     detail,
	})
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
