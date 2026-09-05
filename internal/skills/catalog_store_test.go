package skills

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// catalog_store_test.go is the daemon-free half of the catalog: the validation that runs
// before a connection is borrowed, and the projection from the sqlc row. The round trip
// itself needs a live Postgres and lives in catalog_store_integration_test.go.

const catalogTestOwner = "66666666-6666-4666-8666-666666666666"

// TestCatalogValidatesTheOwnerBeforeTouchingPostgres passes a nil *sqlc.Queries on purpose:
// if the owner check ever stopped running first, these calls would panic on it instead of
// returning an error. The test therefore pins the ORDER, not only the message.
func TestCatalogValidatesTheOwnerBeforeTouchingPostgres(t *testing.T) {
	t.Parallel()
	if _, err := UpsertCatalogTx(t.Context(), nil, CatalogUpsert{OwnerID: "local", Name: "calc"}); err == nil ||
		!strings.Contains(err.Error(), "owner identity") {
		t.Fatalf("UpsertCatalogTx with a label owner = %v, want an owner-identity error", err)
	}
	if err := DeleteCatalogTx(t.Context(), nil, "local", "calc"); err == nil ||
		!strings.Contains(err.Error(), "owner identity") {
		t.Fatalf("DeleteCatalogTx with a label owner = %v, want an owner-identity error", err)
	}
}

// TestCatalogStoreRejectsMalformedIdentities proves the store's read paths refuse a
// non-identity before opening a transaction (the store here has no pool, so reaching one
// would panic).
func TestCatalogStoreRejectsMalformedIdentities(t *testing.T) {
	t.Parallel()
	s := &CatalogStore{}
	if _, err := s.ListOwned(t.Context(), "local"); err == nil {
		t.Error("ListOwned must reject an owner that is not an identity uuid")
	}
	if _, err := s.ListAlwaysApply(t.Context(), "local"); err == nil {
		t.Error("ListAlwaysApply must reject an owner that is not an identity uuid")
	}
	if _, err := s.ListByIDs(t.Context(), catalogTestOwner, []string{"not-a-uuid"}); err == nil {
		t.Error("ListByIDs must reject an id that is not a uuid")
	}
}

// TestCatalogListByIDsShortCircuitsOnNothing proves the shared-in half asks Postgres nothing
// when the ACL returned no ids — the common case for an identity nobody has shared with.
func TestCatalogListByIDsShortCircuitsOnNothing(t *testing.T) {
	t.Parallel()
	rows, err := (&CatalogStore{}).ListByIDs(t.Context(), catalogTestOwner, nil)
	if err != nil || rows != nil {
		t.Fatalf("ListByIDs(nil) = (%v, %v), want (nil, nil)", rows, err)
	}
}

// TestNewCatalogStoreRefusesANilPool keeps the nil out of the composition root's hands: a
// store over no pool would panic on first use, so it is never constructed.
func TestNewCatalogStoreRefusesANilPool(t *testing.T) {
	t.Parallel()
	if got := NewCatalogStore(nil); got != nil {
		t.Fatal("NewCatalogStore(nil) must return nil rather than a store that panics on first use")
	}
}

// TestCatalogRowProjection pins the boundary types: plain Go strings and time, with a NULL
// uuid rendering as empty rather than as the zero uuid, which would read as a real identity.
func TestCatalogRowProjection(t *testing.T) {
	t.Parallel()
	id := uuid.MustParse(catalogTestOwner)
	now := time.Now().UTC().Truncate(time.Second)
	row := catalogRowFrom(sqlc.AuraSkillCatalog{
		ID: pgtype.UUID{Bytes: id, Valid: true},
		// A NULL owner cannot occur in the table (the column is NOT NULL); it is passed here
		// to pin what the projection does with one if a future join ever produces it.
		OwnerIdentityID: pgtype.UUID{},
		Name:            "calc",
		Description:     "desc",
		AlwaysApply:     true,
		ContentHash:     "hash",
		UpdatedAt:       pgtype.Timestamptz{Time: now, Valid: true},
	})

	if row.ID != catalogTestOwner || row.OwnerID != "" {
		t.Errorf("projection = id %q owner %q, want the id back and an empty owner for the NULL", row.ID, row.OwnerID)
	}
	if row.Name != "calc" || row.Description != "desc" || !row.AlwaysApply || row.ContentHash != "hash" {
		t.Errorf("projection dropped a field: %+v", row)
	}
	if !row.UpdatedAt.Equal(now) {
		t.Errorf("updated_at = %s, want %s", row.UpdatedAt, now)
	}
	if got := catalogUUID(pgtype.UUID{}); got != "" {
		t.Errorf("catalogUUID(NULL) = %q, want empty", got)
	}
}

// TestResolveIDReportsAnUnknownSkillAsASentinel proves the caller can tell "no such skill of
// yours" from a database failure — the CLI prints two different sentences for them, and an
// unknown name must never render as an outage.
func TestResolveIDReportsAnUnknownSkillAsASentinel(t *testing.T) {
	t.Parallel()
	// ListOwned fails on the malformed owner before any pool use, so ResolveID surfaces that
	// error and NOT the unknown-skill sentinel: the two paths stay distinguishable.
	_, err := (&CatalogStore{}).ResolveID(t.Context(), "local", "calc")
	if err == nil || errors.Is(err, ErrCatalogUnknownSkill) {
		t.Fatalf("ResolveID with a malformed owner = %v, want a validation error rather than the unknown-skill sentinel", err)
	}
}
