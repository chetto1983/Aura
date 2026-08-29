package cron

// store_notifications_fake_test.go covers the durable-notification-queue Store methods
// (InsertPendingNotification + the Mark* writers + MarkUnknownRecovery) over the same
// in-memory sqlc.DBTX fake as store_fake_test.go, hitting the default-fill, arg-binding,
// invalid-UUID, and DB-error-wrap branches the db_integration happy-path tier misses.
// The pool-bound SweepDueNotifications (db.WithTx) stays in the real-PG tier.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// pendingRow returns the 12-column AuraPendingNotifications value-set (migration 0105
// added steer_queue_id) in the InsertPendingNotification/SweepDueNotifications
// RETURNING order.
func pendingRow(t *testing.T) []any {
	t.Helper()
	now := time.Date(2026, 6, 13, 9, 30, 0, 0, time.UTC)
	return []any{
		uuidVal(t, fakeNotifID), // id
		uuidVal(t, fakeRunID),   // run_id
		"email",                 // notify_route
		"late summary",          // body
		pgtype.Timestamptz{Time: now, Valid: true}, // notify_after
		int32(2),                                 // attempts
		pgtype.Text{String: "boom", Valid: true}, // last_error
		"failed",                                 // status
		pgtype.Timestamptz{Time: now, Valid: true}, // created_at
		pgtype.Timestamptz{Time: now, Valid: true}, // updated_at
		"id-owner",    // identity_id
		pgtype.UUID{}, // steer_queue_id (unset for a RunID-owned row)
	}
}

// --- InsertPendingNotification (incl. status + notify_after defaulting) ---

func TestStoreFakeInsertPendingNotificationSuccessAndDefaults(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{values: pendingRow(t)}}
	s := storeWithFake(f)

	got, err := s.InsertPendingNotification(context.Background(), InsertPendingNotificationParams{
		RunID: fakeRunID, NotifyRoute: "none", IdentityID: "local", Body: "late summary", // Status + NotifyAfter intentionally unset → defaulted
	})
	if err != nil {
		t.Fatalf("InsertPendingNotification: %v", err)
	}
	if got.ID != fakeNotifID || got.Status != "failed" || got.Attempts != 2 || got.IdentityID != "id-owner" {
		t.Fatalf("pending projection mismatch: %+v", got)
	}
	// Unset Status defaults to "pending"; unset NotifyAfter defaults to now() (a non-NULL ts).
	if st := f.gotArgs[7].(string); st != "pending" {
		t.Fatalf("empty Status must default to \"pending\", got %q", st)
	}
	if na := f.gotArgs[4].(pgtype.Timestamptz); !na.Valid {
		t.Fatalf("empty NotifyAfter must default to a non-NULL now(), got %#v", na)
	}
}

func TestStoreFakeInsertPendingNotificationExplicitFields(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{values: pendingRow(t)}}
	s := storeWithFake(f)

	after := time.Date(2026, 6, 13, 7, 30, 0, 0, time.UTC)
	if _, err := s.InsertPendingNotification(context.Background(), InsertPendingNotificationParams{
		RunID: fakeRunID, Body: "deferred", Status: "pending", NotifyAfter: after,
		NotifyRoute: "whatsapp", IdentityID: "id-X", Attempts: 1, LastError: "prev",
	}); err != nil {
		t.Fatalf("InsertPendingNotification: %v", err)
	}
	if na := f.gotArgs[4].(pgtype.Timestamptz); !na.Time.Equal(after) {
		t.Fatalf("explicit NotifyAfter must bind verbatim, got %v", na.Time)
	}
	if id := f.gotArgs[8].(string); id != "id-X" {
		t.Fatalf("IdentityID must bind as the snapshot text, got %#v", id)
	}
}

func TestStoreFakeInsertPendingNotificationInvalidRunUUID(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{})
	if _, err := s.InsertPendingNotification(context.Background(), InsertPendingNotificationParams{RunID: "bogus", NotifyRoute: "none", IdentityID: "local", Body: "x"}); err == nil {
		t.Fatal("InsertPendingNotification must reject an invalid run UUID")
	}
}

func TestStoreFakeInsertPendingNotificationDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{queryRowVal: &cronFakeRow{scanErr: errDB}}
	s := storeWithFake(f)
	if _, err := s.InsertPendingNotification(context.Background(), InsertPendingNotificationParams{RunID: fakeRunID, NotifyRoute: "none", IdentityID: "local", Body: "x"}); !errors.Is(err, errDB) {
		t.Fatalf("InsertPendingNotification must wrap the DB error, got %v", err)
	}
}

// --- MarkNotificationDelivered / Failed ---

func TestStoreFakeMarkNotificationDelivered(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{}
	s := storeWithFake(f)
	if err := s.MarkNotificationDelivered(context.Background(), fakeNotifID); err != nil {
		t.Fatalf("MarkNotificationDelivered: %v", err)
	}
}

func TestStoreFakeMarkNotificationDeliveredInvalidUUID(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{})
	if err := s.MarkNotificationDelivered(context.Background(), "bogus"); err == nil {
		t.Fatal("MarkNotificationDelivered must reject an invalid UUID")
	}
}

func TestStoreFakeMarkNotificationDeliveredDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{execErr: errDB}
	s := storeWithFake(f)
	if err := s.MarkNotificationDelivered(context.Background(), fakeNotifID); !errors.Is(err, errDB) {
		t.Fatalf("MarkNotificationDelivered must wrap a valid-UUID DB error, got %v", err)
	}
}

func TestStoreFakeMarkNotificationFailed(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{}
	s := storeWithFake(f)
	if err := s.MarkNotificationFailed(context.Background(), fakeNotifID, "telegram down"); err != nil {
		t.Fatalf("MarkNotificationFailed: %v", err)
	}
	if le := f.gotArgs[1].(pgtype.Text); !le.Valid || le.String != "telegram down" {
		t.Fatalf("last_error must bind as text, got %#v", le)
	}
}

func TestStoreFakeMarkNotificationFailedInvalidUUID(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{})
	if err := s.MarkNotificationFailed(context.Background(), "bogus", "x"); err == nil {
		t.Fatal("MarkNotificationFailed must reject an invalid UUID")
	}
}

func TestStoreFakeMarkNotificationFailedDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{execErr: errDB}
	s := storeWithFake(f)
	if err := s.MarkNotificationFailed(context.Background(), fakeNotifID, "x"); !errors.Is(err, errDB) {
		t.Fatalf("MarkNotificationFailed must wrap a valid-UUID DB error, got %v", err)
	}
}

// --- MarkUnknownRecovery ---

func TestStoreFakeMarkUnknownRecovery(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{}
	s := storeWithFake(f)
	if err := s.MarkUnknownRecovery(context.Background(), fakeRunID); err != nil {
		t.Fatalf("MarkUnknownRecovery: %v", err)
	}
}

func TestStoreFakeMarkUnknownRecoveryInvalidUUID(t *testing.T) {
	t.Parallel()
	s := storeWithFake(&cronFakeDBTX{})
	if err := s.MarkUnknownRecovery(context.Background(), "bogus"); err == nil {
		t.Fatal("MarkUnknownRecovery must reject an invalid run UUID")
	}
}

func TestStoreFakeMarkUnknownRecoveryDBErrorWraps(t *testing.T) {
	t.Parallel()
	f := &cronFakeDBTX{execErr: errDB}
	s := storeWithFake(f)
	if err := s.MarkUnknownRecovery(context.Background(), fakeRunID); !errors.Is(err, errDB) {
		t.Fatalf("MarkUnknownRecovery must wrap a valid-UUID DB error, got %v", err)
	}
}
