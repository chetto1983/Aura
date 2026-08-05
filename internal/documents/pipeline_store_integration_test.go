//go:build db_integration

package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPipelineStoreCandidateActivationAndRollback(t *testing.T) {
	pool := pipelineDisposablePool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	store := NewPostgresPipelineStore(pool)
	fixture := newPipelineStoreFixture(t, ctx, pool, store)

	if fixture.v1.VersionNumber != 1 || fixture.v1.PipelineGeneration != 1 || fixture.v1.Replayed {
		t.Fatalf("first candidate version = %#v", fixture.v1)
	}
	if !fixture.v1Replay.Replayed || fixture.v1Replay.ID != fixture.v1.ID ||
		fixture.v1Replay.PipelineGeneration != fixture.v1.PipelineGeneration {
		t.Fatalf("version replay = %#v, first = %#v", fixture.v1Replay, fixture.v1)
	}
	if fixture.v2.VersionNumber != 2 || fixture.v2.PipelineGeneration <= fixture.v1.PipelineGeneration ||
		fixture.v2.PipelineGeneration < int64(fixture.v2.VersionNumber) {
		t.Fatalf("second candidate version = %#v", fixture.v2)
	}

	artifactReq := DerivedArtifactRequest{
		IdentityID: fixture.identityID, DocumentID: fixture.documentID, VersionID: fixture.v2.ID,
		PipelineGeneration: fixture.v2.PipelineGeneration, Bucket: "documents",
		ObjectKey: fixture.prefix + "/derived/chunks.json", Kind: "chunk_manifest",
		SHA256: strings.Repeat("c", 64), SizeBytes: 123, ContentType: "application/json",
	}
	artifactID, err := store.RecordDerivedArtifact(ctx, artifactReq)
	if err != nil {
		t.Fatalf("RecordDerivedArtifact: %v", err)
	}
	replayedArtifactID, err := store.RecordDerivedArtifact(ctx, artifactReq)
	if err != nil || replayedArtifactID != artifactID {
		t.Fatalf("artifact replay = (%q, %v), want %q", replayedArtifactID, err, artifactID)
	}
	driftedArtifact := artifactReq
	driftedArtifact.SizeBytes++
	if _, err := store.RecordDerivedArtifact(ctx, driftedArtifact); !errors.Is(err, ErrPipelineCandidateRejected) {
		t.Fatalf("artifact drift error = %v", err)
	}

	testPipelineStageFences(t, ctx, store, fixture, artifactID)

	staleCandidate := pipelineCandidateForVersion(t, fixture, fixture.v1, 1)
	if err := store.SaveCandidate(ctx, staleCandidate); err != nil {
		t.Fatalf("SaveCandidate stale generation: %v", err)
	}
	staleJobID := seedPipelineIngestionJob(t, ctx, pool, fixture, fixture.asset1ID, "worker-stale")
	if err := store.BindJobCandidate(ctx, bindingFor(
		fixture, fixture.v1, fixture.asset1ID, staleJobID, "worker-stale",
	)); err != nil {
		t.Fatalf("BindJobCandidate stale generation: %v", err)
	}
	if err := store.ActivateCandidate(ctx, activationFor(
		fixture, fixture.v1, fixture.asset1ID, staleJobID, "worker-stale", 1,
	)); !errors.Is(err, ErrPipelineActivationRejected) {
		t.Fatalf("stale candidate activation error = %v", err)
	}
	assertPipelineUnpublished(t, ctx, pool, fixture, fixture.v1, fixture.asset1ID, staleJobID)

	candidate := pipelineCandidateForVersion(t, fixture, fixture.v2, 2)
	if err := store.SaveCandidate(ctx, candidate); err != nil {
		t.Fatalf("SaveCandidate: %v", err)
	}
	passages, err := store.ListPassages(ctx, CandidateKey{
		IdentityID: fixture.identityID, VersionID: fixture.v2.ID,
		PipelineGeneration: fixture.v2.PipelineGeneration,
	})
	if err != nil || len(passages) != 2 || passages[0].Ordinal != 0 || passages[1].Ordinal != 1 {
		t.Fatalf("ListPassages = (%#v, %v)", passages, err)
	}
	embeddings, err := store.ListEmbeddings(ctx, CandidateKey{
		IdentityID: fixture.identityID, VersionID: fixture.v2.ID,
		PipelineGeneration: fixture.v2.PipelineGeneration,
	})
	if err != nil || len(embeddings) != 2 || embeddings[0].ProjectionStatus != "active" {
		t.Fatalf("ListEmbeddings = (%#v, %v)", embeddings, err)
	}

	badCandidate := pipelineCandidateForVersion(t, fixture, fixture.v2, 3)
	badFirst := badCandidate.Passages[0]
	badFirst.Ordinal = 3
	badCandidate.Passages = []Passage{badCandidate.Passages[2], badFirst, badCandidate.Passages[1]}
	badCandidate.Embeddings = [][]float64{
		badCandidate.Embeddings[2], badCandidate.Embeddings[0], badCandidate.Embeddings[1],
	}
	if err := store.SaveCandidate(ctx, badCandidate); !errors.Is(err, ErrPipelineCandidateRejected) {
		t.Fatalf("drifted candidate error = %v", err)
	}
	passages, err = store.ListPassages(ctx, CandidateKey{
		IdentityID: fixture.identityID, VersionID: fixture.v2.ID,
		PipelineGeneration: fixture.v2.PipelineGeneration,
	})
	if err != nil || len(passages) != 2 {
		t.Fatalf("candidate rollback left passages = (%d, %v), want 2", len(passages), err)
	}

	jobID := seedPipelineIngestionJob(t, ctx, pool, fixture, fixture.asset2ID, "worker-active")
	wrongBinding := bindingFor(fixture, fixture.v1, fixture.asset2ID, jobID, "worker-active")
	if err := store.BindJobCandidate(ctx, wrongBinding); !errors.Is(err, ErrPipelineActivationRejected) {
		t.Fatalf("incoherent job binding error = %v", err)
	}
	wrongBinding = bindingFor(fixture, fixture.v2, fixture.asset2ID, jobID, "worker-active")
	wrongBinding.LeaseGeneration++
	if err := store.BindJobCandidate(ctx, wrongBinding); !errors.Is(err, ErrIngestionJobLeaseLost) {
		t.Fatalf("stale job binding fence error = %v", err)
	}
	if err := store.BindJobCandidate(ctx, bindingFor(
		fixture, fixture.v2, fixture.asset2ID, jobID, "worker-active",
	)); err != nil {
		t.Fatalf("BindJobCandidate: %v", err)
	}
	request := activationFor(fixture, fixture.v2, fixture.asset2ID, jobID, "worker-active", 2)
	wrongFence := request
	wrongFence.LeaseGeneration++
	if err := store.ActivateCandidate(ctx, wrongFence); !errors.Is(err, ErrPipelineActivationRejected) {
		t.Fatalf("stale activation fence error = %v", err)
	}
	request.ExpectedProjectionCount = 1
	if err := store.ActivateCandidate(ctx, request); !errors.Is(err, ErrPipelineActivationRejected) {
		t.Fatalf("incomplete candidate activation error = %v", err)
	}
	assertPipelineUnpublished(t, ctx, pool, fixture, fixture.v2, fixture.asset2ID, jobID)

	request.ExpectedProjectionCount = 2
	if err := store.ActivateCandidate(ctx, request); err != nil {
		t.Fatalf("ActivateCandidate: %v", err)
	}
	assertPipelinePublished(t, ctx, pool, fixture, jobID)
	testCreateDocumentVersionAllocatesNextGeneration(t, ctx, pool, fixture)
	if err := store.ActivateCandidate(ctx, request); !errors.Is(err, ErrPipelineActivationRejected) {
		t.Fatalf("replayed activation error = %v", err)
	}

	foreignIdentity := seedDocumentTestIdentity(t, ctx, pool)
	foreignPassages, err := store.ListPassages(ctx, CandidateKey{
		IdentityID: foreignIdentity, VersionID: fixture.v2.ID,
		PipelineGeneration: fixture.v2.PipelineGeneration,
	})
	if err != nil || len(foreignPassages) != 0 {
		t.Fatalf("foreign owner passages = (%#v, %v)", foreignPassages, err)
	}
}

