package documents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

func TestPipelineWorkerActivatesOnlyAfterVerifiedProjection(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "synthetic pipeline payload")
	persistence := newPipelinePersistenceFake()
	objects := &pipelineObjectStore{objects: map[objectstore.ObjectRef][]byte{}}
	worker := pipelineWorkerFixture(persistence)

	result, err := worker.Run(t.Context(), pipelineRunFixture(path, objects))
	if err != nil {
		t.Fatal(err)
	}
	if !persistence.activated || persistence.saved.ProjectionCount != 1 {
		t.Fatalf("activation = %t, candidate = %#v", persistence.activated, persistence.saved)
	}
	if result.PassageCount != 1 || result.VersionNumber != 1 {
		t.Fatalf("result = %#v", result)
	}
	if len(objects.objects) != 4 {
		t.Fatalf("derived objects = %d, want convert/chunk/embed/project", len(objects.objects))
	}
	for ref := range objects.objects {
		if strings.Contains(ref.Key, "manual.pdf") || strings.Contains(ref.Key, "synthetic") {
			t.Fatalf("artifact key leaks content: %q", ref.Key)
		}
	}
}

func TestPipelineWorkerReplaysVerifiedImmutableArtifacts(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "synthetic pipeline payload")
	persistence := newPipelinePersistenceFake()
	objects := &pipelineObjectStore{objects: map[objectstore.ObjectRef][]byte{}}
	worker := pipelineWorkerFixture(persistence)
	req := pipelineRunFixture(path, objects)

	if _, err := worker.Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	if _, err := worker.Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	converter := worker.Converter.(*pipelineConverterFake)
	embedder := worker.Embedder.(*pipelineEmbedderFake)
	projector := worker.Projector.(*pipelineProjectorFake)
	if converter.calls != 1 || embedder.calls != 1 || projector.calls != 1 {
		t.Fatalf("model calls after replay = convert:%d embed:%d project:%d, want one each",
			converter.calls, embedder.calls, projector.calls)
	}
}

func TestPipelineWorkerRejectsCorruptCachedArtifact(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "synthetic pipeline payload")
	persistence := newPipelinePersistenceFake()
	objects := &pipelineObjectStore{objects: map[objectstore.ObjectRef][]byte{}}
	worker := pipelineWorkerFixture(persistence)
	req := pipelineRunFixture(path, objects)
	if _, err := worker.Run(t.Context(), req); err != nil {
		t.Fatal(err)
	}
	for ref, body := range objects.objects {
		if strings.Contains(ref.Key, "/chunks/") {
			body[0] ^= 1
			objects.objects[ref] = body
		}
	}
	if _, err := worker.Run(t.Context(), req); err == nil ||
		!strings.Contains(err.Error(), "digest does not match") {
		t.Fatalf("error = %v, want cached digest rejection", err)
	}
}

func TestPipelineWorkerProjectionFailureNeverPublishes(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "synthetic pipeline payload")
	persistence := newPipelinePersistenceFake()
	objects := &pipelineObjectStore{objects: map[objectstore.ObjectRef][]byte{}}
	worker := pipelineWorkerFixture(persistence)
	projector := worker.Projector.(*pipelineProjectorFake)
	projector.err = errors.New("arcadedb unavailable")

	if _, err := worker.Run(t.Context(), pipelineRunFixture(path, objects)); !errors.Is(err, projector.err) {
		t.Fatalf("error = %v", err)
	}
	if persistence.activated {
		t.Fatal("projection failure published the candidate")
	}
	if len(persistence.failures) == 0 || persistence.failures[len(persistence.failures)-1].StageID == "" {
		t.Fatalf("stage failure was not durably fenced: %#v", persistence.failures)
	}
}

