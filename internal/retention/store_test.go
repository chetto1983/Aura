package retention

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func TestStoreRejectsUnconfiguredAndInvalidTransitions(t *testing.T) {
	store := NewStore(nil)
	if _, _, err := store.SavePlan(context.Background(), Plan{}, time.Now()); err == nil {
		t.Fatal("unconfigured store accepted a plan")
	}
	if _, err := store.PendingCount(context.Background()); err == nil {
		t.Fatal("unconfigured store counted backlog")
	}
	if _, err := store.GetByToken(context.Background(), ""); err == nil {
		t.Fatal("unconfigured store resolved an empty token")
	}
	if _, err := store.Authorize(context.Background(), "bad", "token", "policy", time.Now()); err == nil {
		t.Fatal("invalid authorization ID succeeded")
	}
	if _, err := store.Claim(context.Background(), "bad", "worker", 1, time.Now(), time.Minute); err == nil {
		t.Fatal("invalid claim ID succeeded")
	}
	for name, call := range map[string]func() error{
		"record": func() error {
			return store.RecordArtifact(context.Background(), "bad", "worker", ArtifactRemoved, 1, "", time.Now())
		},
		"finalize": func() error { return store.FinalizeItem(context.Background(), "bad", "worker", time.Now()) },
		"retry":    func() error { return store.RetryItem(context.Background(), "bad", "worker", "failure", time.Now()) },
		"fail":     func() error { return store.FailItem(context.Background(), "bad", "worker", "failure", time.Now()) },
	} {
		if err := call(); err == nil {
			t.Fatalf("invalid %s item ID succeeded", name)
		}
	}
	if _, err := store.FinalizeOperation(context.Background(), "bad", time.Now()); err == nil {
		t.Fatal("invalid finalization ID succeeded")
	}
	if err := retentionRows("transition", 0, nil); !errors.Is(err, ErrLeaseLost) {
		t.Fatalf("zero-row transition error = %v", err)
	}
	sentinel := errors.New("database unavailable")
	if err := retentionRows("transition", 1, sentinel); !errors.Is(err, sentinel) {
		t.Fatalf("database transition error = %v", err)
	}
	if err := retentionRows("transition", 1, nil); err != nil {
		t.Fatalf("one-row transition error = %v", err)
	}
}

func TestStoreRowProjectionsAndValueHelpers(t *testing.T) {
	id := uuid.Must(uuid.NewV7())
	operationID := uuid.Must(uuid.NewV7())
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.FixedZone("test", 3600))
	pgID := pgtype.UUID{Bytes: id, Valid: true}
	pgOperationID := pgtype.UUID{Bytes: operationID, Valid: true}
	operation := operationFromRow(sqlc.AuraRetentionOperations{
		ID: pgID, Token: "token", PolicyVersion: "v1", Status: string(StatusRetryable),
		CandidateCount: 3, PlannedBytes: 9, CompletedCount: 1, CompletedBytes: 4, FailureCount: 2,
	})
	if operation.ID != id.String() || operation.Status != StatusRetryable || operation.FailureCount != 2 {
		t.Fatalf("operation projection = %+v", operation)
	}
	item := itemFromRow(sqlc.AuraRetentionOperationItems{
		ID: pgID, OperationID: pgOperationID, IdentityID: "owner",
		ConversationID: pgtype.Text{String: "conversation", Valid: true},
		ArtifactID:     pgtype.Text{String: "artifact", Valid: true}, Class: string(ClassTemporary),
		Action: string(ActionDeleteArtifact), ExpectedVersion: 7, ExpectedBytes: 8,
		Status: string(StatusDeleting), ArtifactResult: ArtifactRemoved, RemovedBytes: 8,
		FailureClass:   pgtype.Text{String: "failure", Valid: true},
		ClaimOwner:     pgtype.Text{String: "worker", Valid: true},
		LeaseExpiresAt: pgtype.Timestamptz{Time: now, Valid: true}, AttemptCount: 2,
	})
	if item.ID != id.String() || item.OperationID != operationID.String() || item.Candidate.Version != 7 || item.ClaimOwner != "worker" {
		t.Fatalf("item projection = %+v", item)
	}
	parsed, err := retentionParseUUID(id.String())
	if err != nil || retentionUUIDString(parsed) != id.String() {
		t.Fatalf("UUID round trip = %+v, %v", parsed, err)
	}
	if _, err := retentionParseUUID("bad"); err == nil {
		t.Fatal("invalid UUID parsed")
	}
	if retentionUUIDString(pgtype.UUID{}) != "" || !retentionUUID().Valid {
		t.Fatal("UUID helper validity mismatch")
	}
	if got := retentionTime(now); !got.Valid || got.Time.Location() != time.UTC {
		t.Fatalf("retentionTime() = %+v", got)
	}
	if retentionText("").Valid || !retentionText("value").Valid {
		t.Fatal("retentionText validity mismatch")
	}
}
