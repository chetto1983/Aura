package documents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	maximumDeleteSnapshotBytes   = 16 << 20
	maximumDeleteSnapshotEntries = 100_000
	deleteFailureMessage         = "document deletion could not be verified"
)

var deleteErrorCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// PostgresDeleteJobStore is the DurableDeleteStore backed by aura.delete_jobs.
//
// It keeps a pool and not a *sqlc.Queries because the delete tables sit under the
// fail-closed RLS floor: every statement has to run inside a transaction that first
// binds app.current_identity, and a handle that never bound one simply sees nothing.
type PostgresDeleteJobStore struct {
	pool *pgxpool.Pool
}

// NewPostgresDurableDeleteStore wraps a pool. The parameter is deliberately narrower
// than sqlc.DBTX: this store opens its own identity-scoped transactions and must not
// inherit a handle whose scope it did not set.
func NewPostgresDurableDeleteStore(pool *pgxpool.Pool) *PostgresDeleteJobStore {
	return &PostgresDeleteJobStore{pool: pool}
}

// Claim leases due jobs, taking over ones whose lease expired as well as fresh ones —
// a worker that died mid-teardown must not strand its document in deleting forever.
// Each claim bumps the lease generation, and that bump is what turns the previous
// holder's later writes into no-ops.
func (s *PostgresDeleteJobStore) Claim(
	ctx context.Context,
	req ClaimDeleteJobsRequest,
) ([]DeleteJob, error) {
	identityID, err := pgUUID("delete job identity id", req.IdentityID)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(req.WorkerID) == "" {
		return nil, fmt.Errorf("delete job worker id is required")
	}
	if req.BatchSize < 1 || req.BatchSize > 100 {
		return nil, fmt.Errorf("delete job batch size must be between 1 and 100")
	}
	if req.LeaseDuration <= 0 {
		return nil, fmt.Errorf("delete job lease duration must be positive")
	}
	var rows []sqlc.ClaimDeleteJobsRow
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		rows, queryErr = q.ClaimDeleteJobs(ctx, sqlc.ClaimDeleteJobsParams{
			IdentityID: identityID, BatchSize: int32(req.BatchSize),
			LockedBy: pgText(req.WorkerID), LeaseDuration: pgInterval(req.LeaseDuration),
		})
		return queryErr
	})
	if err != nil {
		return nil, fmt.Errorf("claim document delete jobs: %w", err)
	}
	jobs := make([]DeleteJob, 0, len(rows))
	for index := range rows {
		job, convertErr := deleteJobFromClaim(rows[index])
		if convertErr != nil {
			return nil, convertErr
		}
		jobs = append(jobs, job)
	}
	return jobs, nil
}

// Heartbeat extends the lease, and refuses rather than re-acquires once it has lapsed:
// by then another worker may already be deleting the same blobs, and two settlers is
// precisely the outcome the fence exists to prevent.
func (s *PostgresDeleteJobStore) Heartbeat(
	ctx context.Context,
	req HeartbeatDeleteJobRequest,
) error {
	identityID, jobID, err := deleteJobFenceParams(req.DeleteJobFence)
	if err != nil {
		return err
	}
	if req.LeaseDuration <= 0 {
		return fmt.Errorf("delete job lease duration must be positive")
	}
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		_, queryErr := q.HeartbeatDeleteJob(ctx, sqlc.HeartbeatDeleteJobParams{
			ID: jobID, IdentityID: identityID, LockedBy: pgText(req.WorkerID),
			LeaseGeneration: req.LeaseGeneration, LeaseDuration: pgInterval(req.LeaseDuration),
		})
		return queryErr
	})
	return fencedDeleteJobError("heartbeat", err)
}

// MarkObjectDeleted records the ledger row for a blob the worker just proved gone. The
// statement also accepts a row already marked deleted-and-verified, so an attempt that
// crashed between erasing the blob and writing this row converges on re-run instead of
// dead-lettering a document whose data is in fact already destroyed.
func (s *PostgresDeleteJobStore) MarkObjectDeleted(
	ctx context.Context,
	req MarkDeletedObjectRequest,
) error {
	identityID, jobID, err := deleteJobFenceParams(req.DeleteJobFence)
	if err != nil {
		return err
	}
	objectID, err := pgUUID("delete storage object id", req.StorageObjectID)
	if err != nil {
		return err
	}
	if req.DeletionGeneration <= 0 {
		return fmt.Errorf("delete storage object generation must be positive")
	}
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		_, queryErr := q.MarkDeleteJobStorageObjectDeleted(ctx,
			sqlc.MarkDeleteJobStorageObjectDeletedParams{
				JobID: jobID, IdentityID: identityID, LockedBy: pgText(req.WorkerID),
				LeaseGeneration: req.LeaseGeneration, StorageObjectID: objectID,
				DeletionGeneration: req.DeletionGeneration,
			})
		return queryErr
	})
	return fencedDeleteJobError("verify object deletion", err)
}

