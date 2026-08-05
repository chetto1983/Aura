package documents

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

func stageUpsertParams(req StageUpsert) (sqlc.UpsertPipelineStageLedgerParams, error) {
	identityID, documentID, versionID, err := pipelineIDs(req.IdentityID, req.DocumentID, req.VersionID)
	if err != nil {
		return sqlc.UpsertPipelineStageLedgerParams{}, err
	}
	if !validPipelineStage(req.Stage) || strings.TrimSpace(req.InputFingerprint) == "" ||
		strings.TrimSpace(req.ProducerVersion) == "" || req.PipelineGeneration <= 0 {
		return sqlc.UpsertPipelineStageLedgerParams{}, fmt.Errorf("pipeline stage identity is incomplete")
	}
	maxAttempts := req.MaxAttempts
	if maxAttempts == 0 {
		maxAttempts = 5
	}
	if maxAttempts < 1 || maxAttempts > math.MaxInt32 {
		return sqlc.UpsertPipelineStageLedgerParams{}, fmt.Errorf("pipeline stage max attempts is invalid")
	}
	diagnostic, err := pipelineDiagnosticJSON(req.DiagnosticState)
	if err != nil {
		return sqlc.UpsertPipelineStageLedgerParams{}, err
	}
	return sqlc.UpsertPipelineStageLedgerParams{
		IdentityID: identityID, DocumentID: documentID, VersionID: versionID,
		Stage: string(req.Stage), InputFingerprint: strings.TrimSpace(req.InputFingerprint),
		ProducerVersion:    strings.TrimSpace(req.ProducerVersion),
		PipelineGeneration: req.PipelineGeneration, MaxAttempts: int32(maxAttempts),
		NextAttemptAt: pipelineTime(req.NextAttemptAt), DiagnosticState: diagnostic,
	}, nil
}

func stageClaimParams(req StageClaim) (sqlc.ClaimPipelineStageLedgerParams, error) {
	identityID, err := pgUUID("pipeline stage identity id", strings.TrimSpace(req.IdentityID))
	if err != nil {
		return sqlc.ClaimPipelineStageLedgerParams{}, err
	}
	stageID, err := pgUUID("pipeline stage id", strings.TrimSpace(req.StageID))
	if err != nil {
		return sqlc.ClaimPipelineStageLedgerParams{}, err
	}
	if !validPipelineStage(req.Stage) || strings.TrimSpace(req.WorkerID) == "" || req.LeaseDuration <= 0 {
		return sqlc.ClaimPipelineStageLedgerParams{}, fmt.Errorf("pipeline stage claim is incomplete")
	}
	return sqlc.ClaimPipelineStageLedgerParams{
		LockedBy: pgText(req.WorkerID), LeaseDuration: pipelineInterval(req.LeaseDuration),
		ID: stageID, IdentityID: identityID, Stage: string(req.Stage),
	}, nil
}

func stageHeartbeatParams(fence StageFence) (sqlc.HeartbeatPipelineStageLedgerParams, error) {
	identityID, stageID, err := stageFenceIDs(fence)
	if err != nil {
		return sqlc.HeartbeatPipelineStageLedgerParams{}, err
	}
	if fence.LeaseDuration <= 0 {
		return sqlc.HeartbeatPipelineStageLedgerParams{}, fmt.Errorf("pipeline stage heartbeat lease duration must be positive")
	}
	return sqlc.HeartbeatPipelineStageLedgerParams{
		LeaseDuration: pipelineInterval(fence.LeaseDuration), ID: stageID, IdentityID: identityID,
		LockedBy: pgText(fence.WorkerID), LeaseGeneration: fence.LeaseGeneration,
	}, nil
}

func stageCompletionParams(completion StageCompletion) (sqlc.CompletePipelineStageLedgerParams, error) {
	identityID, stageID, err := stageFenceIDs(completion.StageFence)
	if err != nil {
		return sqlc.CompletePipelineStageLedgerParams{}, err
	}
	artifactID, err := pipelineOptionalUUID("pipeline artifact storage object id", completion.ArtifactStorageObjectID)
	if err != nil {
		return sqlc.CompletePipelineStageLedgerParams{}, err
	}
	artifactSHA := strings.TrimSpace(completion.ArtifactSHA256)
	if artifactSHA != "" {
		artifactSHA, err = canonicalSHA256(artifactSHA)
		if err != nil {
			return sqlc.CompletePipelineStageLedgerParams{}, fmt.Errorf("pipeline artifact sha256: %w", err)
		}
	}
	if completion.ArtifactSizeBytes < 0 {
		return sqlc.CompletePipelineStageLedgerParams{}, fmt.Errorf("pipeline artifact size must be non-negative")
	}
	diagnostic, err := pipelineDiagnosticJSON(completion.DiagnosticState)
	if err != nil {
		return sqlc.CompletePipelineStageLedgerParams{}, err
	}
	return sqlc.CompletePipelineStageLedgerParams{
		ArtifactStorageObjectID: artifactID,
		ArtifactObjectKey:       strings.TrimSpace(completion.ArtifactObjectKey),
		ArtifactSha256:          artifactSHA, ArtifactSizeBytes: completion.ArtifactSizeBytes,
		DiagnosticState: diagnostic, ID: stageID, IdentityID: identityID,
		LockedBy: pgText(completion.WorkerID), LeaseGeneration: completion.LeaseGeneration,
	}, nil
}