func TestPipelineWorkerDiscardsProjectionWhenCandidateFenceRejects(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "synthetic pipeline payload")
	persistence := newPipelinePersistenceFake()
	persistence.saveErr = ErrPipelineCandidateRejected
	objects := &pipelineObjectStore{objects: map[objectstore.ObjectRef][]byte{}}
	worker := pipelineWorkerFixture(persistence)

	if _, err := worker.Run(t.Context(), pipelineRunFixture(path, objects)); !errors.Is(err, ErrPipelineCandidateRejected) {
		t.Fatalf("error = %v, want candidate rejection", err)
	}
	projector := worker.Projector.(*pipelineProjectorFake)
	if projector.discardCalls != 1 || persistence.activated {
		t.Fatalf("discard calls = %d, activated = %t", projector.discardCalls, persistence.activated)
	}
}

// A generation this attempt did not create belongs to an earlier, possibly activated
// attempt. Compensating would physically delete passages a live document still cites.
func TestPipelineWorkerLeavesUnownedProjectionForReconciliation(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "synthetic pipeline payload")
	persistence := newPipelinePersistenceFake()
	persistence.saveErr = ErrPipelineCandidateRejected
	objects := &pipelineObjectStore{objects: map[objectstore.ObjectRef][]byte{}}
	worker := pipelineWorkerFixture(persistence)
	worker.Projector.(*pipelineProjectorFake).created = false

	_, err := worker.Run(t.Context(), pipelineRunFixture(path, objects))
	if !errors.Is(err, ErrPipelineCandidateRejected) {
		t.Fatalf("error = %v, want candidate rejection", err)
	}
	if !errors.Is(err, ErrPipelineProjectionUnreconciled) {
		t.Fatalf("error = %v, want the reconciliation sentinel joined in", err)
	}
	projector := worker.Projector.(*pipelineProjectorFake)
	if projector.discardCalls != 0 {
		t.Fatalf("discard calls = %d, want 0 for a generation this attempt did not create", projector.discardCalls)
	}
	if persistence.activated {
		t.Fatal("candidate must not activate after a save failure")
	}
}

func TestPipelineWorkerRemovesArtifactRejectedByLedger(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "synthetic pipeline payload")
	persistence := newPipelinePersistenceFake()
	persistence.recordErr = ErrPipelineCandidateRejected
	objects := &pipelineObjectStore{objects: map[objectstore.ObjectRef][]byte{}}
	worker := pipelineWorkerFixture(persistence)

	if _, err := worker.Run(t.Context(), pipelineRunFixture(path, objects)); !errors.Is(err, ErrPipelineCandidateRejected) {
		t.Fatalf("error = %v, want artifact ledger rejection", err)
	}
	if len(objects.objects) != 0 {
		t.Fatalf("untracked artifacts = %d, want none", len(objects.objects))
	}
}

// A returned ledger error does not prove the insert failed. When PostgreSQL still
// reports an owner, the object belongs to somebody and must survive the cleanup.
func TestPipelineWorkerKeepsArtifactStillOwnedByLedger(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "synthetic pipeline payload")
	persistence := newPipelinePersistenceFake()
	persistence.recordErr = ErrPipelineCandidateRejected
	persistence.ownerCount = 1
	objects := &pipelineObjectStore{objects: map[objectstore.ObjectRef][]byte{}}
	worker := pipelineWorkerFixture(persistence)

	_, err := worker.Run(t.Context(), pipelineRunFixture(path, objects))
	if !errors.Is(err, ErrPipelineArtifactOwnershipUnproven) {
		t.Fatalf("error = %v, want the ownership-unproven sentinel", err)
	}
	if len(objects.objects) != 1 {
		t.Fatalf("artifacts = %d, want the owned artifact left intact", len(objects.objects))
	}
}

// An unreadable ledger is an unknown answer, not a proof of absence.
func TestPipelineWorkerKeepsArtifactWhenLedgerUnreadable(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "synthetic pipeline payload")
	persistence := newPipelinePersistenceFake()
	persistence.recordErr = ErrPipelineCandidateRejected
	persistence.ownerErr = errors.New("connection reset while counting owners")
	objects := &pipelineObjectStore{objects: map[objectstore.ObjectRef][]byte{}}
	worker := pipelineWorkerFixture(persistence)

	_, err := worker.Run(t.Context(), pipelineRunFixture(path, objects))
	if !errors.Is(err, ErrPipelineArtifactOwnershipUnproven) {
		t.Fatalf("error = %v, want the ownership-unproven sentinel", err)
	}
	if len(objects.objects) != 1 {
		t.Fatalf("artifacts = %d, want the artifact left for reconciliation", len(objects.objects))
	}
}

