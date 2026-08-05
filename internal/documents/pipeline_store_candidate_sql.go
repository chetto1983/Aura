package documents

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type preparedCandidate struct {
	version    sqlc.GetPipelineCandidateVersionParams
	chunks     []sqlc.UpsertPipelineCandidateChunkParams
	embeddings []sqlc.UpsertPipelineCandidateEmbeddingParams
}

func candidateVersionParams(req CandidateVersionRequest) (sqlc.ReservePipelineCandidateVersionParams, error) {
	identityID, err := pgUUID("candidate identity id", strings.TrimSpace(req.IdentityID))
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, err
	}
	documentID, err := pgUUID("candidate document id", strings.TrimSpace(req.DocumentID))
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, err
	}
	assetID, err := pgUUID("candidate asset id", strings.TrimSpace(req.AssetID))
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, err
	}
	bucket, objectKey := strings.TrimSpace(req.ObjectBucket), strings.TrimSpace(req.ObjectKey)
	if bucket == "" || objectKey == "" {
		return sqlc.ReservePipelineCandidateVersionParams{},
			fmt.Errorf("candidate object bucket and key are required")
	}
	sha256, err := canonicalSHA256(req.SHA256)
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, fmt.Errorf("candidate sha256: %w", err)
	}
	versionID, err := DeterministicVersionID(req.DocumentID, sha256)
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, err
	}
	pgVersionID, err := pgUUID("candidate version id", versionID)
	if err != nil {
		return sqlc.ReservePipelineCandidateVersionParams{}, err
	}
	if req.SizeBytes < 0 {
		return sqlc.ReservePipelineCandidateVersionParams{}, fmt.Errorf("candidate size must be non-negative")
	}
	status := strings.TrimSpace(req.Status)
	if status == "" {
		status = "queued"
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	retentionClass := strings.TrimSpace(req.RetentionClass)
	if retentionClass == "" {
		retentionClass = defaultRetentionClass
	}
	return sqlc.ReservePipelineCandidateVersionParams{
		DocumentID: documentID, IdentityID: identityID, Sha256: sha256,
		AssetID: assetID, ID: pgVersionID, Bucket: bucket, ObjectKey: objectKey,
		Etag: strings.TrimSpace(req.ObjectETag), RetentionClass: retentionClass,
		Status: status, Sha1: strings.TrimSpace(req.SHA1), ContentType: contentType,
		SizeBytes: req.SizeBytes, ChunkingConfigHash: strings.TrimSpace(req.ChunkingConfigHash),
		PipelineConfigHash: strings.TrimSpace(req.PipelineConfigHash),
	}, nil
}

func candidateVersionFromSQL(row sqlc.ReservePipelineCandidateVersionRow) CandidateVersion {
	return CandidateVersion{
		ID: uuidString(row.ID), IdentityID: uuidString(row.IdentityID),
		DocumentID: uuidString(row.DocumentID), SearchDocumentID: row.SearchDocumentID,
		AssetID: uuidString(row.AssetID), StorageObjectID: uuidString(row.StorageObjectID),
		VersionNumber: int(row.VersionNumber), PipelineGeneration: row.PipelineGeneration,
		SHA1: row.Sha1, SHA256: row.Sha256, ContentType: row.ContentType,
		SizeBytes: row.SizeBytes, Status: row.Status, Replayed: row.Replayed,
		// The statement's conjunction is total (IS NOT DISTINCT FROM), so the column is
		// never SQL NULL; sqlc types it nullable only because it crosses a UNION.
		ReplayedActive: row.ReplayIsActive.Bool,
	}
}

