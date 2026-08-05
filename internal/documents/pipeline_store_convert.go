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

func pipelineIDs(identity, document, version string) (pgtype.UUID, pgtype.UUID, pgtype.UUID, error) {
	identityID, err := pgUUID("pipeline identity id", strings.TrimSpace(identity))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, err
	}
	documentID, err := pgUUID("pipeline document id", strings.TrimSpace(document))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, err
	}
	versionID, err := pgUUID("pipeline version id", strings.TrimSpace(version))
	if err != nil {
		return pgtype.UUID{}, pgtype.UUID{}, pgtype.UUID{}, err
	}
	return identityID, documentID, versionID, nil
}

func candidateKeyParams(key CandidateKey) (sqlc.ListPipelineCandidateChunksParams, error) {
	identityID, err := pgUUID("candidate identity id", strings.TrimSpace(key.IdentityID))
	if err != nil {
		return sqlc.ListPipelineCandidateChunksParams{}, err
	}
	versionID, err := pgUUID("candidate version id", strings.TrimSpace(key.VersionID))
	if err != nil {
		return sqlc.ListPipelineCandidateChunksParams{}, err
	}
	if key.PipelineGeneration <= 0 {
		return sqlc.ListPipelineCandidateChunksParams{}, fmt.Errorf("candidate pipeline generation must be positive")
	}
	return sqlc.ListPipelineCandidateChunksParams{
		IdentityID: identityID, VersionID: versionID,
		PipelineGeneration: key.PipelineGeneration,
	}, nil
}

func candidateEmbeddingKeyParams(key CandidateKey) (sqlc.ListPipelineCandidateEmbeddingsParams, error) {
	params, err := candidateKeyParams(key)
	if err != nil {
		return sqlc.ListPipelineCandidateEmbeddingsParams{}, err
	}
	return sqlc.ListPipelineCandidateEmbeddingsParams(params), nil
}

func pipelinePassageFromSQL(row sqlc.AuraDocumentChunks) (Passage, error) {
	locator := PassageLocator{
		SelfRef: row.SelfRef, HeadingPath: append([]string(nil), row.HeadingPath...),
		Captions: append([]string(nil), row.Captions...), PageNumber: int4Pointer(row.PageNumber),
		CharStart: int4Pointer(row.CharStart), CharEnd: int4Pointer(row.CharEnd),
		SheetName: textString(row.SheetName), TableName: textString(row.TableName),
		RowNumber: int4Pointer(row.RowNumber), ColumnNumber: int4Pointer(row.ColumnNumber),
		CellRef: textString(row.CellRef),
	}
	if len(row.BoundingBox) != 0 {
		if err := json.Unmarshal(row.BoundingBox, &locator.BoundingBox); err != nil {
			return Passage{}, fmt.Errorf("decode passage bounding box: %w", err)
		}
	}
	return Passage{
		ID: uuidString(row.ID), IdentityID: uuidString(row.IdentityID),
		DocumentID: uuidString(row.DocumentID), SearchDocumentID: row.SearchDocumentID,
		VersionID: uuidString(row.VersionID), VersionNumber: int(row.VersionNumber),
		OriginalSHA256: row.OriginalSha256, PipelineGeneration: row.PipelineGeneration,
		Ordinal: int(row.Ordinal), Text: row.Text, ContextualizedText: row.Text,
		NormalizedTextSHA256: row.NormalizedTextSha256, Locator: locator,
	}, nil
}

func pipelineEmbeddingFromSQL(row sqlc.AuraDocumentEmbeddings) CandidateEmbedding {
	return CandidateEmbedding{
		ID: uuidString(row.ID), IdentityID: uuidString(row.IdentityID),
		DocumentID: uuidString(row.DocumentID), VersionID: uuidString(row.VersionID),
		PassageID: uuidString(row.ChunkID), PipelineGeneration: row.PipelineGeneration,
		EmbeddingModel: row.EmbeddingModel, EmbeddingVersion: row.EmbeddingVersion,
		EmbeddingDimension: int(row.EmbeddingDim), EmbeddingFingerprint: row.EmbeddingFingerprint,
		ProjectionFingerprint: row.VectorNamespace, ProjectionStatus: row.ProjectionStatus,
		ProjectedAt: timeValue(row.ProjectedAt), Active: row.Active,
	}
}

