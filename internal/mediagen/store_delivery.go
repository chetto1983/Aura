package mediagen

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// ClaimDelivery gives exactly one caller the delivery of a completed job. In one identity
// transaction it marks the job delivered and binds the job's asset to deliveryCallID, the call
// whose result shows the clip; the job's own ToolCallID keeps naming the call that submitted
// it. Unless exactly one asset row is bound, the transaction rolls back with asset_not_found
// and the job stays collectible.
//
// True means the caller won. A lost claim returns the owned row with false, so the caller can
// tell a job already delivered from one still running. A job that is missing, foreign or
// submitted in another conversation returns pgx.ErrNoRows: outside its conversation there is
// no job to deliver.
//
// The claim is one winner per job, not proof of receipt: a crash after it commits leaves the
// clip in identity storage and the job delivered.
func (s *Store) ClaimDelivery(ctx context.Context, ownerID, jobID, conversationID, deliveryCallID string) (Job, bool, error) {
	if conversationID == "" || deliveryCallID == "" {
		return Job{}, false, errors.New("mediagen: a delivery claim needs its conversation and tool call")
	}
	key, err := jobKey(ownerID, jobID)
	if err != nil {
		return Job{}, false, err
	}
	claimed := false
	job, err := s.withJob(ctx, ownerID, func(q *sqlc.Queries) (sqlc.AuraMediaJob, error) {
		row, err := q.ClaimMediaJobDelivery(ctx, sqlc.ClaimMediaJobDeliveryParams{
			ID: key.ID, IdentityID: key.IdentityID, ConversationID: conversationID,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return ownedInConversation(ctx, q, key, conversationID)
		}
		if err != nil {
			return row, err
		}
		bound, err := q.BindMediaJobAssetDelivery(ctx, sqlc.BindMediaJobAssetDeliveryParams{
			ID: row.AssetID, IdentityID: key.IdentityID, ToolCallID: deliveryCallID, ThreadID: conversationID,
		})
		if err != nil {
			return row, err
		}
		if bound != 1 {
			return row, videoAssetNotFound()
		}
		claimed = true
		return row, nil
	})
	if err != nil {
		return Job{}, false, err
	}
	return job, claimed, nil
}

func ownedInConversation(ctx context.Context, q *sqlc.Queries, key sqlc.GetMediaJobForIdentityParams, conversationID string) (sqlc.AuraMediaJob, error) {
	row, err := q.GetMediaJobForIdentity(ctx, key)
	if err == nil && row.ConversationID != conversationID {
		return sqlc.AuraMediaJob{}, pgx.ErrNoRows
	}
	return row, err
}
