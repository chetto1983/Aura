package documents

import (
	"context"
	"errors"
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
	UpdateStatus(ctx context.Context, req TransitionIngestionJobRequest) (IngestionJob, error)
	Retry(ctx context.Context, req RetryIngestionJobRequest) (IngestionJob, error)
	Heartbeat(ctx context.Context, req HeartbeatIngestionJobRequest) (IngestionJob, error)
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
	CountByStatus(ctx context.Context, identityID, status string) (int64, error)
}

// IngestionJobWorker claims durable ingestion jobs and dispatches by job type.
type IngestionJobWorker struct {
	Store         IngestionJobQueue
	Handlers      map[string]IngestionJobHandler
	IdentityID    string
	WorkerID      string
	LeaseDuration time.Duration
	BatchSize     int
	RetryBackoff  time.Duration
	RetryJitter   func(time.Duration) time.Duration
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
	if w.IdentityID == "" {
		return 0, fmt.Errorf("ingestion job worker has no identity")
	}
	jobs, err := w.Store.Claim(ctx, ClaimIngestionJobsRequest{
		IdentityID:    w.IdentityID,
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
		if job.IdentityID != w.IdentityID {
			return processed, fmt.Errorf("claimed ingestion job %q has unexpected identity %q", job.ID, job.IdentityID)
		}
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
		if _, err := w.Store.UpdateStatus(ctx, w.transitionRequest(job, ingestionJobStatusDeadLetter,
			"handler_missing", message, "ingestion_job.dead_letter")); err != nil {
			return err
		}
		slog.Warn("documents: ingestion job dead-lettered", "job_id", job.ID, "job_type", job.JobType,
			"stage", job.Stage, "code", "handler_missing", "attempts", job.AttemptCount)
		recordIngestionJobOutcome(ctx, ingestionJobStatusDeadLetter)
		return nil
	}
	if err := w.handleWithHeartbeat(ctx, job, handler); err != nil {
		if errors.Is(err, ErrIngestionJobCompletionCommitted) {
			recordIngestionJobOutcome(ctx, ingestionJobStatusSucceeded)
			return nil
		}
		return w.recordHandlerFailure(ctx, job, err)
	}
	if _, err := w.Store.UpdateStatus(ctx, w.transitionRequest(job, ingestionJobStatusSucceeded,
		"", "", "ingestion_job.succeeded")); err != nil {
		return err
	}
	recordIngestionJobOutcome(ctx, ingestionJobStatusSucceeded)
	return nil
}

func (w *IngestionJobWorker) recordHandlerFailure(ctx context.Context, job IngestionJob, cause error) error {
	if errors.Is(cause, ErrIngestionJobLeaseLost) {
		return cause
	}
	message := cause.Error()
	if job.MaxAttempts > 0 && job.AttemptCount >= job.MaxAttempts {
		if _, err := w.Store.UpdateStatus(ctx, w.transitionRequest(job, ingestionJobStatusDeadLetter,
			"handler_failed", message, "ingestion_job.dead_letter")); err != nil {
			return err
		}
		slog.Warn("documents: ingestion job dead-lettered", "job_id", job.ID, "job_type", job.JobType,
			"stage", job.Stage, "code", "handler_failed", "attempts", job.AttemptCount, "err", message)
		recordIngestionJobOutcome(ctx, ingestionJobStatusDeadLetter)
		return nil
	}
	if _, err := w.Store.Retry(ctx, RetryIngestionJobRequest{
		IdentityID: job.IdentityID, JobID: job.ID, WorkerID: w.workerID(),
		LeaseGeneration: job.LeaseGeneration, Stage: job.Stage,
		ErrorCode: "handler_failed", ErrorMessage: message, EventMessage: message,
		NextAttemptAt: w.now().Add(w.retryDelay(job.AttemptCount)),
	}); err != nil {
		return err
	}
	recordIngestionJobOutcome(ctx, ingestionJobOutcomeRetryScheduled)
	return nil
}

// recordQueueDepth samples the queued-job backlog and records the gauge. It
// is fail-soft: a missing source or a count error simply skips the point,
// never affecting ProcessOnce's return value.
func (w *IngestionJobWorker) recordQueueDepth(ctx context.Context) {
	if w.QueueDepth == nil {
		return
	}
	depth, err := w.QueueDepth.CountByStatus(ctx, w.IdentityID, ingestionJobStatusQueued)
	if err != nil {
		return
	}
	recordIngestionQueueDepth(ctx, depth)
}

func (w *IngestionJobWorker) transitionRequest(job IngestionJob, status, code, message, eventType string) TransitionIngestionJobRequest {
	detail := map[string]any{"job_type": job.JobType, "stage": job.Stage}
	if code != "" {
		detail["error_code"] = code
	}
	return TransitionIngestionJobRequest{
		IdentityID: job.IdentityID, JobID: job.ID, WorkerID: w.workerID(),
		LeaseGeneration: job.LeaseGeneration, Status: status, Stage: job.Stage,
		ErrorCode: code, ErrorMessage: message, EventType: eventType,
		EventMessage: message, EventDetail: detail,
	}
}

func (w *IngestionJobWorker) handleWithHeartbeat(ctx context.Context, job IngestionJob, handler IngestionJobHandler) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	heartbeatResult := make(chan error, 1)
	go func() {
		heartbeatResult <- w.maintainLease(runCtx, cancel, job)
	}()
	handlerErr := handler.HandleIngestionJob(runCtx, job)
	cancel()
	heartbeatErr := <-heartbeatResult
	if errors.Is(handlerErr, ErrIngestionJobCompletionCommitted) {
		return handlerErr
	}
	if heartbeatErr != nil {
		return heartbeatErr
	}
	return handlerErr
}

func (w *IngestionJobWorker) maintainLease(ctx context.Context, cancel context.CancelFunc, job IngestionJob) error {
	interval := w.leaseDuration() / 3
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			_, err := w.Store.Heartbeat(ctx, HeartbeatIngestionJobRequest{
				IdentityID: job.IdentityID, JobID: job.ID, WorkerID: w.workerID(),
				LeaseGeneration: job.LeaseGeneration, LeaseDuration: w.leaseDuration(),
			})
			if err == nil {
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			cancel()
			return err
		}
	}
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

func (w *IngestionJobWorker) retryDelay(attempt int) time.Duration {
	cap := exponentialRetryCap(w.retryBackoff(), attempt)
	if w.RetryJitter != nil {
		return w.RetryJitter(cap)
	}
	return fullJitter(cap)
}

func (w *IngestionJobWorker) now() time.Time {
	if w.Clock != nil {
		return w.Clock().UTC()
	}
	return time.Now().UTC()
}
