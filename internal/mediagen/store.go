package mediagen

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/pgnumeric"
)

// Store persists video jobs in aura.media_job. Every method runs in its owner's identity
// transaction (db.WithIdentityTx), so row-level security scopes each statement even where a
// WHERE clause would not.
type Store struct {
	pool *pgxpool.Pool
}

var _ JobStore = (*Store)(nil)

// NewStore returns a Store over the aura_app runtime pool.
func NewStore(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

// Insert persists a job just accepted by the provider, as pending or in_progress; the
// database assigns its ID and timestamps. A provider job ID already stored fails with a
// unique violation, so one paid job never gets two rows.
func (s *Store) Insert(ctx context.Context, job Job) (Job, error) {
	if err := validateNewJob(job); err != nil {
		return Job{}, err
	}
	owner, err := db.ParseUUID("owner id", job.IdentityID)
	if err != nil {
		return Job{}, err
	}
	cost, err := pgnumeric.NullableFromFloat(job.CostUSD)
	if err != nil {
		return Job{}, err
	}
	return s.withJob(ctx, job.IdentityID, func(q *sqlc.Queries) (sqlc.AuraMediaJob, error) {
		return q.InsertMediaJob(ctx, sqlc.InsertMediaJobParams{
			IdentityID: owner, ConversationID: job.ConversationID, ToolCallID: job.ToolCallID,
			ProviderJobID: job.ProviderJobID, Model: job.Model, Request: job.Request,
			Status: string(job.Status), CostUsd: cost,
		})
	})
}

func validateNewJob(job Job) error {
	switch {
	case !job.Status.active():
		return fmt.Errorf("mediagen: a new job must be pending or in_progress, not %q", job.Status)
	case job.ConversationID == "" || job.ToolCallID == "" || job.Model == "":
		return errors.New("mediagen: a new job needs its conversation, tool call and model")
	case !json.Valid(job.Request):
		return errors.New("mediagen: a new job needs its request as JSON")
	}
	_, err := validProviderID(job.ProviderJobID)
	return err
}

// Get returns the owner's job, or pgx.ErrNoRows when it is missing or belongs to another
// identity.
func (s *Store) Get(ctx context.Context, ownerID, jobID string) (Job, error) {
	key, err := jobKey(ownerID, jobID)
	if err != nil {
		return Job{}, err
	}
	return s.withJob(ctx, ownerID, func(q *sqlc.Queries) (sqlc.AuraMediaJob, error) {
		return q.GetMediaJobForIdentity(ctx, key)
	})
}

// Recoverable lists the owner's jobs a restart must pick up, oldest first: active jobs to
// resume polling and completed jobs never delivered.
func (s *Store) Recoverable(ctx context.Context, ownerID string) ([]Job, error) {
	owner, err := db.ParseUUID("owner id", ownerID)
	if err != nil {
		return nil, err
	}
	var jobs []Job
	err = db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		rows, err := q.ListRecoverableMediaJobs(ctx, owner)
		if err != nil {
			return err
		}
		jobs = make([]Job, 0, len(rows))
		for _, row := range rows {
			job, err := jobFromRow(row)
			if err != nil {
				return err
			}
			jobs = append(jobs, job)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return jobs, nil
}

// Progress records a remote status change of an active job. A non-nil cost is recorded, zero
// included; a nil cost keeps the recorded one. Failed, expired and cancelled set completed_at.
// Completed is refused: a job completes through Complete, once its clip is an Aura asset.
func (s *Store) Progress(ctx context.Context, ownerID, jobID string, status Status, cost *float64, failure *Error) (Job, error) {
	if status == StatusCompleted {
		return Job{}, errors.New("mediagen: a job completes through Complete, once its asset is ingested")
	}
	key, err := jobKey(ownerID, jobID)
	if err != nil {
		return Job{}, err
	}
	costUSD, err := pgnumeric.NullableFromFloat(cost)
	if err != nil {
		return Job{}, err
	}
	failureDoc, err := failureJSON(failure)
	if err != nil {
		return Job{}, err
	}
	return s.withJob(ctx, ownerID, func(q *sqlc.Queries) (sqlc.AuraMediaJob, error) {
		row, err := q.UpdateMediaJobProgress(ctx, sqlc.UpdateMediaJobProgressParams{
			ID: key.ID, IdentityID: key.IdentityID, Status: string(status), CostUsd: costUSD, Error: failureDoc,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return row, unmatchedUpdate(ctx, q, key, ErrJobNotActive)
		}
		return row, err
	})
}

// Complete marks an active job completed with its ingested clip. The asset must be the
// owner's accepted, undeleted agent video in the job's conversation, the one a delivery claim
// can bind; any other asset is refused as asset_not_found and the job stays active.
func (s *Store) Complete(ctx context.Context, ownerID, jobID, assetID string, cost *float64) (Job, error) {
	key, err := jobKey(ownerID, jobID)
	if err != nil {
		return Job{}, err
	}
	asset, err := db.ParseUUID("asset id", assetID)
	if err != nil {
		return Job{}, err
	}
	costUSD, err := pgnumeric.NullableFromFloat(cost)
	if err != nil {
		return Job{}, err
	}
	return s.withJob(ctx, ownerID, func(q *sqlc.Queries) (sqlc.AuraMediaJob, error) {
		row, err := q.CompleteMediaJob(ctx, sqlc.CompleteMediaJobParams{
			ID: key.ID, IdentityID: key.IdentityID, AssetID: asset, CostUsd: costUSD,
		})
		if errors.Is(err, pgx.ErrNoRows) {
			return row, unmatchedUpdate(ctx, q, key, videoAssetNotFound())
		}
		return row, err
	})
}

// withJob runs one owned-job statement in the owner's identity transaction and maps its row
// inside it, so a row that cannot be mapped rolls the statement back.
func (s *Store) withJob(ctx context.Context, ownerID string, run func(*sqlc.Queries) (sqlc.AuraMediaJob, error)) (Job, error) {
	var job Job
	err := db.WithIdentityTx(ctx, s.pool, ownerID, func(q *sqlc.Queries) error {
		row, err := run(q)
		if err != nil {
			return err
		}
		job, err = jobFromRow(row)
		return err
	})
	if err != nil {
		return Job{}, err
	}
	return job, nil
}

// jobKey parses an owned job's key. A malformed owner is a caller bug and is reported; a
// malformed job ID can only name a job that does not exist, so it reads as missing.
func jobKey(ownerID, jobID string) (sqlc.GetMediaJobForIdentityParams, error) {
	owner, err := db.ParseUUID("owner id", ownerID)
	if err != nil {
		return sqlc.GetMediaJobForIdentityParams{}, err
	}
	id, err := db.ParseUUID("job id", jobID)
	if err != nil {
		return sqlc.GetMediaJobForIdentityParams{}, pgx.ErrNoRows
	}
	return sqlc.GetMediaJobForIdentityParams{ID: id, IdentityID: owner}, nil
}

// unmatchedUpdate explains a guarded update that matched no row: the job is missing
// (pgx.ErrNoRows), no longer active (ErrJobNotActive), or still active and refused by the
// update's remaining guard (refused).
func unmatchedUpdate(ctx context.Context, q *sqlc.Queries, key sqlc.GetMediaJobForIdentityParams, refused error) error {
	row, err := q.GetMediaJobForIdentity(ctx, key)
	if err != nil {
		return err
	}
	if !Status(row.Status).active() {
		return ErrJobNotActive
	}
	return refused
}

func videoAssetNotFound() error {
	return &Error{Code: "asset_not_found", Message: "The generated video is not available for this job."}
}

func failureJSON(failure *Error) ([]byte, error) {
	if failure == nil {
		return nil, nil
	}
	return json.Marshal(failure)
}

func jobFromRow(row sqlc.AuraMediaJob) (Job, error) {
	var failure *Error
	if row.Error != nil {
		if err := json.Unmarshal(row.Error, &failure); err != nil {
			return Job{}, fmt.Errorf("mediagen: decode job %s error: %w", row.ID, err)
		}
	}
	return Job{
		ID: row.ID.String(), IdentityID: row.IdentityID.String(),
		ConversationID: row.ConversationID, ToolCallID: row.ToolCallID,
		ProviderJobID: row.ProviderJobID, Model: row.Model, Request: json.RawMessage(row.Request),
		Status: Status(row.Status), Error: failure, AssetID: row.AssetID.String(),
		CostUSD:   pgnumeric.NullableFloat(row.CostUsd),
		CreatedAt: row.CreatedAt.Time, UpdatedAt: row.UpdatedAt.Time,
		CompletedAt: optionalTime(row.CompletedAt), DeliveredAt: optionalTime(row.DeliveredAt),
	}, nil
}

func optionalTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}
	return &value.Time
}