func stageFailureParams(failure StageFailure) (sqlc.FailPipelineStageLedgerParams, error) {
	identityID, stageID, err := stageFenceIDs(failure.StageFence)
	if err != nil {
		return sqlc.FailPipelineStageLedgerParams{}, err
	}
	if strings.TrimSpace(failure.ErrorClass) == "" || strings.TrimSpace(failure.ErrorCode) == "" {
		return sqlc.FailPipelineStageLedgerParams{}, fmt.Errorf("pipeline stage failure class and code are required")
	}
	nextAttempt := failure.NextAttemptAt
	if nextAttempt.IsZero() {
		nextAttempt = time.Now().UTC()
	}
	return sqlc.FailPipelineStageLedgerParams{
		Retryable: failure.Retryable, ErrorClass: strings.TrimSpace(failure.ErrorClass),
		ErrorCode: strings.TrimSpace(failure.ErrorCode), ErrorMessage: strings.TrimSpace(failure.RedactedMessage),
		NextAttemptAt: pipelineTime(nextAttempt), ID: stageID, IdentityID: identityID,
		LockedBy: pgText(failure.WorkerID), LeaseGeneration: failure.LeaseGeneration,
	}, nil
}

func stageFenceIDs(fence StageFence) (pgtype.UUID, pgtype.UUID, error) {
	identityID, err := pgUUID("pipeline stage identity id", strings.TrimSpace(fence.IdentityID))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	stageID, err := pgUUID("pipeline stage id", strings.TrimSpace(fence.StageID))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, err
	}
	if strings.TrimSpace(fence.WorkerID) == "" || fence.LeaseGeneration <= 0 {
		return pgtype.UUID{}, pgtype.UUID{}, fmt.Errorf("pipeline stage worker fence is incomplete")
	}
	return identityID, stageID, nil
}

func pipelineStageFromSQL(row sqlc.AuraDocumentPipelineStages) (PipelineStageRecord, error) {
	diagnostic := make(map[string]any)
	if len(row.DiagnosticState) != 0 {
		if err := json.Unmarshal(row.DiagnosticState, &diagnostic); err != nil {
			return PipelineStageRecord{}, fmt.Errorf("decode pipeline stage diagnostic state: %w", err)
		}
	}
	return PipelineStageRecord{
		ID: uuidString(row.ID), IdentityID: uuidString(row.IdentityID),
		DocumentID: uuidString(row.DocumentID), VersionID: uuidString(row.VersionID),
		Stage: PipelineStage(row.Stage), InputFingerprint: row.InputFingerprint,
		ProducerVersion: row.ProducerVersion, PipelineGeneration: row.PipelineGeneration,
		ArtifactStorageObjectID: uuidString(row.ArtifactStorageObjectID),
		ArtifactObjectKey:       row.ArtifactObjectKey, ArtifactSHA256: row.ArtifactSha256,
		ArtifactSizeBytes: row.ArtifactSizeBytes, Status: row.Status,
		AttemptCount: int(row.AttemptCount), MaxAttempts: int(row.MaxAttempts),
		LeaseGeneration: row.LeaseGeneration, LockedBy: textString(row.LockedBy),
		LockedUntil: timeValue(row.LockedUntil), NextAttemptAt: timeValue(row.NextAttemptAt),
		DiagnosticState: diagnostic, CompletedAt: timeValue(row.CompletedAt),
	}, nil
}

func pipelineDiagnosticJSON(value map[string]any) ([]byte, error) {
	if value == nil {
		value = map[string]any{}
	}
	out, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encode pipeline stage diagnostic state: %w", err)
	}
	return out, nil
}

func validPipelineStage(stage PipelineStage) bool {
	switch stage {
	case PipelineStageIdentify, PipelineStageConvert, PipelineStageChunk,
		PipelineStageEmbed, PipelineStageProject, PipelineStageActivate, PipelineStageCard:
		return true
	default:
		return false
	}
}