func TestPipelineWorkerDoesNotTreatArtifactHeadFailureAsAbsence(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "synthetic pipeline payload")
	persistence := newPipelinePersistenceFake()
	persistence.recordErr = ErrPipelineCandidateRejected
	objects := &pipelineObjectStore{
		objects: map[objectstore.ObjectRef][]byte{}, headErr: errors.New("object store unauthorized"),
	}
	worker := pipelineWorkerFixture(persistence)

	if _, err := worker.Run(t.Context(), pipelineRunFixture(path, objects)); err == nil ||
		!strings.Contains(err.Error(), "verify untracked artifact absence") {
		t.Fatalf("error = %v, want HEAD verification failure", err)
	}
}

func TestPipelineWorkerRejectsPartialDoclingResult(t *testing.T) {
	path := writeNamedTempFile(t, "manual.pdf", "synthetic pipeline payload")
	persistence := newPipelinePersistenceFake()
	objects := &pipelineObjectStore{objects: map[objectstore.ObjectRef][]byte{}}
	worker := pipelineWorkerFixture(persistence)
	converter := worker.Converter.(*pipelineConverterFake)
	converter.result.Partial = true

	if _, err := worker.Run(t.Context(), pipelineRunFixture(path, objects)); err == nil ||
		!strings.Contains(err.Error(), "complete conversion") {
		t.Fatalf("error = %v, want partial refusal", err)
	}
	if persistence.activated || persistence.saved.VersionID != "" {
		t.Fatalf("partial conversion escaped candidate fence: %#v", persistence.saved)
	}
}

func pipelineWorkerFixture(persistence *pipelinePersistenceFake) *PipelineWorker {
	raw := "exact canary answer"
	return &PipelineWorker{
		Converter: &pipelineConverterFake{result: DoclingResult{
			Chunks: []DoclingChunk{{
				Filename: "manual.pdf", ChunkIndex: 0, Text: raw,
				RawText: &raw, DocItems: []string{"#/texts/0"}, PageNumbers: []int{1},
			}},
			DocumentJSON:   []byte(`{"schema_name":"DoclingDocument","texts":[{"self_ref":"#/texts/0","prov":[{"page_no":1,"bbox":{"l":0,"t":0,"r":1,"b":1},"charspan":[0,19]}]}]}`),
			DocumentStatus: "success",
		}},
		// created: the default fixture models a first, uncached attempt, which is the
		// only case allowed to compensate by destroying the generation.
		Embedder: &pipelineEmbedderFake{}, Projector: &pipelineProjectorFake{created: true},
		Persistence: persistence,
		Config: PipelineWorkerConfig{
			ProducerVersion: "docling-v1.29.0", EmbeddingModel: "embeddinggemma",
			ConversionProfile: DoclingConversionProfile(false),
			ChunkTokenizer:    "embeddinggemma-revision-1", ChunkMaxTokens: 512,
			EmbeddingVersion: "revision-1", EmbeddingFingerprint: strings.Repeat("e", 64),
			EmbeddingDimensions: 2, EmbeddingNormalization: EmbeddingNormalizationMRL,
			ProjectionSchema: "arcadedb-document-projection-v1",
			MaxPassages:      100, MaxAttempts: 5, LeaseDuration: 20 * time.Minute,
		},
		Clock:       func() time.Time { return time.Date(2026, 8, 4, 8, 0, 0, 0, time.UTC) },
		RetryJitter: func(cap time.Duration) time.Duration { return cap / 2 },
	}
}

