package documents

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// BindJobCandidate attaches a claimed job to the version it may write, under the job's
// own fence. A stalled worker whose lease generation has moved on binds nothing, so two
// workers can never both believe they own the same candidate.
func (s *PostgresPipelineStore) BindJobCandidate(ctx context.Context, binding JobCandidateBinding) error {
	params, err := jobCandidateBindingParams(binding)
	if err != nil {
		return err
	}
	var result sqlc.BindPipelineJobCandidateRow
	err = s.withIdentity(ctx, binding.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		result, queryErr = q.BindPipelineJobCandidate(ctx, params)
		return queryErr
	})
	if err != nil {
		return fmt.Errorf("bind pipeline job candidate: %w", err)
	}
	if !result.LeaseValid {
		return fmt.Errorf("bind pipeline job candidate: %w", ErrIngestionJobLeaseLost)
	}
	if !result.Bound {
		return fmt.Errorf("bind pipeline job candidate: %w", ErrPipelineActivationRejected)
	}
	return nil
}

func jobCandidateBindingParams(binding JobCandidateBinding) (sqlc.BindPipelineJobCandidateParams, error) {
	identityID, documentID, versionID, err := pipelineIDs(
		binding.IdentityID, binding.DocumentID, binding.VersionID,
	)
	if err != nil {
		return sqlc.BindPipelineJobCandidateParams{}, err
	}
	jobID, err := pgUUID("pipeline binding job id", strings.TrimSpace(binding.JobID))
	if err != nil {
		return sqlc.BindPipelineJobCandidateParams{}, err
	}
	assetID, err := pgUUID("pipeline binding asset id", strings.TrimSpace(binding.AssetID))
	if err != nil {
		return sqlc.BindPipelineJobCandidateParams{}, err
	}
	stage := binding.Stage
	if stage == "" {
		stage = PipelineStageConvert
	}
	if !validPipelineStage(stage) || strings.TrimSpace(binding.WorkerID) == "" ||
		binding.LeaseGeneration <= 0 || binding.PipelineGeneration <= 0 {
		return sqlc.BindPipelineJobCandidateParams{}, fmt.Errorf("pipeline job candidate fence is incomplete")
	}
	return sqlc.BindPipelineJobCandidateParams{
		JobID: jobID, IdentityID: identityID, LockedBy: pgText(binding.WorkerID),
		LeaseGeneration: binding.LeaseGeneration, AssetID: assetID,
		DocumentID: documentID, VersionID: versionID,
		PipelineGeneration: binding.PipelineGeneration, Stage: string(stage),
	}, nil
}
