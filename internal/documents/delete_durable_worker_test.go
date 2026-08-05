package documents

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
)

var errDeleteTestNotFound = errors.New("delete test object not found")

func TestDurableDeleteWorkerVerifiesEverySurfaceBeforeFinalize(t *testing.T) {
	store := &deleteWorkerStore{}
	projector := &deleteWorkerProjector{present: map[string]bool{"1": true, "2": true}}
	objects := newDeleteWorkerObjects(t, "owner-bucket", map[string]string{
		"raw/original": "raw bytes", "derived/chunks": "derived bytes",
	})
	purger := &deleteWorkerPurger{}
	worker := newDeleteWorker(store, projector, objects, purger)

	result, err := worker.RunClaimed(context.Background(), claimedDeleteJob())
	if err != nil {
		t.Fatalf("RunClaimed: %v", err)
	}
	if result.Status != "succeeded" || result.ObjectsDeleted != 2 || result.Projections != 2 {
		t.Fatalf("result = %#v", result)
	}
	if !store.finalized || len(store.marked) != 2 {
		t.Fatalf("store finalized=%t marked=%v", store.finalized, store.marked)
	}
	if purger.identityID != deleteTestIdentityID || purger.documentID != deleteTestDocumentID {
		t.Fatalf("purge owner=%q document=%q", purger.identityID, purger.documentID)
	}
	for _, key := range []string{"raw/original", "derived/chunks"} {
		if _, err := objects.store.Head(context.Background(), objectstore.ObjectRef{
			Bucket: "owner-bucket", Key: key,
		}); !errors.Is(err, errDeleteTestNotFound) {
			t.Fatalf("object %q remains: %v", key, err)
		}
	}
}

func TestDurableDeleteWorkerRetriesProjectionFailureWithRedactedError(t *testing.T) {
	store := &deleteWorkerStore{}
	projector := &deleteWorkerProjector{
		present:      map[string]bool{"1": true},
		tombstoneErr: errors.New("private/customer-name.pdf"),
	}
	worker := newDeleteWorker(store, projector, nil, &deleteWorkerPurger{})
	job := claimedDeleteJob()
	job.Snapshot.Objects = nil
	job.Snapshot.ProjectionGenerations = []int64{1}

	result, err := worker.RunClaimed(context.Background(), job)
	if err == nil || strings.Contains(err.Error(), "customer-name") {
		t.Fatalf("error was not redacted: %v", err)
	}
	if result.Status != "queued" || store.retry.ErrorCode != "projection_tombstone_failed" {
		t.Fatalf("result=%#v retry=%#v", result, store.retry)
	}
	wantRetry := time.Date(2026, 8, 4, 12, 2, 0, 0, time.UTC)
	if !result.RetryAt.Equal(wantRetry) || !store.retry.NextAttemptAt.Equal(wantRetry) {
		t.Fatalf("retry at %s / %s, want %s", result.RetryAt, store.retry.NextAttemptAt, wantRetry)
	}
	if store.finalized {
		t.Fatal("projection failure finalized deletion")
	}
}

func TestDurableDeleteWorkerDoesNotFinalizeWhileProjectionRemains(t *testing.T) {
	store := &deleteWorkerStore{}
	projector := &deleteWorkerProjector{present: map[string]bool{"1": true}, sticky: true}
	worker := newDeleteWorker(store, projector, nil, &deleteWorkerPurger{})
	job := claimedDeleteJob()
	job.Snapshot.Objects = nil
	job.Snapshot.ProjectionGenerations = []int64{1}

	result, err := worker.RunClaimed(context.Background(), job)
	if err == nil || result.Status != "queued" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if store.retry.ErrorCode != "projection_still_present" || store.finalized {
		t.Fatalf("retry=%#v finalized=%t", store.retry, store.finalized)
	}
}

// Amendment #117: the extracted document text is the strongest PII the system holds.
// A delete that cannot prove those rows are gone must retry, never report success.
func TestDurableDeleteWorkerDoesNotFinalizeWhilePassagesRemain(t *testing.T) {
	store := &deleteWorkerStore{purgeRemaining: 3}
	worker := newDeleteWorker(store, &deleteWorkerProjector{}, nil, &deleteWorkerPurger{})
	job := claimedDeleteJob()
	job.Snapshot.Objects = nil
	job.Snapshot.ProjectionGenerations = nil

	result, err := worker.RunClaimed(context.Background(), job)
	if err == nil || result.Status != "queued" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if store.retry.ErrorCode != "passages_still_present" || store.finalized {
		t.Fatalf("retry=%#v finalized=%t", store.retry, store.finalized)
	}
}