func pipelineRunFixture(path string, objects objectstore.Store) PipelineRunRequest {
	return PipelineRunRequest{
		Record: DocumentVersionRecord{
			Document: Document{
				ID: "10000000-0000-0000-0000-000000000001", IdentityID: "00000000-0000-0000-0000-000000000001",
				SearchDocumentID: "doc_canary", Status: DocumentStatusStored,
			},
			Version: DocumentVersion{
				ID: "20000000-0000-0000-0000-000000000001", IdentityID: "00000000-0000-0000-0000-000000000001",
				DocumentID: "10000000-0000-0000-0000-000000000001", SearchDocumentID: "doc_canary",
				VersionNumber: 1, PipelineGeneration: 1, SHA256: strings.Repeat("a", 64),
				ContentType: "application/pdf", SizeBytes: int64(len("synthetic pipeline payload")),
			},
		},
		AssetID: "30000000-0000-0000-0000-000000000001",
		JobID:   "40000000-0000-0000-0000-000000000001", WorkerID: "worker-1",
		LeaseGeneration: 1, Title: "Canary manual", FileName: "manual.pdf",
		MIMEType: "application/pdf", Path: path, ObjectBucket: "e2e-owner-bucket", Objects: objects,
	}
}

type pipelineConverterFake struct {
	result DoclingResult
	err    error
	calls  int
}

func (f *pipelineConverterFake) ChunkFile(context.Context, DoclingFile) (DoclingResult, error) {
	f.calls++
	return f.result, f.err
}

type pipelineEmbedderFake struct {
	err   error
	calls int
}

func (f *pipelineEmbedderFake) Embed(_ context.Context, texts []string) ([][]float64, error) {
	f.calls++
	if f.err != nil {
		return nil, f.err
	}
	out := make([][]float64, len(texts))
	for index := range texts {
		out[index] = []float64{0.25, 0.75}
	}
	return out, nil
}

// created mirrors the real projector's ownership signal; the zero value means "this
// attempt did not create the generation", which is what the replay paths assert.
type pipelineProjectorFake struct {
	created bool

	err          error
	discardErr   error
	calls        int
	discardCalls int
}

func (f *pipelineProjectorFake) DiscardCandidate(context.Context, CandidateGeneration) error {
	f.discardCalls++
	return f.discardErr
}

func (f *pipelineProjectorFake) ProjectCandidate(_ context.Context, candidate CandidateGeneration) (ProjectionCommit, error) {
	f.calls++
	if f.err != nil {
		return ProjectionCommit{}, f.err
	}
	return ProjectionCommit{
		Fingerprint:  strings.Repeat("p", 64),
		PassageCount: len(candidate.Passages),
		Created:      f.created,
	}, nil
}

type pipelinePersistenceFake struct {
	nextStage int
	stages    map[string]PipelineStageRecord
	saved     CandidateGeneration
	activated bool
	failures  []StageFailure
	recordErr error
	saveErr   error
	// ownerCount defaults to 0: PostgreSQL positively reports nobody owns the key, so
	// cleanup is permitted. ownerErr models an unreadable ledger, which is not proof.
	ownerCount int
	ownerErr   error
}

func newPipelinePersistenceFake() *pipelinePersistenceFake {
	return &pipelinePersistenceFake{stages: map[string]PipelineStageRecord{}}
}

func (f *pipelinePersistenceFake) CountArtifactLedgerOwners(context.Context, ArtifactLedgerQuery) (int, error) {
	return f.ownerCount, f.ownerErr
}

func (f *pipelinePersistenceFake) ReserveCandidateVersion(context.Context, CandidateVersionRequest) (CandidateVersion, error) {
	return CandidateVersion{}, nil
}

func (f *pipelinePersistenceFake) BindJobCandidate(context.Context, JobCandidateBinding) error {
	return nil
}

func (f *pipelinePersistenceFake) RecordDerivedArtifact(_ context.Context, request DerivedArtifactRequest) (string, error) {
	if f.recordErr != nil {
		return "", f.recordErr
	}
	return "50000000-0000-0000-0000-" + request.SHA256[:12], nil
}