func testCreateDocumentVersionAllocatesNextGeneration(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture pipelineStoreFixture,
) {
	t.Helper()
	assetID, storageID, request := seedPipelineRawObject(
		t, ctx, pool, fixture, "three", strings.Repeat("d", 64),
	)
	versionID, err := DeterministicVersionID(fixture.documentID, request.SHA256)
	if err != nil {
		t.Fatalf("third DeterministicVersionID: %v", err)
	}
	identity, _ := pgUUID("identity", fixture.identityID)
	document, _ := pgUUID("document", fixture.documentID)
	asset, _ := pgUUID("asset", assetID)
	storage, _ := pgUUID("storage", storageID)
	version, _ := pgUUID("version", versionID)
	var created sqlc.AuraDocumentVersions
	err = asDocumentIdentity(ctx, pool, fixture.identityID, func(tx pgx.Tx) error {
		var queryErr error
		created, queryErr = sqlc.New(tx).CreateDocumentVersion(ctx, sqlc.CreateDocumentVersionParams{
			ID: version, AssetID: asset, Status: "queued", Sha256: request.SHA256,
			ContentType: request.ContentType, SizeBytes: request.SizeBytes,
			StorageObjectID: storage, DocumentID: document, IdentityID: identity,
			PipelineGeneration: 1,
		})
		return queryErr
	})
	if err != nil {
		t.Fatalf("CreateDocumentVersion third: %v", err)
	}
	if created.VersionNumber != 3 || created.PipelineGeneration != 3 {
		t.Fatalf("legacy CreateDocumentVersion slot = version:%d generation:%d, want 3/3",
			created.VersionNumber, created.PipelineGeneration)
	}
}