func activationParams(req ActivationRequest) (sqlc.ActivatePipelineCandidateParams, error) {
	identityID, documentID, versionID, err := pipelineIDs(req.IdentityID, req.DocumentID, req.VersionID)
	if err != nil {
		return sqlc.ActivatePipelineCandidateParams{}, err
	}
	assetID, err := pgUUID("activation asset id", strings.TrimSpace(req.AssetID))
	if err != nil {
		return sqlc.ActivatePipelineCandidateParams{}, err
	}
	jobID, err := pgUUID("activation job id", strings.TrimSpace(req.JobID))
	if err != nil {
		return sqlc.ActivatePipelineCandidateParams{}, err
	}
	if strings.TrimSpace(req.LockedBy) == "" || req.LeaseGeneration <= 0 || req.PipelineGeneration <= 0 {
		return sqlc.ActivatePipelineCandidateParams{}, fmt.Errorf("activation worker and generations are required")
	}
	counts := []int{req.ExpectedPassageCount, req.ExpectedEmbeddingCount, req.ExpectedProjectionCount}
	for _, count := range counts {
		if count < 0 || uint64(count) > math.MaxInt64 {
			return sqlc.ActivatePipelineCandidateParams{}, fmt.Errorf("activation expected count is invalid")
		}
	}
	return sqlc.ActivatePipelineCandidateParams{
		VersionID: versionID, AssetID: assetID, JobID: jobID,
		DocumentID: documentID, IdentityID: identityID,
		PipelineGeneration: req.PipelineGeneration, LockedBy: pgText(req.LockedBy),
		LeaseGeneration:         req.LeaseGeneration,
		ExpectedPassageCount:    int64(req.ExpectedPassageCount),
		ExpectedEmbeddingCount:  int64(req.ExpectedEmbeddingCount),
		ExpectedProjectionCount: int64(req.ExpectedProjectionCount),
		EventMessage:            strings.TrimSpace(req.EventMessage),
	}, nil
}

func int4Pointer(value pgtype.Int4) *int {
	if !value.Valid {
		return nil
	}
	converted := int(value.Int32)
	return &converted
}

func validatePassageLocator(locator PassageLocator) error {
	if locator.PageNumber != nil && *locator.PageNumber <= 0 {
		return fmt.Errorf("passage page number must be positive")
	}
	if locator.CharStart != nil && *locator.CharStart < 0 {
		return fmt.Errorf("passage character start must be non-negative")
	}
	if locator.CharEnd != nil && (locator.CharStart == nil || *locator.CharEnd < *locator.CharStart) {
		return fmt.Errorf("passage character end requires an ordered start")
	}
	for _, value := range []*int{locator.RowNumber, locator.ColumnNumber} {
		if value != nil && (*value < 0 || *value > math.MaxInt32) {
			return fmt.Errorf("passage row and column numbers must fit non-negative int32")
		}
	}
	for _, value := range []*int{locator.PageNumber, locator.CharStart, locator.CharEnd} {
		if value != nil && (*value < math.MinInt32 || *value > math.MaxInt32) {
			return fmt.Errorf("passage locator coordinate exceeds int32")
		}
	}
	if len(locator.BoundingBox) != 0 && len(locator.BoundingBox) != 4 {
		return fmt.Errorf("passage bounding box must contain four coordinates")
	}
	return nil
}

func pipelineOptionalUUID(field, value string) (pgtype.UUID, error) {
	if strings.TrimSpace(value) == "" {
		return pgtype.UUID{}, nil
	}
	return pgUUID(field, strings.TrimSpace(value))
}

func pipelineTime(value time.Time) pgtype.Timestamptz {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func pipelineInterval(value time.Duration) pgtype.Interval {
	return pgtype.Interval{Microseconds: value.Microseconds(), Valid: value > 0}
}
