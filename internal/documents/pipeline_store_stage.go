package documents

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
)

// UpsertStage is keyed on (identity, version, stage, input fingerprint), so a
// worker replaying a stage over unchanged inputs recovers the existing ledger row
// rather than restarting the attempt budget. A row that exists under a different
// producer version or generation is not adoptable and comes back as
// ErrPipelineCandidateRejected.
func (s *PostgresPipelineStore) UpsertStage(
	ctx context.Context,
	req StageUpsert,
) (PipelineStageRecord, error) {
	params, err := stageUpsertParams(req)
	if err != nil {
		return PipelineStageRecord{}, err
	}
	var row sqlc.AuraDocumentPipelineStages
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.UpsertPipelineStageLedger(ctx, params)
		return queryErr
	})
	if err != nil {
		return PipelineStageRecord{}, candidateRejectedError("upsert pipeline stage", err)
	}
	return pipelineStageFromSQL(row)
}

// ClaimStage takes the lease and bumps the lease generation, which is what makes
// every later fenced call able to tell this attempt from the next one. Finding
// nothing to claim — not yet due, still leased, attempts exhausted — is the normal
// idle outcome and returns ErrPipelineStageUnavailable, not a failure to log.
func (s *PostgresPipelineStore) ClaimStage(
	ctx context.Context,
	req StageClaim,
) (PipelineStageRecord, error) {
	params, err := stageClaimParams(req)
	if err != nil {
		return PipelineStageRecord{}, err
	}
	var row sqlc.AuraDocumentPipelineStages
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.ClaimPipelineStageLedger(ctx, params)
		return queryErr
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return PipelineStageRecord{}, ErrPipelineStageUnavailable
		}
		return PipelineStageRecord{}, fmt.Errorf("claim pipeline stage: %w", err)
	}
	return pipelineStageFromSQL(row)
}

// HeartbeatStage extends the lease only while the worker id and lease generation
// still match a running, unexpired row. ErrPipelineStageLeaseLost therefore means
// the stage was already reclaimed: the caller must abandon its work, because
// retrying would race the worker that now holds it.
func (s *PostgresPipelineStore) HeartbeatStage(ctx context.Context, fence StageFence) error {
	params, err := stageHeartbeatParams(fence)
	if err != nil {
		return err
	}
	err = s.withIdentity(ctx, fence.IdentityID, func(q *sqlc.Queries) error {
		_, queryErr := q.HeartbeatPipelineStageLedger(ctx, params)
		return queryErr
	})
	return pipelineStageFenceError("heartbeat", err)
}

// CompleteStage records the artifact under the same worker and lease-generation
// fence as the heartbeat, so a worker that lost its lease mid-run cannot overwrite
// the result its successor already published.
func (s *PostgresPipelineStore) CompleteStage(ctx context.Context, completion StageCompletion) error {
	params, err := stageCompletionParams(completion)
	if err != nil {
		return err
	}
	err = s.withIdentity(ctx, completion.IdentityID, func(q *sqlc.Queries) error {
		_, queryErr := q.CompletePipelineStageLedger(ctx, params)
		return queryErr
	})
	return pipelineStageFenceError("complete", err)
}

// FailStage passes the retryable flag down and lets SQL pick the terminal state
// against the attempt budget it already holds — pending, dead_letter or failed —
// so the decision cannot disagree with the attempt count the claim just wrote.
// It is fenced like CompleteStage: a lost lease reports ErrPipelineStageLeaseLost
// rather than burning an attempt that belongs to another worker.
func (s *PostgresPipelineStore) FailStage(ctx context.Context, failure StageFailure) error {
	params, err := stageFailureParams(failure)
	if err != nil {
		return err
	}
	err = s.withIdentity(ctx, failure.IdentityID, func(q *sqlc.Queries) error {
		_, queryErr := q.FailPipelineStageLedger(ctx, params)
		return queryErr
	})
	return pipelineStageFenceError("fail", err)
}

func pipelineStageFenceError(action string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s pipeline stage: %w", action, ErrPipelineStageLeaseLost)
	}
	return fmt.Errorf("%s pipeline stage: %w", action, err)
}