type pipelineStoreFixture struct {
	identityID, documentID, searchID, prefix string
	asset1ID, asset2ID                       string
	v1, v1Replay, v2                         CandidateVersion
}

func newPipelineStoreFixture(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	store *PostgresPipelineStore,
) pipelineStoreFixture {
	t.Helper()
	fixture := pipelineStoreFixture{
		identityID: seedDocumentTestIdentity(t, ctx, pool), documentID: uuid.NewString(),
		searchID: "doc_pipeline_" + uuid.NewString(), prefix: "pipeline-test/" + uuid.NewString(),
	}
	if err := asDocumentIdentity(ctx, pool, fixture.identityID, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
INSERT INTO aura.documents (
    id, identity_id, scope, title, status, source_kind, source_key,
    search_document_id, pipeline_generation
) VALUES ($1, $2, 'library', 'Pipeline integration', 'accepted', 'cli', $3, $4, 0)`,
			fixture.documentID, fixture.identityID, fixture.prefix, fixture.searchID)
		return err
	}); err != nil {
		t.Fatalf("seed pipeline document: %v", err)
	}

	var request1 CandidateVersionRequest
	fixture.asset1ID, request1 = seedPipelineAsset(
		t, ctx, pool, fixture, "one", strings.Repeat("a", 64),
	)
	var err error
	fixture.v1, err = store.ReserveCandidateVersion(ctx, request1)
	if err != nil {
		t.Fatalf("ReserveCandidateVersion one: %v", err)
	}
	fixture.v1Replay, err = store.ReserveCandidateVersion(ctx, request1)
	if err != nil {
		t.Fatalf("ReserveCandidateVersion replay: %v", err)
	}
	var request2 CandidateVersionRequest
	fixture.asset2ID, request2 = seedPipelineAsset(
		t, ctx, pool, fixture, "two", strings.Repeat("b", 64),
	)
	fixture.v2, err = store.ReserveCandidateVersion(ctx, request2)
	if err != nil {
		t.Fatalf("ReserveCandidateVersion two: %v", err)
	}
	return fixture
}

// seedPipelineAsset inserts ONLY the asset row. The storage object is deliberately absent:
// ReservePipelineCandidateVersion writes that ledger row itself, so pre-seeding it would
// drive the statement's ON CONFLICT leg on every call and never its INSERT leg — which is
// the leg that has to prove the deferred foreign keys let an object name a version that
// does not exist yet.
func seedPipelineAsset(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture pipelineStoreFixture,
	suffix, sha256 string,
) (string, CandidateVersionRequest) {
	t.Helper()
	assetID := uuid.NewString()
	objectKey := fixture.prefix + "/raw-" + suffix
	err := asDocumentIdentity(ctx, pool, fixture.identityID, func(tx pgx.Tx) error {
		_, execErr := tx.Exec(ctx, `
INSERT INTO aura.assets (
    id, identity_id, source_kind, source_ref, scope, modality, status,
    file_name, mime_type, size_bytes, content_hash, object_bucket, object_key
) VALUES ($1, $2, 'cli', $3, 'library', 'document', 'accepted',
          $4, 'application/pdf', 42, $5, 'documents', $6)`,
			assetID, fixture.identityID, objectKey, suffix+".pdf", sha256, objectKey)
		return execErr
	})
	if err != nil {
		t.Fatalf("seed asset %s: %v", suffix, err)
	}
	return assetID, CandidateVersionRequest{
		IdentityID: fixture.identityID, DocumentID: fixture.documentID,
		AssetID: assetID, ObjectBucket: "documents", ObjectKey: objectKey,
		SHA256: sha256, ContentType: "application/pdf", SizeBytes: 42, Status: "queued",
	}
}

// seedPipelineRawObject adds the raw ledger row on top, for the one test that drives the
// legacy CreateDocumentVersion statement — which still requires the object to pre-exist.
func seedPipelineRawObject(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture pipelineStoreFixture,
	suffix, sha256 string,
) (string, string, CandidateVersionRequest) {
	t.Helper()
	assetID, request := seedPipelineAsset(t, ctx, pool, fixture, suffix, sha256)
	storageID := uuid.NewString()
	err := asDocumentIdentity(ctx, pool, fixture.identityID, func(tx pgx.Tx) error {
		_, execErr := tx.Exec(ctx, `
INSERT INTO aura.storage_objects (
    id, identity_id, document_id, asset_id, bucket, object_key, kind,
    sha256, size_bytes, content_type, status, pipeline_generation
) VALUES ($1, $2, $3, $4, 'documents', $5, 'raw', $6, 42,
          'application/pdf', 'live', 0)`,
			storageID, fixture.identityID, fixture.documentID, assetID, request.ObjectKey, sha256)
		return execErr
	})
	if err != nil {
		t.Fatalf("seed raw object %s: %v", suffix, err)
	}
	return assetID, storageID, request
}

func pipelineCandidateForVersion(
	t *testing.T,
	fixture pipelineStoreFixture,
	version CandidateVersion,
	count int,
) CandidateGeneration {
	t.Helper()
	candidate := CandidateGeneration{
		IdentityID: fixture.identityID, DocumentID: fixture.documentID,
		SearchDocumentID: fixture.searchID, VersionID: version.ID,
		VersionNumber: version.VersionNumber, RawSHA256: version.SHA256,
		PipelineGeneration: version.PipelineGeneration,
		EmbeddingModel:     "embeddinggemma", EmbeddingVersion: "revision-1",
		EmbeddingFingerprint:  "embed-fingerprint-v1",
		ProjectionFingerprint: "projection-fingerprint-v1", ProjectionCount: count,
	}
	for ordinal := 0; ordinal < count; ordinal++ {
		text := fmt.Sprintf("pipeline passage %d", ordinal)
		normalizedSHA := NormalizedTextSHA256(text)
		passageID, err := DeterministicPassageID(version.ID, fmt.Sprintf("page:%d", ordinal+1), 0, normalizedSHA)
		if err != nil {
			t.Fatalf("DeterministicPassageID: %v", err)
		}
		page := ordinal + 1
		candidate.Passages = append(candidate.Passages, Passage{
			ID: passageID, IdentityID: fixture.identityID, DocumentID: fixture.documentID,
			SearchDocumentID: fixture.searchID, VersionID: version.ID,
			VersionNumber: version.VersionNumber, OriginalSHA256: version.SHA256,
			PipelineGeneration: version.PipelineGeneration, Ordinal: ordinal,
			Text: text, ContextualizedText: "title: Pipeline integration | text: " + text,
			NormalizedTextSHA256: normalizedSHA, Locator: PassageLocator{PageNumber: &page},
		})
		candidate.Embeddings = append(candidate.Embeddings, []float64{float64(ordinal), 0.25, 1})
	}
	return candidate
}

func testPipelineStageFences(
	t *testing.T,
	ctx context.Context,
	store *PostgresPipelineStore,
	fixture pipelineStoreFixture,
	artifactID string,
) {
	t.Helper()
	first, err := store.UpsertStage(ctx, StageUpsert{
		IdentityID: fixture.identityID, DocumentID: fixture.documentID, VersionID: fixture.v2.ID,
		Stage: PipelineStageConvert, InputFingerprint: "convert-input", ProducerVersion: "docling-1",
		PipelineGeneration: fixture.v2.PipelineGeneration, MaxAttempts: 2,
	})
	if err != nil {
		t.Fatalf("UpsertStage first: %v", err)
	}
	second, err := store.UpsertStage(ctx, StageUpsert{
		IdentityID: fixture.identityID, DocumentID: fixture.documentID, VersionID: fixture.v2.ID,
		Stage: PipelineStageChunk, InputFingerprint: "chunk-input", ProducerVersion: "chunker-1",
		PipelineGeneration: fixture.v2.PipelineGeneration, MaxAttempts: 2,
	})
	if err != nil {
		t.Fatalf("UpsertStage second: %v", err)
	}
	claimed, err := store.ClaimStage(ctx, StageClaim{
		IdentityID: fixture.identityID, StageID: second.ID, Stage: PipelineStageChunk,
		WorkerID: "stage-worker", LeaseDuration: time.Minute,
	})
	if err != nil || claimed.ID != second.ID || claimed.ID == first.ID {
		t.Fatalf("exact stage claim = (%#v, %v)", claimed, err)
	}
	completion := StageCompletion{
		StageFence: StageFence{
			IdentityID: fixture.identityID, StageID: claimed.ID,
			WorkerID: "stage-worker", LeaseGeneration: claimed.LeaseGeneration,
		},
		ArtifactStorageObjectID: artifactID, ArtifactObjectKey: fixture.prefix + "/derived/chunks.json",
		ArtifactSHA256: strings.Repeat("c", 64), ArtifactSizeBytes: 123,
		DiagnosticState: map[string]any{"passages": 2},
	}
	if err := store.CompleteStage(ctx, completion); err != nil {
		t.Fatalf("CompleteStage: %v", err)
	}
	if err := store.CompleteStage(ctx, completion); !errors.Is(err, ErrPipelineStageLeaseLost) {
		t.Fatalf("stale CompleteStage error = %v", err)
	}

	retryStage, err := store.UpsertStage(ctx, StageUpsert{
		IdentityID: fixture.identityID, DocumentID: fixture.documentID, VersionID: fixture.v2.ID,
		Stage: PipelineStageEmbed, InputFingerprint: "embed-input", ProducerVersion: "embedder-1",
		PipelineGeneration: fixture.v2.PipelineGeneration, MaxAttempts: 2,
	})
	if err != nil {
		t.Fatalf("UpsertStage retry: %v", err)
	}
	claimedOnce, err := store.ClaimStage(ctx, StageClaim{
		IdentityID: fixture.identityID, StageID: retryStage.ID, Stage: PipelineStageEmbed,
		WorkerID: "retry-worker", LeaseDuration: time.Minute,
	})
	if err != nil {
		t.Fatalf("ClaimStage once: %v", err)
	}
	if err := store.FailStage(ctx, StageFailure{
		StageFence: StageFence{IdentityID: fixture.identityID, StageID: retryStage.ID,
			WorkerID: "retry-worker", LeaseGeneration: claimedOnce.LeaseGeneration},
		Retryable: true, ErrorClass: "transient", ErrorCode: "timeout",
		RedactedMessage: "converter unavailable", NextAttemptAt: time.Now().Add(-time.Second),
	}); err != nil {
		t.Fatalf("FailStage retry: %v", err)
	}
	claimedTwice, err := store.ClaimStage(ctx, StageClaim{
		IdentityID: fixture.identityID, StageID: retryStage.ID, Stage: PipelineStageEmbed,
		WorkerID: "retry-worker", LeaseDuration: time.Minute,
	})
	if err != nil || claimedTwice.LeaseGeneration != claimedOnce.LeaseGeneration+1 {
		t.Fatalf("ClaimStage twice = (%#v, %v)", claimedTwice, err)
	}
	if err := store.HeartbeatStage(ctx, StageFence{
		IdentityID: fixture.identityID, StageID: retryStage.ID, WorkerID: "retry-worker",
		LeaseGeneration: claimedOnce.LeaseGeneration, LeaseDuration: time.Minute,
	}); !errors.Is(err, ErrPipelineStageLeaseLost) {
		t.Fatalf("stale HeartbeatStage error = %v", err)
	}
	if err := store.FailStage(ctx, StageFailure{
		StageFence: StageFence{IdentityID: fixture.identityID, StageID: retryStage.ID,
			WorkerID: "retry-worker", LeaseGeneration: claimedTwice.LeaseGeneration},
		Retryable: true, ErrorClass: "transient", ErrorCode: "timeout",
		RedactedMessage: "converter unavailable", NextAttemptAt: time.Now(),
	}); err != nil {
		t.Fatalf("FailStage dead letter: %v", err)
	}
	if _, err := store.ClaimStage(ctx, StageClaim{
		IdentityID: fixture.identityID, StageID: retryStage.ID, Stage: PipelineStageEmbed,
		WorkerID: "retry-worker", LeaseDuration: time.Minute,
	}); !errors.Is(err, ErrPipelineStageUnavailable) {
		t.Fatalf("dead-letter stage claim error = %v", err)
	}
}

func seedPipelineIngestionJob(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture pipelineStoreFixture,
	assetID, workerID string,
) string {
	t.Helper()
	jobID := uuid.NewString()
	err := asDocumentIdentity(ctx, pool, fixture.identityID, func(tx pgx.Tx) error {
		_, execErr := tx.Exec(ctx, `
INSERT INTO aura.ingestion_jobs (
    id, identity_id, job_type, asset_id, status,
    idempotency_key, stage, attempt_count, max_attempts, locked_by, locked_until,
    next_attempt_at, payload, pipeline_generation, attempt_generation, lease_generation
) VALUES ($1, $2, 'document_pipeline', $3, 'running', $4,
          '', 1, 5, $5, now() + interval '5 minutes', now(), '{}', 0, 1, 1)`,
			jobID, fixture.identityID, assetID, "pipeline:"+jobID, workerID)
		return execErr
	})
	if err != nil {
		t.Fatalf("seed ingestion job: %v", err)
	}
	return jobID
}

func bindingFor(
	fixture pipelineStoreFixture,
	version CandidateVersion,
	assetID, jobID, workerID string,
) JobCandidateBinding {
	return JobCandidateBinding{
		IdentityID: fixture.identityID, JobID: jobID, AssetID: assetID,
		DocumentID: fixture.documentID, VersionID: version.ID, WorkerID: workerID,
		LeaseGeneration: 1, PipelineGeneration: version.PipelineGeneration,
		Stage: PipelineStageConvert,
	}
}

func activationFor(
	fixture pipelineStoreFixture,
	version CandidateVersion,
	assetID, jobID, workerID string,
	count int,
) ActivationRequest {
	return ActivationRequest{
		IdentityID: fixture.identityID, DocumentID: fixture.documentID,
		VersionID: version.ID, AssetID: assetID, JobID: jobID, LockedBy: workerID,
		LeaseGeneration: 1, PipelineGeneration: version.PipelineGeneration,
		ExpectedPassageCount: count, ExpectedEmbeddingCount: count,
		ExpectedProjectionCount: count, EventMessage: "candidate ready",
	}
}

func assertPipelineUnpublished(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture pipelineStoreFixture,
	version CandidateVersion,
	assetID, jobID string,
) {
	t.Helper()
	var documentStatus, activeVersion, versionStatus, assetStatus, jobStatus string
	var generation int64
	var activeChunks, activeEmbeddings, events int
	err := asDocumentIdentity(ctx, pool, fixture.identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
SELECT document.status, coalesce(document.active_version_id::text, ''), document.pipeline_generation,
       version.status, asset.status, job.status,
       (SELECT count(*) FROM aura.document_chunks WHERE identity_id = $1 AND document_id = $2 AND active),
       (SELECT count(*) FROM aura.document_embeddings WHERE identity_id = $1 AND document_id = $2 AND active),
       (SELECT count(*) FROM aura.ingestion_events WHERE identity_id = $1 AND job_id = $5)
FROM aura.documents document
JOIN aura.document_versions version ON version.id = $3 AND version.identity_id = document.identity_id
JOIN aura.assets asset ON asset.id = $4 AND asset.identity_id = document.identity_id
JOIN aura.ingestion_jobs job ON job.id = $5 AND job.identity_id = document.identity_id
WHERE document.id = $2 AND document.identity_id = $1`,
			fixture.identityID, fixture.documentID, version.ID, assetID, jobID).Scan(
			&documentStatus, &activeVersion, &generation, &versionStatus, &assetStatus, &jobStatus,
			&activeChunks, &activeEmbeddings, &events,
		)
	})
	if err != nil {
		t.Fatalf("read unpublished pipeline: %v", err)
	}
	if documentStatus == "ready" || activeVersion != "" || versionStatus == "ready" ||
		assetStatus == "searchable" || jobStatus != "running" || activeChunks != 0 ||
		activeEmbeddings != 0 || events != 0 || generation != 0 {
		t.Fatalf("partial publication: doc=%s active=%s gen=%d version=%s asset=%s job=%s chunks=%d embeddings=%d events=%d",
			documentStatus, activeVersion, generation, versionStatus, assetStatus, jobStatus,
			activeChunks, activeEmbeddings, events)
	}
}