// An unreadable purge is an unknown answer, not proof of erasure.
func TestDurableDeleteWorkerRetriesWhenPassagePurgeFails(t *testing.T) {
	store := &deleteWorkerStore{purgeErr: errors.New("connection reset during purge")}
	worker := newDeleteWorker(store, &deleteWorkerProjector{}, nil, &deleteWorkerPurger{})
	job := claimedDeleteJob()
	job.Snapshot.Objects = nil
	job.Snapshot.ProjectionGenerations = nil

	result, err := worker.RunClaimed(context.Background(), job)
	if err == nil || result.Status != "queued" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if store.retry.ErrorCode != "passage_purge_failed" || store.finalized {
		t.Fatalf("retry=%#v finalized=%t", store.retry, store.finalized)
	}
}

// Erasure must precede finalization, so a successful run always purges exactly once.
func TestDurableDeleteWorkerPurgesPassagesBeforeFinalize(t *testing.T) {
	store := &deleteWorkerStore{}
	worker := newDeleteWorker(store, &deleteWorkerProjector{}, nil, &deleteWorkerPurger{})
	job := claimedDeleteJob()
	job.Snapshot.Objects = nil
	job.Snapshot.ProjectionGenerations = nil

	result, err := worker.RunClaimed(context.Background(), job)
	if err != nil || result.Status != "succeeded" {
		t.Fatalf("result=%#v err=%v", result, err)
	}
	if store.purgeCalls != 1 || !store.finalized {
		t.Fatalf("purgeCalls=%d finalized=%t", store.purgeCalls, store.finalized)
	}
}

func TestDurableDeleteWorkerDeadLettersOwnerBucketMismatchBeforeDelete(t *testing.T) {
	store := &deleteWorkerStore{}
	objects := newDeleteWorkerObjects(t, "foreign-bucket", map[string]string{
		"raw/original": "raw bytes",
	})
	worker := newDeleteWorker(store, nil, objects, &deleteWorkerPurger{})
	job := claimedDeleteJob()
	job.Snapshot.ProjectionGenerations = nil
	job.Snapshot.Objects = job.Snapshot.Objects[:1]

	result, err := worker.RunClaimed(context.Background(), job)
	if err == nil || strings.Contains(err.Error(), "owner-bucket") || strings.Contains(err.Error(), "raw/original") {
		t.Fatalf("error was not redacted: %v", err)
	}
	if result.Status != "dead_letter" || store.deadLetter.ErrorCode != "object_bucket_mismatch" {
		t.Fatalf("result=%#v dead-letter=%#v", result, store.deadLetter)
	}
	if len(objects.store.deleted) != 0 || store.finalized {
		t.Fatalf("foreign bucket touched=%v finalized=%t", objects.store.deleted, store.finalized)
	}
}

func TestDurableDeleteWorkerTreatsAlreadyAbsentObjectAsSuccess(t *testing.T) {
	store := &deleteWorkerStore{}
	objects := newDeleteWorkerObjects(t, "owner-bucket", nil)
	worker := newDeleteWorker(store, nil, objects, &deleteWorkerPurger{})
	job := claimedDeleteJob()
	job.Snapshot.ProjectionGenerations = nil
	job.Snapshot.Objects = job.Snapshot.Objects[:1]

	result, err := worker.RunClaimed(context.Background(), job)
	if err != nil || result.Status != "succeeded" || len(store.marked) != 1 {
		t.Fatalf("result=%#v marked=%v err=%v", result, store.marked, err)
	}
}

func TestDurableDeleteWorkerRetriesWhileObjectHeadStillFindsBytes(t *testing.T) {
	store := &deleteWorkerStore{}
	objects := newDeleteWorkerObjects(t, "owner-bucket", map[string]string{
		"raw/original": "raw bytes",
	})
	objects.store.keepOnDelete = true
	worker := newDeleteWorker(store, nil, objects, &deleteWorkerPurger{})
	job := claimedDeleteJob()
	job.Snapshot.ProjectionGenerations = nil
	job.Snapshot.Objects = job.Snapshot.Objects[:1]

	result, err := worker.RunClaimed(context.Background(), job)
	if err == nil || result.Status != "queued" || store.retry.ErrorCode != "object_still_present" {
		t.Fatalf("result=%#v retry=%#v err=%v", result, store.retry, err)
	}
	if len(store.marked) != 0 || store.finalized {
		t.Fatalf("unverified object marked=%v finalized=%t", store.marked, store.finalized)
	}
}

