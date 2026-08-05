package documents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/objectstore"
)

// ProcessOwner claims and runs a batch for one identity, joining the per-job failures
// instead of returning at the first: one poisoned document must not hold up the erasure
// of everything queued behind it. Cancellation does stop the loop, and the untouched
// jobs simply keep their lease until it lapses and a later claim takes them over.
func (w *DurableDeleteWorker) ProcessOwner(
	ctx context.Context,
	req ClaimDeleteJobsRequest,
) ([]DeleteRunResult, error) {
	if w == nil || w.Store == nil {
		return nil, fmt.Errorf("document durable delete worker is not configured")
	}
	jobs, err := w.Store.Claim(ctx, req)
	if err != nil {
		return nil, err
	}
	results := make([]DeleteRunResult, 0, len(jobs))
	var failures []error
	for index := range jobs {
		result, runErr := w.RunClaimed(ctx, jobs[index])
		results = append(results, result)
		if runErr != nil {
			failures = append(failures, runErr)
		}
		if ctx.Err() != nil {
			break
		}
	}
	return results, errors.Join(failures...)
}

// RunClaimed drives one already-claimed job to proven erasure. The step order —
// projections, staged sandbox copies, blobs, passages, then Finalize — is chosen so a
// crash between any two steps leaves a state a later claim can resume from, and every
// step re-reads its own result rather than trusting the call that performed it: a delete
// that returned no error is not evidence the data is gone.
func (w *DurableDeleteWorker) RunClaimed(
	ctx context.Context,
	job DeleteJob,
) (DeleteRunResult, error) {
	result := DeleteRunResult{JobID: job.ID, Status: job.Status}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if w == nil || w.Store == nil {
		return result, fmt.Errorf("document durable delete worker is not configured")
	}
	if err := validateClaimedDeleteJob(job); err != nil {
		return w.settleFailure(ctx, job, result, err)
	}
	snapshot, err := normalizedDeleteSnapshot(job)
	if err != nil {
		return w.settleFailure(ctx, job, result, permanentDelete("snapshot_invalid"))
	}
	job.Snapshot = snapshot

	if err := w.deleteProjections(ctx, job, &result); err != nil {
		return w.settleFailure(ctx, job, result, err)
	}
	if err := w.purgeStaged(ctx, job); err != nil {
		return w.settleFailure(ctx, job, result, err)
	}
	if err := w.deleteObjects(ctx, job, &result); err != nil {
		return w.settleFailure(ctx, job, result, err)
	}
	if err := w.purgePassages(ctx, job); err != nil {
		return w.settleFailure(ctx, job, result, err)
	}
	if err := w.heartbeat(ctx, job); err != nil {
		return result, err
	}
	deleted, err := w.Store.Finalize(ctx, FinalizeDeleteJobRequest{
		DeleteJobFence:     fenceForDeleteJob(job),
		ProjectionVerified: true,
	})
	if err != nil {
		if errors.Is(err, ErrDeleteJobLeaseLost) {
			return result, err
		}
		return w.settleFailure(ctx, job, result, transientDelete("finalize_failed"))
	}
	if deleted.Status != DocumentStatusDeleted || deleted.ID != job.DocumentID {
		return w.settleFailure(ctx, job, result, permanentDelete("finalize_postcondition_failed"))
	}
	result.Status = "succeeded"
	return result, nil
}

// purgePassages erases the extracted document text and its embeddings, then refuses to
// continue unless PostgreSQL reports zero live rows. Amendment #117: finalization is
// gated on proven erasure, so an unproven purge retries rather than reporting success.
func (w *DurableDeleteWorker) purgePassages(ctx context.Context, job DeleteJob) error {
	remaining, err := w.Store.PurgePassages(ctx, PurgeDocumentPassagesRequest{
		DeleteJobFence: fenceForDeleteJob(job), DocumentID: job.DocumentID,
	})
	if err != nil {
		if contextError(ctx, err) != nil {
			return contextError(ctx, err)
		}
		return transientDelete("passage_purge_failed")
	}
	if remaining > 0 {
		return transientDelete("passages_still_present")
	}
	return nil
}