func assertPipelinePublished(
	t *testing.T,
	ctx context.Context,
	pool *pgxpool.Pool,
	fixture pipelineStoreFixture,
	jobID string,
) {
	t.Helper()
	var documentStatus, activeVersion, versionStatus, assetStatus, jobStatus, eventType string
	var generation int64
	var activeChunks, activeEmbeddings int
	err := asDocumentIdentity(ctx, pool, fixture.identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `
SELECT document.status, document.active_version_id::text, document.pipeline_generation,
       version.status, asset.status, job.status,
       (SELECT count(*) FROM aura.document_chunks WHERE identity_id = $1 AND document_id = $2 AND active),
       (SELECT count(*) FROM aura.document_embeddings WHERE identity_id = $1 AND document_id = $2 AND active),
       event.event_type
FROM aura.documents document
JOIN aura.document_versions version ON version.id = $3 AND version.identity_id = document.identity_id
JOIN aura.assets asset ON asset.id = $4 AND asset.identity_id = document.identity_id
JOIN aura.ingestion_jobs job ON job.id = $5 AND job.identity_id = document.identity_id
JOIN aura.ingestion_events event ON event.job_id = job.id AND event.identity_id = job.identity_id
WHERE document.id = $2 AND document.identity_id = $1`,
			fixture.identityID, fixture.documentID, fixture.v2.ID, fixture.asset2ID, jobID).Scan(
			&documentStatus, &activeVersion, &generation, &versionStatus, &assetStatus, &jobStatus,
			&activeChunks, &activeEmbeddings, &eventType,
		)
	})
	if err != nil {
		t.Fatalf("read published pipeline: %v", err)
	}
	if documentStatus != "ready" || activeVersion != fixture.v2.ID || generation != fixture.v2.PipelineGeneration ||
		versionStatus != "ready" || assetStatus != "searchable" || jobStatus != "succeeded" ||
		activeChunks != 2 || activeEmbeddings != 2 || eventType != "candidate_activated" {
		t.Fatalf("published pipeline: doc=%s active=%s gen=%d version=%s asset=%s job=%s chunks=%d embeddings=%d event=%s",
			documentStatus, activeVersion, generation, versionStatus, assetStatus, jobStatus,
			activeChunks, activeEmbeddings, eventType)
	}
}