// PurgePassages physically removes the document body under the job's fence and then
// proves the removal. Amendment #117 makes erasure physical for document_chunks and
// document_embeddings, so a surviving row is a privacy failure, not a stale cache.
//
// The proof is deliberately a separate read after the delete: PurgeDocumentPassages
// returns only its own chunk count, while embeddings disappear through the FK cascade,
// so the count alone cannot show the tables are empty.
func (s *PostgresDeleteJobStore) PurgePassages(
	ctx context.Context,
	req PurgeDocumentPassagesRequest,
) (int, error) {
	identityID, jobID, err := deleteJobFenceParams(req.DeleteJobFence)
	if err != nil {
		return 0, err
	}
	documentID, err := pgUUID("delete document id", req.DocumentID)
	if err != nil {
		return 0, err
	}
	var remaining int64
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		if _, purgeErr := q.PurgeDocumentPassages(ctx, sqlc.PurgeDocumentPassagesParams{
			JobID: jobID, IdentityID: identityID, LockedBy: pgText(req.WorkerID),
			LeaseGeneration: req.LeaseGeneration,
		}); purgeErr != nil {
			return purgeErr
		}
		live, countErr := q.CountLiveDocumentPassages(ctx, sqlc.CountLiveDocumentPassagesParams{
			IdentityID: identityID, DocumentID: documentID,
		})
		remaining = live
		return countErr
	})
	if err != nil {
		// Not fencedDeleteJobError: that maps pgx.ErrNoRows to a lost lease, which the
		// worker treats as someone else's problem. An unproven erasure must stay this
		// job's problem and retry.
		return 0, fmt.Errorf("purge document passages: %w", err)
	}
	return int(remaining), nil
}

// Retry reschedules the job and returns the state it actually settled into, which may
// be dead_letter: the statement flips there itself once attempts are exhausted, so a
// caller that assumes "queued" would report a retry that will never happen.
func (s *PostgresDeleteJobStore) Retry(
	ctx context.Context,
	req RetryDeleteJobRequest,
) (DeleteJob, error) {
	identityID, jobID, err := deleteJobFenceParams(req.DeleteJobFence)
	if err != nil {
		return DeleteJob{}, err
	}
	if err := validateDeleteErrorCode(req.ErrorCode); err != nil {
		return DeleteJob{}, err
	}
	var row sqlc.RetryDeleteJobRow
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.RetryDeleteJob(ctx, sqlc.RetryDeleteJobParams{
			ID: jobID, IdentityID: identityID, LockedBy: pgText(req.WorkerID),
			LeaseGeneration: req.LeaseGeneration, ErrorCode: req.ErrorCode,
			ErrorMessage: deleteFailureMessage, NextAttemptAt: pgTime(req.NextAttemptAt),
			EventMessage: deleteFailureMessage,
		})
		return queryErr
	})
	if err != nil {
		return DeleteJob{}, fencedDeleteJobError("retry", err)
	}
	return deleteJobFromRetry(row)
}

// DeadLetter parks a job that can never succeed. Only the validated code varies; the
// persisted message is always deleteFailureMessage, so an operator-visible row cannot
// carry text produced by the tenant's storage or projection backend.
func (s *PostgresDeleteJobStore) DeadLetter(
	ctx context.Context,
	req DeadLetterDeleteJobRequest,
) (DeleteJob, error) {
	identityID, jobID, err := deleteJobFenceParams(req.DeleteJobFence)
	if err != nil {
		return DeleteJob{}, err
	}
	if err := validateDeleteErrorCode(req.ErrorCode); err != nil {
		return DeleteJob{}, err
	}
	var row sqlc.UpdateDeleteJobStatusRow
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.UpdateDeleteJobStatus(ctx, sqlc.UpdateDeleteJobStatusParams{
			ID: jobID, IdentityID: identityID, LockedBy: pgText(req.WorkerID),
			LeaseGeneration: req.LeaseGeneration, Status: "dead_letter",
			ErrorCode: req.ErrorCode, ErrorMessage: deleteFailureMessage,
			EventType: "delete_dead_letter", EventMessage: deleteFailureMessage,
		})
		return queryErr
	})
	if err != nil {
		return DeleteJob{}, fencedDeleteJobError("dead-letter", err)
	}
	return deleteJobFromTransition(row)
}

