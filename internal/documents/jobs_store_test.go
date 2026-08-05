package documents

import (
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestIngestionJobFromSQLDecodesPayloadAndLeaseFields(t *testing.T) {
	row := sqlc.AuraIngestionJobs{
		ID:                 mustCatalogTestUUID(t, "10000000-0000-0000-0000-000000000001"),
		IdentityID:         mustCatalogTestUUID(t, testIngestionIdentityID),
		JobType:            "asset_process",
		Status:             "running",
		IdempotencyKey:     "asset_process:asset-1",
		Stage:              "accepted",
		AttemptCount:       2,
		MaxAttempts:        5,
		PipelineGeneration: 3,
		AttemptGeneration:  4,
		LeaseGeneration:    5,
		LockedBy:           pgtype.Text{String: "worker-1", Valid: true},
		LockedUntil:        pgtype.Timestamptz{Time: time.Unix(20, 0), Valid: true},
		NextAttemptAt:      pgtype.Timestamptz{Time: time.Unix(10, 0), Valid: true},
		Payload:            []byte(`{"asset_id":"asset-1"}`),
		ErrorCode:          "retryable",
		ErrorMessage:       "extractor down",
		CreatedAt:          pgtype.Timestamptz{Time: time.Unix(1, 0), Valid: true},
		UpdatedAt:          pgtype.Timestamptz{Time: time.Unix(2, 0), Valid: true},
	}
	job, err := ingestionJobFromSQL(row)
	if err != nil {
		t.Fatal(err)
	}
	if job.ID != "10000000-0000-0000-0000-000000000001" || job.JobType != "asset_process" {
		t.Fatalf("job identity = %#v", job)
	}
	if job.IdentityID != testIngestionIdentityID || job.PipelineGeneration != 3 ||
		job.AttemptGeneration != 4 || job.LeaseGeneration != 5 {
		t.Fatalf("job owner/generations = %#v", job)
	}
	if job.LockedBy != "worker-1" || job.LockedUntil.IsZero() || job.NextAttemptAt.IsZero() {
		t.Fatalf("lease fields = %#v", job)
	}
	if !reflect.DeepEqual(job.Payload, map[string]any{"asset_id": "asset-1"}) {
		t.Fatalf("payload = %#v", job.Payload)
	}
}

func TestNewPostgresIngestionJobStoreWiresQueries(t *testing.T) {
	store := NewPostgresIngestionJobStore(nil)
	if store == nil {
		t.Fatalf("NewPostgresIngestionJobStore = %#v", store)
	}
}

func TestFencedIngestionJobErrorMapsNoRowsToLeaseLost(t *testing.T) {
	err := fencedIngestionJobError("transition", pgx.ErrNoRows)
	if !errors.Is(err, ErrIngestionJobLeaseLost) {
		t.Fatalf("fencedIngestionJobError = %v", err)
	}
}

func TestOptionalUUIDFromStringRejectsInvalidValue(t *testing.T) {
	_, err := optionalUUIDFromString("document id", "not-a-uuid")
	if err == nil {
		t.Fatal("expected invalid UUID error")
	}
}

func TestOptionalUUIDFromStringAllowsEmptyValue(t *testing.T) {
	got, err := optionalUUIDFromString("document id", "")
	if err != nil {
		t.Fatal(err)
	}
	if got.Valid {
		t.Fatalf("empty optional UUID should be invalid/null, got %#v", got)
	}
}