func TestDurableDeleteWorkerRetriesSandboxPurgeBeforeObjectAccess(t *testing.T) {
	store := &deleteWorkerStore{}
	objects := newDeleteWorkerObjects(t, "owner-bucket", map[string]string{
		"raw/original": "raw bytes",
	})
	purger := &deleteWorkerPurger{err: errors.New("sandbox unavailable")}
	worker := newDeleteWorker(store, nil, objects, purger)
	job := claimedDeleteJob()
	job.Snapshot.ProjectionGenerations = nil
	job.Snapshot.Objects = job.Snapshot.Objects[:1]

	result, err := worker.RunClaimed(context.Background(), job)
	if err == nil || result.Status != "queued" || store.retry.ErrorCode != "sandbox_purge_failed" {
		t.Fatalf("result=%#v retry=%#v err=%v", result, store.retry, err)
	}
	if len(objects.store.deleted) != 0 || store.finalized {
		t.Fatalf("object accessed before sandbox verification: %v", objects.store.deleted)
	}
}

func TestDurableDeleteWorkerDeadLettersCorruptSnapshot(t *testing.T) {
	store := &deleteWorkerStore{}
	worker := newDeleteWorker(store, nil, nil, &deleteWorkerPurger{})
	job := claimedDeleteJob()
	job.Snapshot.DocumentID = "10000000-0000-0000-0000-000000000099"

	result, err := worker.RunClaimed(context.Background(), job)
	if err == nil || result.Status != "dead_letter" || store.deadLetter.ErrorCode != "snapshot_invalid" {
		t.Fatalf("result=%#v dead-letter=%#v err=%v", result, store.deadLetter, err)
	}
}

func TestDurableDeleteWorkerStopsOnLostFence(t *testing.T) {
	store := &deleteWorkerStore{heartbeatErr: ErrDeleteJobLeaseLost}
	worker := newDeleteWorker(store, nil, nil, &deleteWorkerPurger{})
	job := claimedDeleteJob()
	job.Snapshot.Objects = nil
	job.Snapshot.ProjectionGenerations = nil

	_, err := worker.RunClaimed(context.Background(), job)
	if !errors.Is(err, ErrDeleteJobLeaseLost) {
		t.Fatalf("error = %v", err)
	}
	if store.finalized || store.retry.ErrorCode != "" || store.deadLetter.ErrorCode != "" {
		t.Fatalf("stale worker mutated terminal state: %#v", store)
	}
}

func TestDurableDeleteWorkerProcessOwnerClaimsAndRunsBatch(t *testing.T) {
	job := claimedDeleteJob()
	job.Snapshot.Objects = nil
	job.Snapshot.ProjectionGenerations = nil
	store := &deleteWorkerStore{jobs: []DeleteJob{job}}
	worker := newDeleteWorker(store, nil, nil, &deleteWorkerPurger{})

	results, err := worker.ProcessOwner(context.Background(), ClaimDeleteJobsRequest{
		IdentityID: deleteTestIdentityID, WorkerID: "delete-worker",
		LeaseDuration: time.Minute, BatchSize: 1,
	})
	if err != nil || len(results) != 1 || results[0].Status != "succeeded" {
		t.Fatalf("ProcessOwner = (%#v, %v)", results, err)
	}
}

