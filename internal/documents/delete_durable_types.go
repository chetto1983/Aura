package documents

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/objectstore"
)

// ErrDeleteJobLeaseLost means the fence no longer matches: the row moved on without
// this worker. Every store call maps pgx.ErrNoRows to it, and the worker abandons the
// job instead of settling it — retrying or dead-lettering here would stamp a stale
// verdict onto whoever now holds the lease.
var ErrDeleteJobLeaseLost = errors.New("document delete job lease lost")

// DeleteJob is one claimed unit of erasure. It carries the plan captured when the
// delete was enqueued rather than a live view of the document, so writes racing the
// teardown can neither widen nor shrink the blast radius. snapshotJSON keeps the raw
// column bytes so validation runs against what PostgreSQL stored, not a re-marshalled
// copy of an already-decoded struct.
type DeleteJob struct {
	ID               string
	IdentityID       string
	DocumentID       string
	Scope            string
	Status           string
	Snapshot         DeleteSnapshot
	AttemptCount     int
	MaxAttempts      int
	WorkerID         string
	LeaseGeneration  int64
	DeleteGeneration int64
	LockedUntil      time.Time
	NextAttemptAt    time.Time
	snapshotJSON     []byte
}

// DeleteSnapshot is the erasure plan a DeleteJob carries: the exact blobs and projection
// generations that existed at enqueue time. It is decoded with DisallowUnknownFields and
// bounds-checked because it arrives from a database column — a plan that cannot be
// trusted has to fail the job rather than quietly under-delete.
type DeleteSnapshot struct {
	DocumentID            string                 `json:"document_id"`
	PipelineGeneration    int64                  `json:"pipeline_generation"`
	Objects               []DeleteSnapshotObject `json:"objects"`
	ProjectionGenerations []int64                `json:"projection_generations"`
}

// DeleteSnapshotObject names one blob to erase together with the ledger generation that
// marked it delete_pending. The generation travels with the object so the verification
// write can settle only THAT round of deletion: a later re-upload of the same key
// carries a newer generation and is left alone.
type DeleteSnapshotObject struct {
	ID                 string `json:"id"`
	Bucket             string `json:"bucket"`
	ObjectKey          string `json:"object_key"`
	DeletionGeneration int64  `json:"deletion_generation"`
}

// ClaimDeleteJobsRequest asks for up to BatchSize due jobs of one identity, leased to
// WorkerID. IdentityID is not a filter applied after the fact: the claim runs inside
// that identity's RLS transaction, so a worker cannot lift another tenant's job off the
// queue even if it asks for one.
type ClaimDeleteJobsRequest struct {
	IdentityID    string
	WorkerID      string
	LeaseDuration time.Duration
	BatchSize     int
}

// DeleteJobFence is the four-part credential every mutating call must present: owner,
// job, holder, lease generation. The SQL matches on all four plus an unexpired lease, so
// a worker that resumes after a stall writes nothing instead of stamping its stale
// verdict on the job a successor has already re-claimed.
type DeleteJobFence struct {
	IdentityID      string
	JobID           string
	WorkerID        string
	LeaseGeneration int64
}

// HeartbeatDeleteJobRequest extends the lease mid-teardown. The worker sends one before
// every irreversible step so a slow erasure is never re-claimed underneath it while
// blobs are still disappearing.
type HeartbeatDeleteJobRequest struct {
	DeleteJobFence
	LeaseDuration time.Duration
}

// MarkDeletedObjectRequest records the ledger row for a blob just proven gone.
// DeletionGeneration must equal the one the snapshot planned, so the write cannot settle
// a deletion round it did not perform.
type MarkDeletedObjectRequest struct {
	DeleteJobFence
	StorageObjectID    string
	DeletionGeneration int64
}

// PurgeDocumentPassagesRequest erases one document's body under an owned job fence.
type PurgeDocumentPassagesRequest struct {
	DeleteJobFence
	DocumentID string
}

// RetryDeleteJobRequest returns a transiently failed job to the queue at NextAttemptAt.
// ErrorCode is a closed vocabulary and the persisted message is fixed, so a failure
// raised deep inside a tenant's storage backend cannot leak its text into a row.
type RetryDeleteJobRequest struct {
	DeleteJobFence
	ErrorCode     string
	NextAttemptAt time.Time
}

// DeadLetterDeleteJobRequest parks a job that can never succeed. It is separate from
// Retry because such a job must stop burning attempts and become visible to an operator
// instead of rescheduling itself forever.
type DeadLetterDeleteJobRequest struct {
	DeleteJobFence
	ErrorCode string
}

