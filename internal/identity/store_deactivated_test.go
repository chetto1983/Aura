package identity

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestGetIdentityByID_MapsDeactivated proves fromRow surfaces the 0029 deactivated_at column
// onto Identity.Deactivated (the HI-02 auth gate reads it): a non-NULL deactivated_at → true,
// a NULL deactivated_at → false (a live identity). GetIdentityByID is the public entry that
// runs fromRow; the queries themselves gain NO `deactivated_at IS NULL` filter (the
// deprovision saga + admin roster must still see deactivated rows — the deny is at the auth
// boundary, not in the query).
func TestGetIdentityByID_MapsDeactivated(t *testing.T) {
	t.Parallel()
	u := uuid.MustParse(fixedUUID)

	// Row with a NON-NULL deactivated_at at position 5 (same positional order idRow uses).
	deactivatedRow := []any{
		pgtype.UUID{Bytes: u, Valid: true},
		"victim",
		"user",
		pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true},   // created_at
		pgtype.Timestamptz{Time: time.Unix(100, 0), Valid: true}, // deactivated_at (NON-NULL)
		pgtype.Timestamptz{Time: time.Unix(200, 0), Valid: true}, // purge_after
	}
	fake := &fakeDBTX{queryRowVal: &fakeRow{values: deactivatedRow}}
	got, err := newFakeStore(fake).GetIdentityByID(context.Background(), fixedUUID)
	if err != nil {
		t.Fatalf("GetIdentityByID(deactivated): %v", err)
	}
	if !got.Deactivated {
		t.Fatalf("Identity.Deactivated = false, want true for a non-NULL deactivated_at")
	}

	// A live identity (idRow sets deactivated_at NULL at position 5) → Deactivated=false.
	liveFake := &fakeDBTX{queryRowVal: &fakeRow{values: idRow(t, fixedUUID, "local", "system")}}
	live, err := newFakeStore(liveFake).GetIdentityByID(context.Background(), fixedUUID)
	if err != nil {
		t.Fatalf("GetIdentityByID(live): %v", err)
	}
	if live.Deactivated {
		t.Fatal("Identity.Deactivated = true, want false for a NULL deactivated_at")
	}
}