func TestParseDeleteSnapshotRejectsCorruptOrForeignSteps(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{name: "foreign document", raw: `{"document_id":"10000000-0000-0000-0000-000000000099","pipeline_generation":1,"objects":[],"projection_generations":[]}`},
		{name: "duplicate generation", raw: `{"document_id":"10000000-0000-0000-0000-000000000001","pipeline_generation":1,"objects":[],"projection_generations":[1,1]}`},
		{name: "unknown field", raw: `{"document_id":"10000000-0000-0000-0000-000000000001","pipeline_generation":1,"objects":[],"projection_generations":[],"path":"private"}`},
		{name: "trailing data", raw: `{"document_id":"10000000-0000-0000-0000-000000000001","pipeline_generation":1,"objects":[],"projection_generations":[]} {}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := parseDeleteSnapshot([]byte(test.raw), deleteTestDocumentID); err == nil {
				t.Fatal("corrupt snapshot accepted")
			}
		})
	}
}

func FuzzParseDeleteSnapshotNeverAcceptsForeignOwner(f *testing.F) {
	f.Add(deleteTestDocumentID)
	f.Add("10000000-0000-0000-0000-000000000099")
	f.Fuzz(func(t *testing.T, documentID string) {
		raw := []byte(`{"document_id":"` + documentID + `","pipeline_generation":1,"objects":[],"projection_generations":[]}`)
		_, err := parseDeleteSnapshot(raw, deleteTestDocumentID)
		if documentID != deleteTestDocumentID && err == nil {
			t.Fatal("foreign snapshot accepted")
		}
	})
}

func TestDefaultDeleteObjectNotFoundUsesTypedSignalsOnly(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "filesystem", err: errors.Join(errors.New("head"), errors.New("wrapped: "+errDeleteTestNotFound.Error())), want: false},
		{name: "os", err: errors.Join(errors.New("head"), os.ErrNotExist), want: true},
		{name: "http 404", err: deleteTestHTTPError{status: 404}, want: true},
		{name: "http 500", err: deleteTestHTTPError{status: 500}, want: false},
		{name: "s3 code", err: deleteTestCodeError{code: "NoSuchKey"}, want: true},
		{name: "similar text", err: errors.New("NoSuchKey"), want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := defaultDeleteObjectNotFound(test.err); got != test.want {
				t.Fatalf("defaultDeleteObjectNotFound(%v) = %t, want %t", test.err, got, test.want)
			}
		})
	}
}

const (
	deleteTestIdentityID = "00000000-0000-0000-0000-000000000001"
	deleteTestDocumentID = "10000000-0000-0000-0000-000000000001"
	deleteTestJobID      = "20000000-0000-0000-0000-000000000001"
)

func claimedDeleteJob() DeleteJob {
	return DeleteJob{
		ID: deleteTestJobID, IdentityID: deleteTestIdentityID,
		DocumentID: deleteTestDocumentID, Scope: "hard", Status: "running",
		AttemptCount: 2, MaxAttempts: 5, WorkerID: "delete-worker", LeaseGeneration: 3,
		DeleteGeneration: 2,
		Snapshot: DeleteSnapshot{
			DocumentID: deleteTestDocumentID, PipelineGeneration: 2,
			ProjectionGenerations: []int64{1, 2},
			Objects: []DeleteSnapshotObject{
				{ID: "30000000-0000-0000-0000-000000000001", Bucket: "owner-bucket", ObjectKey: "raw/original", DeletionGeneration: 1},
				{ID: "30000000-0000-0000-0000-000000000002", Bucket: "owner-bucket", ObjectKey: "derived/chunks", DeletionGeneration: 4},
			},
		},
	}
}

func newDeleteWorker(
	store *deleteWorkerStore,
	projector *deleteWorkerProjector,
	objects *deleteWorkerResolver,
	purger *deleteWorkerPurger,
) *DurableDeleteWorker {
	return &DurableDeleteWorker{
		Store: store, Projector: projector, Objects: objects, Staged: purger,
		Config: DurableDeleteWorkerConfig{
			LeaseDuration: 10 * time.Minute, RetryBase: time.Minute,
			Now:        func() time.Time { return time.Date(2026, 8, 4, 12, 0, 0, 0, time.UTC) },
			Jitter:     func(cap time.Duration) time.Duration { return cap },
			IsNotFound: func(err error) bool { return errors.Is(err, errDeleteTestNotFound) },
		},
	}
}

type deleteWorkerStore struct {
	heartbeatErr error
	jobs         []DeleteJob
	marked       []MarkDeletedObjectRequest
	retry        RetryDeleteJobRequest
	deadLetter   DeadLetterDeleteJobRequest
	finalized    bool
	// purgeRemaining defaults to 0: erasure proven. A positive value models passages
	// that survived the purge, which must block finalization.
	purgeRemaining int
	purgeErr       error
	purgeCalls     int
}

func (s *deleteWorkerStore) PurgePassages(context.Context, PurgeDocumentPassagesRequest) (int, error) {
	s.purgeCalls++
	return s.purgeRemaining, s.purgeErr
}

func (s *deleteWorkerStore) Claim(context.Context, ClaimDeleteJobsRequest) ([]DeleteJob, error) {
	return s.jobs, nil
}

func (s *deleteWorkerStore) Heartbeat(context.Context, HeartbeatDeleteJobRequest) error {
	return s.heartbeatErr
}

func (s *deleteWorkerStore) MarkObjectDeleted(_ context.Context, req MarkDeletedObjectRequest) error {
	s.marked = append(s.marked, req)
	return nil
}

func (s *deleteWorkerStore) Retry(_ context.Context, req RetryDeleteJobRequest) (DeleteJob, error) {
	s.retry = req
	return DeleteJob{Status: "queued"}, nil
}

func (s *deleteWorkerStore) DeadLetter(_ context.Context, req DeadLetterDeleteJobRequest) (DeleteJob, error) {
	s.deadLetter = req
	return DeleteJob{Status: "dead_letter"}, nil
}

func (s *deleteWorkerStore) Finalize(_ context.Context, req FinalizeDeleteJobRequest) (Document, error) {
	if !req.ProjectionVerified {
		return Document{}, errors.New("delete finalization preconditions missing")
	}
	s.finalized = true
	return Document{ID: deleteTestDocumentID, Status: DocumentStatusDeleted}, nil
}

type deleteWorkerProjector struct {
	present      map[string]bool
	tombstoneErr error
	sticky       bool
}

func (p *deleteWorkerProjector) TombstoneGeneration(
	context.Context, string, string, string,
) (DeleteProjectionResult, error) {
	return DeleteProjectionResult{}, p.tombstoneErr
}

func (p *deleteWorkerProjector) DeleteGeneration(
	_ context.Context,
	_, _, generation string,
) (DeleteProjectionResult, error) {
	found := p.present[generation]
	if found && !p.sticky {
		delete(p.present, generation)
	}
	return DeleteProjectionResult{Found: found}, nil
}

type deleteWorkerPurger struct {
	identityID string
	documentID string
	err        error
}

func (p *deleteWorkerPurger) PurgeStagedDocument(ctx context.Context, documentID string) error {
	p.identityID = identityctx.IdentityID(ctx)
	p.documentID = documentID
	return p.err
}

type deleteWorkerResolver struct {
	store  *deleteWorkerObjectStore
	bucket string
}

func (r *deleteWorkerResolver) ResolveDeleteObjectStore(
	context.Context,
	string,
) (ResolvedDeleteObjectStore, error) {
	return ResolvedDeleteObjectStore{Store: r.store, Bucket: r.bucket}, nil
}

func newDeleteWorkerObjects(t *testing.T, bucket string, values map[string]string) *deleteWorkerResolver {
	t.Helper()
	store := &deleteWorkerObjectStore{FakeStore: objectstore.NewFake()}
	for key, value := range values {
		_, err := store.Put(context.Background(), objectstore.ObjectRef{Bucket: bucket, Key: key},
			bytes.NewBufferString(value), objectstore.PutOptions{Size: int64(len(value))})
		if err != nil {
			t.Fatalf("seed object: %v", err)
		}
	}
	return &deleteWorkerResolver{store: store, bucket: bucket}
}

type deleteWorkerObjectStore struct {
	*objectstore.FakeStore
	deleted      []objectstore.ObjectRef
	keepOnDelete bool
}

func (s *deleteWorkerObjectStore) Head(ctx context.Context, ref objectstore.ObjectRef) (objectstore.Attrs, error) {
	attrs, err := s.FakeStore.Head(ctx, ref)
	if err != nil {
		return objectstore.Attrs{}, errDeleteTestNotFound
	}
	return attrs, nil
}

func (s *deleteWorkerObjectStore) Delete(ctx context.Context, ref objectstore.ObjectRef) error {
	if _, err := s.FakeStore.Head(ctx, ref); err != nil {
		return errDeleteTestNotFound
	}
	s.deleted = append(s.deleted, ref)
	if s.keepOnDelete {
		return nil
	}
	return s.FakeStore.Delete(ctx, ref)
}

var _ objectstore.Store = (*deleteWorkerObjectStore)(nil)

type deleteTestHTTPError struct{ status int }

func (e deleteTestHTTPError) Error() string       { return "typed HTTP error" }
func (e deleteTestHTTPError) HTTPStatusCode() int { return e.status }

type deleteTestCodeError struct{ code string }

func (e deleteTestCodeError) Error() string     { return "typed API error" }
func (e deleteTestCodeError) ErrorCode() string { return e.code }
