package documents

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// WorkerCancellationRequest carries the claim fence; child identity alone cannot mutate a job.
type WorkerCancellationRequest struct {
	IdentityID      string
	JobID           string
	WorkerID        string
	LeaseGeneration int64
	ChildID         string
}

// RequestWorkerCancellation persists stop intent before the runtime is interrupted.
func (s *PostgresIngestionJobStore) RequestWorkerCancellation(ctx context.Context, req WorkerCancellationRequest) error {
	identityID, jobID, err := ingestionJobFence(req.IdentityID, req.JobID)
	if err != nil {
		return err
	}
	if req.ChildID == "" {
		return fmt.Errorf("worker cancellation requires child identity")
	}
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		_, err := q.RequestWorkerCancellation(ctx, sqlc.RequestWorkerCancellationParams{
			ID: jobID, IdentityID: identityID, LockedBy: pgText(req.WorkerID), LeaseGeneration: req.LeaseGeneration, ChildID: req.ChildID,
		})
		return err
	})
	return fencedIngestionJobError("request worker cancellation", err)
}
