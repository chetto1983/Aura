package documents

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// StageDelegationDeliveryRequest replaces a claimed delegation payload after
// worker execution, preserving the terminal report for delivery-only retries.
type StageDelegationDeliveryRequest struct {
	IdentityID      string
	JobID           string
	WorkerID        string
	LeaseGeneration int64
	Payload         map[string]any
}

// StageDelegationDelivery persists terminal work under the active lease fence.
func (s *PostgresIngestionJobStore) StageDelegationDelivery(ctx context.Context, req StageDelegationDeliveryRequest) (IngestionJob, error) {
	identityID, jobID, err := ingestionJobFence(req.IdentityID, req.JobID)
	if err != nil {
		return IngestionJob{}, err
	}
	payload, err := ingestionJobPayloadJSON(req.Payload)
	if err != nil {
		return IngestionJob{}, fmt.Errorf("stage delegation delivery payload: %w", err)
	}
	var row sqlc.AuraIngestionJobs
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.StageDelegationDelivery(ctx, sqlc.StageDelegationDeliveryParams{
			ID:              jobID,
			IdentityID:      identityID,
			LockedBy:        pgText(req.WorkerID),
			LeaseGeneration: req.LeaseGeneration,
			Payload:         payload,
		})
		return queryErr
	})
	if err != nil {
		return IngestionJob{}, fencedIngestionJobError("stage delegation delivery", err)
	}
	return ingestionJobFromSQL(row)
}
