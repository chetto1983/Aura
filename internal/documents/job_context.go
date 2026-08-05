package documents

import (
	"context"
	"errors"
)

// ErrIngestionJobCompletionCommitted tells the queue worker that the handler's
// publication transaction already completed the durable ingestion job.
var ErrIngestionJobCompletionCommitted = errors.New("ingestion job completion already committed")

type ingestionJobContextKey struct{}

// WithClaimedIngestionJob carries the exact fenced claim into the processing
// stack. It is populated only by the durable queue handler.
func WithClaimedIngestionJob(ctx context.Context, job IngestionJob) context.Context {
	return context.WithValue(ctx, ingestionJobContextKey{}, job)
}

// ClaimedIngestionJob returns the fenced durable claim attached by the worker.
func ClaimedIngestionJob(ctx context.Context) (IngestionJob, bool) {
	job, ok := ctx.Value(ingestionJobContextKey{}).(IngestionJob)
	return job, ok
}