// FinalizeDeleteJobRequest closes the job. ProjectionVerified is passed down to SQL
// rather than assumed: the statement matches on it and on the absence of every live
// storage object, chunk, and embedding, so the document cannot reach deleted while
// anything it owned survives.
type FinalizeDeleteJobRequest struct {
	DeleteJobFence
	ProjectionVerified bool
}

// DurableDeleteStore is the persistence half of the teardown, split from the worker so
// the ordering rules can be exercised without PostgreSQL. Every method past Claim takes
// a DeleteJobFence and reports ErrDeleteJobLeaseLost once it stops holding.
type DurableDeleteStore interface {
	Claim(context.Context, ClaimDeleteJobsRequest) ([]DeleteJob, error)
	Heartbeat(context.Context, HeartbeatDeleteJobRequest) error
	MarkObjectDeleted(context.Context, MarkDeletedObjectRequest) error
	// PurgePassages returns the number of live passage rows REMAINING afterwards; a
	// non-zero result means erasure could not be proven and must not be finalized.
	PurgePassages(context.Context, PurgeDocumentPassagesRequest) (int, error)
	Retry(context.Context, RetryDeleteJobRequest) (DeleteJob, error)
	DeadLetter(context.Context, DeadLetterDeleteJobRequest) (DeleteJob, error)
	Finalize(context.Context, FinalizeDeleteJobRequest) (Document, error)
}

// DeleteProjectionResult reports whether the generation was still there. Found is the
// worker's only evidence about the projection store, which is why it reads it twice:
// once for the delete and once for a re-check that has to come back false.
type DeleteProjectionResult struct {
	Found bool
}

// DurableDeleteProjector removes one exact immutable generation and reports whether
// it existed. Calling DeleteGeneration again must return Found=false after success.
type DurableDeleteProjector interface {
	TombstoneGeneration(context.Context, string, string, string) (DeleteProjectionResult, error)
	DeleteGeneration(context.Context, string, string, string) (DeleteProjectionResult, error)
}

// ResolvedDeleteObjectStore binds a store to the one bucket its identity may touch. The
// bucket travels with the store because the worker rejects any snapshot object naming a
// different one: a mismatch means the plan outlived the tenancy that produced it.
type ResolvedDeleteObjectStore struct {
	Store  objectstore.Store
	Bucket string
}

// DurableDeleteObjectResolver hands back an identity's object store at claim time.
// Resolution is deferred rather than fixed at construction because per-identity buckets
// are provisioned while the worker is already running.
type DurableDeleteObjectResolver interface {
	ResolveDeleteObjectStore(context.Context, string) (ResolvedDeleteObjectStore, error)
}

// DurableDeleteWorkerConfig carries the timing knobs plus the three seams a test needs
// to drive the worker deterministically: Jitter, Now, and IsNotFound. Every field is
// optional — config fills production defaults — so a test overrides only what it steers.
type DurableDeleteWorkerConfig struct {
	LeaseDuration time.Duration
	RetryBase     time.Duration
	Jitter        func(time.Duration) time.Duration
	Now           func() time.Time
	IsNotFound    func(error) bool
}

// DurableDeleteWorker drives a claimed job to proven erasure. Each collaborator is an
// interface so this package stays ignorant of ArcadeDB, object storage, and containers;
// a nil one is a permanent failure rather than a skipped step, because "not configured"
// must never be read as "nothing left to delete".
type DurableDeleteWorker struct {
	Store     DurableDeleteStore
	Projector DurableDeleteProjector
	Objects   DurableDeleteObjectResolver
	Staged    StagedDocumentPurger
	Config    DurableDeleteWorkerConfig
}

// DeleteRunResult is what one attempt did, not whether it succeeded — the returned error
// carries that. The counters are filled in even on a failed attempt so an operator can
// see how far a retried teardown got before it stopped. RetryAt is set only when the job
// actually went back to queued, since an exhausted job dead-letters instead.
type DeleteRunResult struct {
	JobID          string
	Status         string
	ObjectsDeleted int
	Projections    int
	RetryAt        time.Time
}

type deleteStepError struct {
	code      string
	permanent bool
}

func (e deleteStepError) Error() string {
	return "document delete " + e.code
}

func permanentDelete(code string) error {
	return deleteStepError{code: strings.TrimSpace(code), permanent: true}
}

func transientDelete(code string) error {
	return deleteStepError{code: strings.TrimSpace(code)}
}

func deleteErrorCode(err error, fallback string) (string, bool) {
	var step deleteStepError
	if errors.As(err, &step) {
		return step.code, step.permanent
	}
	if strings.TrimSpace(fallback) == "" {
		fallback = "delete_failed"
	}
	return fallback, false
}

func redactedDeleteError(code string) error {
	return fmt.Errorf("document delete failed: %s", code)
}