func (f *pipelinePersistenceFake) SaveCandidate(_ context.Context, candidate CandidateGeneration) error {
	if f.saveErr != nil {
		return f.saveErr
	}
	f.saved = candidate
	return nil
}

func (f *pipelinePersistenceFake) UpsertStage(_ context.Context, request StageUpsert) (PipelineStageRecord, error) {
	f.nextStage++
	id := "stage-" + string(request.Stage)
	if existing, ok := f.stages[id]; ok {
		return existing, nil
	}
	record := PipelineStageRecord{
		ID: id, IdentityID: request.IdentityID, Stage: request.Stage,
		Status: "pending", PipelineGeneration: request.PipelineGeneration,
	}
	f.stages[id] = record
	return record, nil
}

func (f *pipelinePersistenceFake) ClaimStage(_ context.Context, request StageClaim) (PipelineStageRecord, error) {
	record := f.stages[request.StageID]
	record.Status = "running"
	record.AttemptCount++
	record.LeaseGeneration++
	f.stages[request.StageID] = record
	return record, nil
}

func (f *pipelinePersistenceFake) HeartbeatStage(context.Context, StageFence) error {
	return nil
}

func (f *pipelinePersistenceFake) CompleteStage(_ context.Context, completion StageCompletion) error {
	record := f.stages[completion.StageID]
	record.Status = "succeeded"
	record.ArtifactStorageObjectID = completion.ArtifactStorageObjectID
	record.ArtifactObjectKey = completion.ArtifactObjectKey
	record.ArtifactSHA256 = completion.ArtifactSHA256
	record.ArtifactSizeBytes = completion.ArtifactSizeBytes
	f.stages[completion.StageID] = record
	return nil
}

func (f *pipelinePersistenceFake) FailStage(_ context.Context, failure StageFailure) error {
	f.failures = append(f.failures, failure)
	return nil
}

func (f *pipelinePersistenceFake) ActivateCandidate(context.Context, ActivationRequest) error {
	f.activated = true
	return nil
}

type pipelineObjectStore struct {
	objects map[objectstore.ObjectRef][]byte
	headErr error
}

func (s *pipelineObjectStore) Put(_ context.Context, ref objectstore.ObjectRef, reader io.Reader, _ objectstore.PutOptions) (objectstore.Attrs, error) {
	body, err := io.ReadAll(reader)
	if err != nil {
		return objectstore.Attrs{}, err
	}
	s.objects[ref] = bytes.Clone(body)
	return objectstore.Attrs{SizeBytes: int64(len(body)), MIMEType: "application/json"}, nil
}

func (*pipelineObjectStore) PresignPut(context.Context, objectstore.PresignPutRequest) (objectstore.PresignedPut, error) {
	return objectstore.PresignedPut{}, errors.New("not implemented")
}
func (s *pipelineObjectStore) Head(_ context.Context, ref objectstore.ObjectRef) (objectstore.Attrs, error) {
	if s.headErr != nil {
		return objectstore.Attrs{}, s.headErr
	}
	body, ok := s.objects[ref]
	if !ok {
		return objectstore.Attrs{}, fmt.Errorf("object not found: %w", fs.ErrNotExist)
	}
	return objectstore.Attrs{SizeBytes: int64(len(body)), MIMEType: "application/json"}, nil
}
func (s *pipelineObjectStore) Get(_ context.Context, ref objectstore.ObjectRef) (io.ReadCloser, objectstore.Attrs, error) {
	body, ok := s.objects[ref]
	if !ok {
		return nil, objectstore.Attrs{}, errors.New("object not found")
	}
	return io.NopCloser(bytes.NewReader(body)), objectstore.Attrs{
		SizeBytes: int64(len(body)), MIMEType: "application/json",
	}, nil
}
func (*pipelineObjectStore) List(context.Context, objectstore.ListRequest) ([]objectstore.ObjectInfo, error) {
	return nil, errors.New("not implemented")
}
func (s *pipelineObjectStore) Delete(_ context.Context, ref objectstore.ObjectRef) error {
	delete(s.objects, ref)
	return nil
}