func (w *DurableDeleteWorker) deleteProjections(
	ctx context.Context,
	job DeleteJob,
	result *DeleteRunResult,
) error {
	if len(job.Snapshot.ProjectionGenerations) == 0 {
		return nil
	}
	if w.Projector == nil {
		return permanentDelete("projection_store_unconfigured")
	}
	for _, generation := range job.Snapshot.ProjectionGenerations {
		if err := w.heartbeat(ctx, job); err != nil {
			return err
		}
		generationText := strconv.FormatInt(generation, 10)
		if _, err := w.Projector.TombstoneGeneration(
			ctx, job.IdentityID, job.DocumentID, generationText,
		); err != nil {
			if contextError(ctx, err) != nil {
				return contextError(ctx, err)
			}
			return transientDelete("projection_tombstone_failed")
		}
		if _, err := w.Projector.DeleteGeneration(
			ctx, job.IdentityID, job.DocumentID, generationText,
		); err != nil {
			if contextError(ctx, err) != nil {
				return contextError(ctx, err)
			}
			return transientDelete("projection_delete_failed")
		}
		verification, err := w.Projector.DeleteGeneration(
			ctx, job.IdentityID, job.DocumentID, generationText,
		)
		if err != nil {
			if contextError(ctx, err) != nil {
				return contextError(ctx, err)
			}
			return transientDelete("projection_verify_failed")
		}
		if verification.Found {
			return transientDelete("projection_still_present")
		}
		result.Projections++
	}
	return nil
}

func (w *DurableDeleteWorker) purgeStaged(ctx context.Context, job DeleteJob) error {
	if w.Staged == nil {
		return permanentDelete("sandbox_purger_unconfigured")
	}
	if err := w.heartbeat(ctx, job); err != nil {
		return err
	}
	ownerCtx := identityctx.WithIdentityID(ctx, job.IdentityID)
	if err := w.Staged.PurgeStagedDocument(ownerCtx, job.DocumentID); err != nil {
		if contextError(ctx, err) != nil {
			return contextError(ctx, err)
		}
		return transientDelete("sandbox_purge_failed")
	}
	return nil
}

func (w *DurableDeleteWorker) deleteObjects(
	ctx context.Context,
	job DeleteJob,
	result *DeleteRunResult,
) error {
	if len(job.Snapshot.Objects) == 0 {
		return nil
	}
	if w.Objects == nil {
		return permanentDelete("object_store_unconfigured")
	}
	resolved, err := w.Objects.ResolveDeleteObjectStore(ctx, job.IdentityID)
	if err != nil {
		if contextError(ctx, err) != nil {
			return contextError(ctx, err)
		}
		return transientDelete("object_store_resolve_failed")
	}
	if resolved.Store == nil || strings.TrimSpace(resolved.Bucket) == "" {
		return permanentDelete("object_store_invalid")
	}
	for _, object := range job.Snapshot.Objects {
		if object.Bucket != resolved.Bucket {
			return permanentDelete("object_bucket_mismatch")
		}
	}
	for _, object := range job.Snapshot.Objects {
		if err := w.heartbeat(ctx, job); err != nil {
			return err
		}
		ref := objectstore.ObjectRef{Bucket: object.Bucket, Key: object.ObjectKey}
		if err := resolved.Store.Delete(ctx, ref); err != nil && !w.isNotFound(err) {
			if contextError(ctx, err) != nil {
				return contextError(ctx, err)
			}
			return transientDelete("object_delete_failed")
		}
		if _, err := resolved.Store.Head(ctx, ref); err == nil {
			return transientDelete("object_still_present")
		} else if !w.isNotFound(err) {
			if contextError(ctx, err) != nil {
				return contextError(ctx, err)
			}
			return transientDelete("object_verify_failed")
		}
		if err := w.Store.MarkObjectDeleted(ctx, MarkDeletedObjectRequest{
			DeleteJobFence: fenceForDeleteJob(job), StorageObjectID: object.ID,
			DeletionGeneration: object.DeletionGeneration,
		}); err != nil {
			return err
		}
		result.ObjectsDeleted++
	}
	return nil
}

