package documents

import (
	"bytes"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type deleteJobSQLRecord struct {
	ID               pgtype.UUID
	DocumentID       pgtype.UUID
	Scope            string
	Status           string
	Steps            []byte
	AttemptCount     int32
	MaxAttempts      int32
	LockedBy         pgtype.Text
	LockedUntil      pgtype.Timestamptz
	NextAttemptAt    pgtype.Timestamptz
	IdentityID       pgtype.UUID
	DeleteGeneration int64
	LeaseGeneration  int64
}

func deleteJobFromClaim(row sqlc.ClaimDeleteJobsRow) (DeleteJob, error) {
	return deleteJobFromRecord(deleteJobSQLRecord{
		ID: row.ID, DocumentID: row.DocumentID, Scope: row.Scope, Status: row.Status,
		Steps: row.Steps, AttemptCount: row.AttemptCount, MaxAttempts: row.MaxAttempts,
		LockedBy: row.LockedBy, LockedUntil: row.LockedUntil, NextAttemptAt: row.NextAttemptAt,
		IdentityID: row.IdentityID, DeleteGeneration: row.DeleteGeneration,
		LeaseGeneration: row.LeaseGeneration,
	})
}

func deleteJobFromRetry(row sqlc.RetryDeleteJobRow) (DeleteJob, error) {
	return deleteJobFromRecord(deleteJobSQLRecord{
		ID: row.ID, DocumentID: row.DocumentID, Scope: row.Scope, Status: row.Status,
		Steps: row.Steps, AttemptCount: row.AttemptCount, MaxAttempts: row.MaxAttempts,
		LockedBy: row.LockedBy, LockedUntil: row.LockedUntil, NextAttemptAt: row.NextAttemptAt,
		IdentityID: row.IdentityID, DeleteGeneration: row.DeleteGeneration,
		LeaseGeneration: row.LeaseGeneration,
	})
}

func deleteJobFromTransition(row sqlc.UpdateDeleteJobStatusRow) (DeleteJob, error) {
	return deleteJobFromRecord(deleteJobSQLRecord{
		ID: row.ID, DocumentID: row.DocumentID, Scope: row.Scope, Status: row.Status,
		Steps: row.Steps, AttemptCount: row.AttemptCount, MaxAttempts: row.MaxAttempts,
		LockedBy: row.LockedBy, LockedUntil: row.LockedUntil, NextAttemptAt: row.NextAttemptAt,
		IdentityID: row.IdentityID, DeleteGeneration: row.DeleteGeneration,
		LeaseGeneration: row.LeaseGeneration,
	})
}

func deleteJobFromRecord(row deleteJobSQLRecord) (DeleteJob, error) {
	job := DeleteJob{
		ID: uuidString(row.ID), IdentityID: uuidString(row.IdentityID),
		DocumentID: uuidString(row.DocumentID), Scope: row.Scope, Status: row.Status,
		AttemptCount: int(row.AttemptCount), MaxAttempts: int(row.MaxAttempts),
		DeleteGeneration: row.DeleteGeneration, LeaseGeneration: row.LeaseGeneration,
		LockedUntil: timeValue(row.LockedUntil), NextAttemptAt: timeValue(row.NextAttemptAt),
		snapshotJSON: bytes.Clone(row.Steps),
	}
	if row.LockedBy.Valid {
		job.WorkerID = row.LockedBy.String
	}
	if job.ID == "" || job.IdentityID == "" || job.DocumentID == "" {
		return DeleteJob{}, fmt.Errorf("document delete job has an invalid owner binding")
	}
	if job.Scope != "hard" || job.AttemptCount < 0 || job.MaxAttempts <= 0 ||
		job.DeleteGeneration <= 0 || job.LeaseGeneration < 0 {
		return DeleteJob{}, fmt.Errorf("document delete job has invalid lifecycle state")
	}
	return job, nil
}