func derivedArtifactParams(req DerivedArtifactRequest) (sqlc.RecordPipelineDerivedArtifactParams, error) {
	identityID, documentID, versionID, err := pipelineIDs(req.IdentityID, req.DocumentID, req.VersionID)
	if err != nil {
		return sqlc.RecordPipelineDerivedArtifactParams{}, err
	}
	sha256, err := canonicalSHA256(req.SHA256)
	if err != nil {
		return sqlc.RecordPipelineDerivedArtifactParams{}, fmt.Errorf("artifact sha256: %w", err)
	}
	if req.PipelineGeneration <= 0 || req.SizeBytes < 0 {
		return sqlc.RecordPipelineDerivedArtifactParams{}, fmt.Errorf("artifact generation must be positive and size non-negative")
	}
	if !derivedArtifactKind(req.Kind) {
		return sqlc.RecordPipelineDerivedArtifactParams{}, fmt.Errorf("unsupported derived artifact kind %q", req.Kind)
	}
	bucket, objectKey := strings.TrimSpace(req.Bucket), strings.TrimSpace(req.ObjectKey)
	if bucket == "" || objectKey == "" {
		return sqlc.RecordPipelineDerivedArtifactParams{}, fmt.Errorf("artifact bucket and object key are required")
	}
	contentType := strings.TrimSpace(req.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	retentionClass := strings.TrimSpace(req.RetentionClass)
	if retentionClass == "" {
		retentionClass = "standard"
	}
	return sqlc.RecordPipelineDerivedArtifactParams{
		IdentityID: identityID, DocumentID: documentID, VersionID: versionID,
		Bucket: bucket, ObjectKey: objectKey, Kind: strings.TrimSpace(req.Kind),
		Sha256: sha256, SizeBytes: req.SizeBytes, ContentType: contentType,
		RetentionClass: retentionClass, PipelineGeneration: req.PipelineGeneration,
	}, nil
}

func derivedArtifactKind(kind string) bool {
	switch strings.TrimSpace(kind) {
	case "converted_document", "extracted_text", "chunks", "chunk_manifest", "embedding_manifest", "preview":
		return true
	default:
		return false
	}
}

func prepareCandidate(candidate CandidateGeneration) (preparedCandidate, error) {
	identityID, documentID, versionID, err := pipelineIDs(
		candidate.IdentityID, candidate.DocumentID, candidate.VersionID,
	)
	if err != nil {
		return preparedCandidate{}, err
	}
	if strings.TrimSpace(candidate.SearchDocumentID) == "" || candidate.VersionNumber <= 0 ||
		candidate.VersionNumber > math.MaxInt32 || candidate.PipelineGeneration <= 0 {
		return preparedCandidate{}, fmt.Errorf("candidate search id, version number, and generation must be valid")
	}
	rawSHA, err := canonicalSHA256(candidate.RawSHA256)
	if err != nil {
		return preparedCandidate{}, fmt.Errorf("candidate raw sha256: %w", err)
	}
	if len(candidate.Passages) == 0 || len(candidate.Passages) != len(candidate.Embeddings) {
		return preparedCandidate{}, fmt.Errorf("candidate passages and embeddings must have the same non-zero count")
	}
	if candidate.ProjectionCount < 0 || candidate.ProjectionCount > len(candidate.Passages) {
		return preparedCandidate{}, fmt.Errorf("candidate projection count is out of range")
	}
	for name, value := range map[string]string{
		"embedding model": candidate.EmbeddingModel, "embedding version": candidate.EmbeddingVersion,
		"embedding fingerprint":  candidate.EmbeddingFingerprint,
		"projection fingerprint": candidate.ProjectionFingerprint,
	} {
		if strings.TrimSpace(value) == "" {
			return preparedCandidate{}, fmt.Errorf("candidate %s is required", name)
		}
	}
	dimension, err := validateCandidateVectors(candidate.Embeddings)
	if err != nil {
		return preparedCandidate{}, err
	}
	projected := candidate.ProjectionCount == len(candidate.Passages)
	projectionStatus := "projecting"
	if projected {
		projectionStatus = "active"
	}
	prepared := preparedCandidate{
		version: sqlc.GetPipelineCandidateVersionParams{
			VersionID: versionID, IdentityID: identityID, DocumentID: documentID,
		},
		chunks:     make([]sqlc.UpsertPipelineCandidateChunkParams, 0, len(candidate.Passages)),
		embeddings: make([]sqlc.UpsertPipelineCandidateEmbeddingParams, 0, len(candidate.Passages)),
	}
	seenIDs, seenOrdinals := make(map[string]struct{}, len(candidate.Passages)), make(map[int]struct{}, len(candidate.Passages))
	for index, passage := range candidate.Passages {
		chunk, embedding, prepareErr := preparePassageCandidate(
			candidate, passage, candidate.Embeddings[index], dimension, projectionStatus,
			identityID, documentID, versionID, rawSHA,
		)
		if prepareErr != nil {
			return preparedCandidate{}, prepareErr
		}
		if _, duplicate := seenIDs[passage.ID]; duplicate {
			return preparedCandidate{}, fmt.Errorf("candidate contains duplicate passage id %s", passage.ID)
		}
		if _, duplicate := seenOrdinals[passage.Ordinal]; duplicate {
			return preparedCandidate{}, fmt.Errorf("candidate contains duplicate passage ordinal %d", passage.Ordinal)
		}
		seenIDs[passage.ID], seenOrdinals[passage.Ordinal] = struct{}{}, struct{}{}
		prepared.chunks = append(prepared.chunks, chunk)
		prepared.embeddings = append(prepared.embeddings, embedding)
	}
	return prepared, nil
}

func preparePassageCandidate(
	candidate CandidateGeneration,
	passage Passage,
	_ []float64,
	dimension int,
	projectionStatus string,
	identityID, documentID, versionID pgtype.UUID,
	rawSHA string,
) (sqlc.UpsertPipelineCandidateChunkParams, sqlc.UpsertPipelineCandidateEmbeddingParams, error) {
	passageID, err := pgUUID("candidate passage id", strings.TrimSpace(passage.ID))
	if err != nil {
		return sqlc.UpsertPipelineCandidateChunkParams{}, sqlc.UpsertPipelineCandidateEmbeddingParams{}, err
	}
	if passage.IdentityID != candidate.IdentityID || passage.DocumentID != candidate.DocumentID ||
		passage.SearchDocumentID != candidate.SearchDocumentID || passage.VersionID != candidate.VersionID ||
		passage.VersionNumber != candidate.VersionNumber || passage.PipelineGeneration != candidate.PipelineGeneration {
		return sqlc.UpsertPipelineCandidateChunkParams{}, sqlc.UpsertPipelineCandidateEmbeddingParams{},
			fmt.Errorf("passage %s coordinates do not match its candidate", passage.ID)
	}
	passageSHA, err := canonicalSHA256(passage.OriginalSHA256)
	if err != nil || passageSHA != rawSHA {
		return sqlc.UpsertPipelineCandidateChunkParams{}, sqlc.UpsertPipelineCandidateEmbeddingParams{},
			fmt.Errorf("passage %s raw sha256 does not match its candidate", passage.ID)
	}
	normalizedSHA, err := canonicalSHA256(passage.NormalizedTextSHA256)
	if err != nil || passage.Ordinal < 0 || passage.Ordinal > math.MaxInt32 || strings.TrimSpace(passage.ContextualizedText) == "" {
		return sqlc.UpsertPipelineCandidateChunkParams{}, sqlc.UpsertPipelineCandidateEmbeddingParams{},
			fmt.Errorf("passage %s has invalid text, hash, or ordinal", passage.ID)
	}
	if err = validatePassageLocator(passage.Locator); err != nil {
		return sqlc.UpsertPipelineCandidateChunkParams{}, sqlc.UpsertPipelineCandidateEmbeddingParams{},
			fmt.Errorf("passage %s locator: %w", passage.ID, err)
	}
	locator, err := json.Marshal(passage.Locator)
	if err != nil {
		return sqlc.UpsertPipelineCandidateChunkParams{}, sqlc.UpsertPipelineCandidateEmbeddingParams{}, err
	}
	box, err := optionalJSON(passage.Locator.BoundingBox)
	if err != nil {
		return sqlc.UpsertPipelineCandidateChunkParams{}, sqlc.UpsertPipelineCandidateEmbeddingParams{}, err
	}
	embeddingID := deterministicUUID(
		"aura.document.embedding.v1", passage.ID,
		strconv.FormatInt(candidate.PipelineGeneration, 10), candidate.EmbeddingFingerprint,
	)
	pgEmbeddingID, err := pgUUID("candidate embedding id", embeddingID)
	if err != nil {
		return sqlc.UpsertPipelineCandidateChunkParams{}, sqlc.UpsertPipelineCandidateEmbeddingParams{}, err
	}
	chunk := sqlc.UpsertPipelineCandidateChunkParams{
		ID: passageID, IdentityID: identityID, DocumentID: documentID, VersionID: versionID,
		Ordinal: int32(passage.Ordinal), NormalizedTextSha256: normalizedSHA,
		Text: passage.ContextualizedText, Locator: locator,
		SearchDocumentID: candidate.SearchDocumentID, VersionNumber: int32(candidate.VersionNumber),
		OriginalSha256: rawSHA, PipelineGeneration: candidate.PipelineGeneration,
		SelfRef: passage.Locator.SelfRef, HeadingPath: nonNilStrings(passage.Locator.HeadingPath),
		Captions: nonNilStrings(passage.Locator.Captions), PageNumber: optionalInt4(passage.Locator.PageNumber),
		BoundingBox: box, CharStart: optionalInt4(passage.Locator.CharStart),
		CharEnd: optionalInt4(passage.Locator.CharEnd), SheetName: pgText(passage.Locator.SheetName),
		TableName: pgText(passage.Locator.TableName), RowNumber: optionalInt4(passage.Locator.RowNumber),
		ColumnNumber: optionalInt4(passage.Locator.ColumnNumber), CellRef: pgText(passage.Locator.CellRef),
	}
	embedding := sqlc.UpsertPipelineCandidateEmbeddingParams{
		ID: pgEmbeddingID, IdentityID: identityID, DocumentID: documentID, VersionID: versionID,
		ChunkID: passageID, EmbeddingModel: candidate.EmbeddingModel,
		EmbeddingVersion: candidate.EmbeddingVersion, EmbeddingDim: int32(dimension),
		VectorNamespace: candidate.ProjectionFingerprint, VectorID: passage.ID,
		PipelineGeneration:   candidate.PipelineGeneration,
		EmbeddingFingerprint: candidate.EmbeddingFingerprint, ProjectionStatus: projectionStatus,
	}
	return chunk, embedding, nil
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func validateCandidateVectors(vectors [][]float64) (int, error) {
	dimension := len(vectors[0])
	if dimension == 0 || dimension > math.MaxInt32 {
		return 0, fmt.Errorf("candidate embedding dimension is invalid")
	}
	for i, vector := range vectors {
		if len(vector) != dimension {
			return 0, fmt.Errorf("candidate embedding %d has dimension %d, want %d", i, len(vector), dimension)
		}
		for _, value := range vector {
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return 0, fmt.Errorf("candidate embedding %d contains a non-finite value", i)
			}
		}
	}
	return dimension, nil
}

func optionalJSON(values []float64) ([]byte, error) {
	if len(values) == 0 {
		return nil, nil
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, fmt.Errorf("passage bounding box contains a non-finite value")
		}
	}
	return json.Marshal(values)
}

func optionalInt4(value *int) pgtype.Int4 {
	if value == nil {
		return pgtype.Int4{}
	}
	return pgtype.Int4{Int32: int32(*value), Valid: *value >= math.MinInt32 && *value <= math.MaxInt32}
}

func candidateMatchesVersion(candidate CandidateGeneration, row sqlc.GetPipelineCandidateVersionRow) bool {
	return int(row.VersionNumber) == candidate.VersionNumber &&
		row.Sha256 == strings.ToLower(strings.TrimSpace(candidate.RawSHA256)) &&
		row.SearchDocumentID == candidate.SearchDocumentID &&
		row.PipelineGeneration == candidate.PipelineGeneration
}