func (w *DurableDeleteWorker) heartbeat(ctx context.Context, job DeleteJob) error {
	lease := w.config().LeaseDuration
	return w.Store.Heartbeat(ctx, HeartbeatDeleteJobRequest{
		DeleteJobFence: fenceForDeleteJob(job), LeaseDuration: lease,
	})
}

func (w *DurableDeleteWorker) settleFailure(
	ctx context.Context,
	job DeleteJob,
	result DeleteRunResult,
	failure error,
) (DeleteRunResult, error) {
	if contextError(ctx, failure) != nil {
		return result, contextError(ctx, failure)
	}
	if errors.Is(failure, ErrDeleteJobLeaseLost) {
		return result, failure
	}
	code, permanent := deleteErrorCode(failure, "delete_failed")
	if permanent {
		settled, err := w.Store.DeadLetter(ctx, DeadLetterDeleteJobRequest{
			DeleteJobFence: fenceForDeleteJob(job), ErrorCode: code,
		})
		if err != nil {
			return result, err
		}
		result.Status = settled.Status
		return result, redactedDeleteError(code)
	}
	config := w.config()
	retryAt := config.Now().Add(config.Jitter(exponentialRetryCap(config.RetryBase, job.AttemptCount)))
	settled, err := w.Store.Retry(ctx, RetryDeleteJobRequest{
		DeleteJobFence: fenceForDeleteJob(job), ErrorCode: code, NextAttemptAt: retryAt,
	})
	if err != nil {
		return result, err
	}
	result.Status = settled.Status
	if settled.Status == "queued" {
		result.RetryAt = retryAt
	}
	return result, redactedDeleteError(code)
}

func (w *DurableDeleteWorker) config() DurableDeleteWorkerConfig {
	config := w.Config
	if config.LeaseDuration <= 0 {
		config.LeaseDuration = 20 * time.Minute
	}
	if config.RetryBase <= 0 {
		config.RetryBase = time.Minute
	}
	if config.Jitter == nil {
		config.Jitter = fullJitter
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.IsNotFound == nil {
		config.IsNotFound = defaultDeleteObjectNotFound
	}
	return config
}

func (w *DurableDeleteWorker) isNotFound(err error) bool {
	return err != nil && w.config().IsNotFound(err)
}

func normalizedDeleteSnapshot(job DeleteJob) (DeleteSnapshot, error) {
	if len(job.snapshotJSON) > 0 {
		return parseDeleteSnapshot(job.snapshotJSON, job.DocumentID)
	}
	raw, err := json.Marshal(job.Snapshot)
	if err != nil {
		return DeleteSnapshot{}, err
	}
	return parseDeleteSnapshot(raw, job.DocumentID)
}

func validateClaimedDeleteJob(job DeleteJob) error {
	if strings.TrimSpace(job.ID) == "" || strings.TrimSpace(job.IdentityID) == "" ||
		strings.TrimSpace(job.DocumentID) == "" || strings.TrimSpace(job.WorkerID) == "" ||
		job.Status != "running" || job.Scope != "hard" || job.LeaseGeneration <= 0 ||
		job.AttemptCount <= 0 || job.MaxAttempts <= 0 {
		return permanentDelete("job_state_invalid")
	}
	return nil
}

func fenceForDeleteJob(job DeleteJob) DeleteJobFence {
	return DeleteJobFence{
		IdentityID: job.IdentityID, JobID: job.ID,
		WorkerID: job.WorkerID, LeaseGeneration: job.LeaseGeneration,
	}
}

func contextError(ctx context.Context, err error) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return nil
}

func defaultDeleteObjectNotFound(err error) bool {
	return objectstore.IsNotFound(err)
}
