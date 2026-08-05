package documents

import (
	"math"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

var _ PipelinePersistence = (*PostgresPipelineStore)(nil)

func TestPipelineStorePreparesDeterministicCandidateRows(t *testing.T) {
	candidate := pipelineStoreCandidateFixture(t, 2)
	prepared, err := prepareCandidate(candidate)
	if err != nil {
		t.Fatalf("prepareCandidate: %v", err)
	}
	again, err := prepareCandidate(candidate)
	if err != nil {
		t.Fatalf("prepareCandidate replay: %v", err)
	}
	if len(prepared.chunks) != 2 || len(prepared.embeddings) != 2 {
		t.Fatalf("prepared counts = chunks:%d embeddings:%d", len(prepared.chunks), len(prepared.embeddings))
	}
	for i := range prepared.embeddings {
		if prepared.embeddings[i].ID != again.embeddings[i].ID {
			t.Fatalf("embedding %d id changed across replay", i)
		}
		if prepared.embeddings[i].ProjectionStatus != "active" || prepared.embeddings[i].EmbeddingDim != 3 {
			t.Fatalf("embedding %d = %#v", i, prepared.embeddings[i])
		}
	}
	if prepared.chunks[0].Text != candidate.Passages[0].ContextualizedText ||
		prepared.chunks[0].Text == candidate.Passages[0].Text {
		t.Fatalf("stored passage text = %q", prepared.chunks[0].Text)
	}
	if prepared.chunks[0].HeadingPath == nil || prepared.chunks[0].Captions == nil {
		t.Fatal("prepared candidate left required locator arrays nil")
	}

	partial := candidate
	partial.ProjectionCount = 1
	partialRows, err := prepareCandidate(partial)
	if err != nil {
		t.Fatalf("prepare partial candidate: %v", err)
	}
	for _, embedding := range partialRows.embeddings {
		if embedding.ProjectionStatus != "projecting" {
			t.Fatalf("partial projection status = %q", embedding.ProjectionStatus)
		}
	}
}

func TestPipelineStoreRejectsCandidateCoordinateAndVectorDrift(t *testing.T) {
	candidate := pipelineStoreCandidateFixture(t, 1)
	candidate.Passages[0].PipelineGeneration++
	if _, err := prepareCandidate(candidate); err == nil {
		t.Fatal("prepareCandidate accepted a passage from another generation")
	}

	candidate = pipelineStoreCandidateFixture(t, 1)
	candidate.Embeddings[0][0] = math.NaN()
	if _, err := prepareCandidate(candidate); err == nil {
		t.Fatal("prepareCandidate accepted a non-finite embedding")
	}

	candidate = pipelineStoreCandidateFixture(t, 1)
	candidate.Passages[0].Locator.BoundingBox = []float64{1, 2, 3}
	if _, err := prepareCandidate(candidate); err == nil {
		t.Fatal("prepareCandidate accepted a malformed bounding box")
	}
}

func TestPipelineStoreVersionArtifactAndFenceParameters(t *testing.T) {
	req := CandidateVersionRequest{
		IdentityID: uuid.NewString(), DocumentID: uuid.NewString(), AssetID: uuid.NewString(),
		StorageObjectID: uuid.NewString(), SHA256: strings.Repeat("a", 64), SizeBytes: 12,
	}
	params, err := candidateVersionParams(req)
	if err != nil {
		t.Fatalf("candidateVersionParams: %v", err)
	}
	wantID, _ := DeterministicVersionID(req.DocumentID, req.SHA256)
	if uuidString(params.ID) != wantID || params.Status != "queued" || params.ContentType != "application/octet-stream" {
		t.Fatalf("version params = %#v", params)
	}

	artifact := DerivedArtifactRequest{
		IdentityID: req.IdentityID, DocumentID: req.DocumentID, VersionID: wantID,
		PipelineGeneration: 3, Bucket: "documents", ObjectKey: "derived/chunks.json",
		Kind: "chunk_manifest", SHA256: strings.Repeat("b", 64), SizeBytes: 30,
	}
	artifactParams, err := derivedArtifactParams(artifact)
	if err != nil {
		t.Fatalf("derivedArtifactParams: %v", err)
	}
	if artifactParams.RetentionClass != "standard" || artifactParams.ContentType != "application/octet-stream" {
		t.Fatalf("artifact defaults = %#v", artifactParams)
	}
	artifact.Kind = "raw"
	if _, err := derivedArtifactParams(artifact); err == nil {
		t.Fatal("derivedArtifactParams accepted a non-derived kind")
	}

	stageID := uuid.NewString()
	claim, err := stageClaimParams(StageClaim{
		IdentityID: req.IdentityID, StageID: stageID, Stage: PipelineStageChunk,
		WorkerID: "worker-1", LeaseDuration: time.Minute,
	})
	if err != nil || uuidString(claim.ID) != stageID {
		t.Fatalf("stageClaimParams = (%#v, %v)", claim, err)
	}
	if _, err := stageClaimParams(StageClaim{
		IdentityID: req.IdentityID, Stage: PipelineStageChunk,
		WorkerID: "worker-1", LeaseDuration: time.Minute,
	}); err == nil {
		t.Fatal("stageClaimParams accepted a claim without stage id")
	}
	binding, err := jobCandidateBindingParams(JobCandidateBinding{
		IdentityID: req.IdentityID, JobID: uuid.NewString(), AssetID: req.AssetID,
		DocumentID: req.DocumentID, VersionID: wantID, WorkerID: "worker-1",
		LeaseGeneration: 1, PipelineGeneration: 2,
	})
	if err != nil || binding.Stage != string(PipelineStageConvert) {
		t.Fatalf("jobCandidateBindingParams = (%#v, %v)", binding, err)
	}
}

func TestPipelineStoreActivationLeavesCountCoherenceToAtomicSQL(t *testing.T) {
	req := ActivationRequest{
		IdentityID: uuid.NewString(), DocumentID: uuid.NewString(), VersionID: uuid.NewString(),
		AssetID: uuid.NewString(), JobID: uuid.NewString(), LockedBy: "worker-1",
		LeaseGeneration: 2, PipelineGeneration: 4,
		ExpectedPassageCount: 3, ExpectedEmbeddingCount: 2, ExpectedProjectionCount: 1,
	}
	params, err := activationParams(req)
	if err != nil {
		t.Fatalf("activationParams mismatch fixture: %v", err)
	}
	if params.ExpectedPassageCount != 3 || params.ExpectedEmbeddingCount != 2 || params.ExpectedProjectionCount != 1 {
		t.Fatalf("activation counts = %#v", params)
	}
	req.ExpectedProjectionCount = -1
	if _, err := activationParams(req); err == nil {
		t.Fatal("activationParams accepted a negative count")
	}
}

func pipelineStoreCandidateFixture(t *testing.T, count int) CandidateGeneration {
	t.Helper()
	identityID, documentID, versionID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	rawSHA := strings.Repeat("a", 64)
	candidate := CandidateGeneration{
		IdentityID: identityID, DocumentID: documentID, SearchDocumentID: "doc_pipeline",
		VersionID: versionID, VersionNumber: 2, RawSHA256: rawSHA, PipelineGeneration: 3,
		EmbeddingModel: "embeddinggemma", EmbeddingVersion: "revision-1",
		EmbeddingFingerprint: "embed-fingerprint", ProjectionFingerprint: "projection-fingerprint",
		ProjectionCount: count,
	}
	for i := range count {
		text := "raw passage " + string(rune('a'+i))
		normalizedSHA := NormalizedTextSHA256(text)
		passageID, err := DeterministicPassageID(versionID, "anchor:"+text, 0, normalizedSHA)
		if err != nil {
			t.Fatalf("DeterministicPassageID: %v", err)
		}
		page := i + 1
		candidate.Passages = append(candidate.Passages, Passage{
			ID: passageID, IdentityID: identityID, DocumentID: documentID,
			SearchDocumentID: "doc_pipeline", VersionID: versionID, VersionNumber: 2,
			OriginalSHA256: rawSHA, PipelineGeneration: 3, Ordinal: i,
			Text: text, ContextualizedText: "title: Fixture | text: " + text,
			NormalizedTextSHA256: normalizedSHA, Locator: PassageLocator{PageNumber: &page},
		})
		candidate.Embeddings = append(candidate.Embeddings, []float64{float64(i), 0.5, 1})
	}
	return candidate
}