// Finalize marks the document deleted, but only where PostgreSQL agrees nothing
// survives: the statement re-checks live storage objects, chunks, and embeddings on its
// own, so a worker that mis-reported success still cannot close the job.
func (s *PostgresDeleteJobStore) Finalize(
	ctx context.Context,
	req FinalizeDeleteJobRequest,
) (Document, error) {
	identityID, jobID, err := deleteJobFenceParams(req.DeleteJobFence)
	if err != nil {
		return Document{}, err
	}
	var row sqlc.FinalizeDocumentDeleteRow
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.FinalizeDocumentDelete(ctx, sqlc.FinalizeDocumentDeleteParams{
			ID: jobID, IdentityID: identityID, LockedBy: pgText(req.WorkerID),
			LeaseGeneration:    req.LeaseGeneration,
			ProjectionVerified: req.ProjectionVerified,
			EventMessage:       "document deletion verified",
		})
		return queryErr
	})
	if err != nil {
		return Document{}, fencedDeleteJobError("finalize", err)
	}
	return catalogDocumentFromSQL(sqlc.AuraDocuments(row))
}

func (s *PostgresDeleteJobStore) withIdentity(
	ctx context.Context,
	identityID string,
	fn func(*sqlc.Queries) error,
) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("document delete job store is not configured")
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, fn)
}

func deleteJobFenceParams(fence DeleteJobFence) (identityID, jobID pgtype.UUID, err error) {
	pgIdentityID, err := pgUUID("delete job identity id", fence.IdentityID)
	if err != nil {
		return identityID, jobID, err
	}
	pgJobID, err := pgUUID("delete job id", fence.JobID)
	if err != nil {
		return identityID, jobID, err
	}
	if strings.TrimSpace(fence.WorkerID) == "" {
		return identityID, jobID, fmt.Errorf("delete job worker id is required")
	}
	if fence.LeaseGeneration <= 0 {
		return identityID, jobID, fmt.Errorf("delete job lease generation must be positive")
	}
	return pgIdentityID, pgJobID, nil
}

func fencedDeleteJobError(action string, err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s document delete job: %w", action, ErrDeleteJobLeaseLost)
	}
	if err != nil {
		return fmt.Errorf("%s document delete job: %w", action, err)
	}
	return nil
}

func validateDeleteErrorCode(code string) error {
	if !deleteErrorCodePattern.MatchString(code) {
		return fmt.Errorf("document delete error code is invalid")
	}
	return nil
}

func parseDeleteSnapshot(raw []byte, documentID string) (DeleteSnapshot, error) {
	if len(raw) == 0 || len(raw) > maximumDeleteSnapshotBytes {
		return DeleteSnapshot{}, fmt.Errorf("document delete snapshot size is invalid")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var snapshot DeleteSnapshot
	if err := decoder.Decode(&snapshot); err != nil {
		return DeleteSnapshot{}, fmt.Errorf("decode document delete snapshot: %w", err)
	}
	if err := ensureJSONEnd(decoder); err != nil {
		return DeleteSnapshot{}, err
	}
	if snapshot.DocumentID != documentID {
		return DeleteSnapshot{}, fmt.Errorf("document delete snapshot owner mismatch")
	}
	if _, err := uuid.Parse(snapshot.DocumentID); err != nil {
		return DeleteSnapshot{}, fmt.Errorf("document delete snapshot document id is invalid")
	}
	if snapshot.PipelineGeneration < 0 ||
		len(snapshot.Objects) > maximumDeleteSnapshotEntries ||
		len(snapshot.ProjectionGenerations) > maximumDeleteSnapshotEntries {
		return DeleteSnapshot{}, fmt.Errorf("document delete snapshot bounds are invalid")
	}
	if err := validateDeleteSnapshotEntries(&snapshot); err != nil {
		return DeleteSnapshot{}, err
	}
	return snapshot, nil
}

func validateDeleteSnapshotEntries(snapshot *DeleteSnapshot) error {
	objectIDs := make(map[string]struct{}, len(snapshot.Objects))
	for index := range snapshot.Objects {
		object := &snapshot.Objects[index]
		if _, err := uuid.Parse(object.ID); err != nil || strings.TrimSpace(object.Bucket) == "" ||
			strings.TrimSpace(object.ObjectKey) == "" || object.DeletionGeneration <= 0 {
			return fmt.Errorf("document delete snapshot object %d is invalid", index)
		}
		if _, duplicate := objectIDs[object.ID]; duplicate {
			return fmt.Errorf("document delete snapshot contains a duplicate object")
		}
		objectIDs[object.ID] = struct{}{}
	}
	slices.SortFunc(snapshot.Objects, func(left, right DeleteSnapshotObject) int {
		return strings.Compare(left.ID, right.ID)
	})
	slices.Sort(snapshot.ProjectionGenerations)
	for index, generation := range snapshot.ProjectionGenerations {
		if generation < 0 || (index > 0 && snapshot.ProjectionGenerations[index-1] == generation) {
			return fmt.Errorf("document delete snapshot projection generation is invalid")
		}
	}
	return nil
}

func ensureJSONEnd(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return fmt.Errorf("document delete snapshot has trailing data")
	}
	return nil
}
