// Unit tier (no build tag): the Store query methods run against an in-memory fakeDBTX
// (fakedbtx_test.go) so every CRUD + grant path — success, scan-error, query-error, the
// pgx.ErrNoRows→ErrIdentityNotFound mapping, and the 23505 idempotent-grant swallow — is
// covered without a live Postgres. The real DB round-trip is additionally exercised under
// db_integration in store_test.go (the canonical conversations/identity split).
package identity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// fixedUUID is the canonical seed identity UUID (migration 0004), reused so the domain
// projection round-trips a known, asserted string.
const fixedUUID = "00000000-0000-0000-0000-000000000001"

// newFakeStore wraps an in-memory DBTX in the same sqlc surface New(pool) builds, so the
// query methods see the identical Queries shape they do in production. The pool field is
// deliberately nil: no Store method touches it (they all route through s.q).
func newFakeStore(db sqlc.DBTX) *Store {
	return &Store{q: sqlc.New(db)}
}

// idRow builds the positional scan values for one aura.identities row, in the exact order
// + type the generated GetIdentity*/ListIdentities loops scan into (&ID pgtype.UUID,
// &Name string, &Kind string, &CreatedAt pgtype.Timestamptz, &DeactivatedAt, &PurgeAfter —
// the last two are the 0029 soft-delete columns the Phase-36 queries now select; NULL for a
// live identity, ignored by the Identity domain projection).
func idRow(t *testing.T, id, name, kind string) []any {
	t.Helper()
	u, err := uuid.Parse(id)
	if err != nil {
		t.Fatalf("idRow: bad uuid %q: %v", id, err)
	}
	return []any{
		pgtype.UUID{Bytes: u, Valid: true},
		name,
		kind,
		pgtype.Timestamptz{Time: time.Unix(0, 0), Valid: true},
		pgtype.Timestamptz{}, // deactivated_at (NULL — live identity)
		pgtype.Timestamptz{}, // purge_after (NULL — live identity)
	}
}

func TestNew_BuildsStoreFromNilPool(t *testing.T) {
	t.Parallel()
	// New(nil) only stashes the handle in sqlc.New; it must not touch the pool eagerly,
	// so the Store is wireable without a connection.
	s := New(nil)
	if s == nil || s.q == nil {
		t.Fatal("New(nil): want a non-nil Store with a wired Queries")
	}
}

func TestListIdentities_FakeSuccess(t *testing.T) {
	t.Parallel()
	const otherUUID = "00000000-0000-0000-0000-0000000000ff"
	fake := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{
		idRow(t, fixedUUID, "local", "system"),
		idRow(t, otherUUID, "svc", "service"),
	}}}
	s := newFakeStore(fake)

	got, err := s.ListIdentities(context.Background())
	if err != nil {
		t.Fatalf("ListIdentities: %v", err)
	}
	want := []Identity{
		{ID: fixedUUID, Name: "local", Kind: "system"},
		{ID: otherUUID, Name: "svc", Kind: "service"},
	}
	if len(got) != len(want) {
		t.Fatalf("ListIdentities returned %d rows, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %#v, want %#v", i, got[i], want[i])
		}
	}
	if !fake.queryRows.closed {
		t.Error("ListIdentities: rows were not Closed (defer rows.Close not honoured)")
	}
}

func TestListIdentities_FakeEmpty(t *testing.T) {
	t.Parallel()
	s := newFakeStore(&fakeDBTX{queryRows: &fakeRows{rows: nil}})
	got, err := s.ListIdentities(context.Background())
	if err != nil {
		t.Fatalf("ListIdentities(empty): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListIdentities(empty): want 0 rows, got %d: %#v", len(got), got)
	}
}

func TestListIdentities_FakeQueryError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("query boom")
	s := newFakeStore(&fakeDBTX{queryErr: sentinel})
	_, err := s.ListIdentities(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("ListIdentities(queryErr): want wrapped %v, got %v", sentinel, err)
	}
}

func TestListIdentities_FakeScanError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("scan boom")
	fake := &fakeDBTX{queryRows: &fakeRows{
		rows:    [][]any{idRow(t, fixedUUID, "local", "system")},
		scanAt:  1,
		scanErr: sentinel,
	}}
	s := newFakeStore(fake)
	_, err := s.ListIdentities(context.Background())
	if !errors.Is(err, sentinel) {
		t.Fatalf("ListIdentities(scanErr): want wrapped %v, got %v", sentinel, err)
	}
}

func TestGetIdentityByName_FakeSuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{queryRowVal: &fakeRow{values: idRow(t, fixedUUID, "local", "system")}}
	s := newFakeStore(fake)

	got, err := s.GetIdentityByName(context.Background(), "local")
	if err != nil {
		t.Fatalf("GetIdentityByName: %v", err)
	}
	want := Identity{ID: fixedUUID, Name: "local", Kind: "system"}
	if got != want {
		t.Errorf("GetIdentityByName = %#v, want %#v", got, want)
	}
	// The name must be bound as a parameter, never string-interpolated into the SQL.
	if len(fake.queryArgs) != 1 || fake.queryArgs[0] != "local" {
		t.Errorf("GetIdentityByName bound args = %#v, want [\"local\"]", fake.queryArgs)
	}
}

func TestGetIdentityByName_FakeNotFound(t *testing.T) {
	t.Parallel()
	// pgx.ErrNoRows from the scan must map to the ErrIdentityNotFound sentinel, wrapped.
	fake := &fakeDBTX{queryRowVal: &fakeRow{scanErr: pgx.ErrNoRows}}
	s := newFakeStore(fake)

	_, err := s.GetIdentityByName(context.Background(), "ghost")
	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("GetIdentityByName(absent): want ErrIdentityNotFound, got %v", err)
	}
	// errors.Is must NOT leak the raw pgx sentinel past the translation boundary.
	if errors.Is(err, pgx.ErrNoRows) {
		t.Error("GetIdentityByName(absent): raw pgx.ErrNoRows leaked past the ErrIdentityNotFound boundary")
	}
}

func TestGetIdentityByName_FakeOtherError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("conn reset")
	fake := &fakeDBTX{queryRowVal: &fakeRow{scanErr: sentinel}}
	s := newFakeStore(fake)

	_, err := s.GetIdentityByName(context.Background(), "local")
	if !errors.Is(err, sentinel) {
		t.Fatalf("GetIdentityByName(otherErr): want wrapped %v, got %v", sentinel, err)
	}
	if errors.Is(err, ErrIdentityNotFound) {
		t.Error("GetIdentityByName(otherErr): a non-NoRows error must NOT map to ErrIdentityNotFound")
	}
}

func TestGetIdentityByID_FakeSuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{queryRowVal: &fakeRow{values: idRow(t, fixedUUID, "local", "system")}}
	s := newFakeStore(fake)

	got, err := s.GetIdentityByID(context.Background(), fixedUUID)
	if err != nil {
		t.Fatalf("GetIdentityByID: %v", err)
	}
	want := Identity{ID: fixedUUID, Name: "local", Kind: "system"}
	if got != want {
		t.Errorf("GetIdentityByID = %#v, want %#v", got, want)
	}
	// The id must be bound as a pgtype.UUID parameter (parsed before the round-trip).
	if len(fake.queryArgs) != 1 {
		t.Fatalf("GetIdentityByID bound %d args, want 1: %#v", len(fake.queryArgs), fake.queryArgs)
	}
	if _, ok := fake.queryArgs[0].(pgtype.UUID); !ok {
		t.Errorf("GetIdentityByID arg[0] = %T, want pgtype.UUID", fake.queryArgs[0])
	}
}

func TestGetIdentityByID_FakeBadUUID(t *testing.T) {
	t.Parallel()
	// A malformed id must fail in the parse branch BEFORE any DB call. A fake with no
	// scripted row proves QueryRow is never reached (it would nil-panic otherwise).
	s := newFakeStore(&fakeDBTX{})
	_, err := s.GetIdentityByID(context.Background(), "not-a-uuid")
	if err == nil {
		t.Fatal("GetIdentityByID(bad uuid): want parse error, got nil")
	}
	if errors.Is(err, ErrIdentityNotFound) {
		t.Error("GetIdentityByID(bad uuid): a parse failure must not be ErrIdentityNotFound")
	}
}

func TestGetIdentityByID_FakeNotFound(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{queryRowVal: &fakeRow{scanErr: pgx.ErrNoRows}}
	s := newFakeStore(fake)

	_, err := s.GetIdentityByID(context.Background(), fixedUUID)
	if !errors.Is(err, ErrIdentityNotFound) {
		t.Fatalf("GetIdentityByID(absent): want ErrIdentityNotFound, got %v", err)
	}
}

func TestGetIdentityByID_FakeOtherError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("timeout")
	fake := &fakeDBTX{queryRowVal: &fakeRow{scanErr: sentinel}}
	s := newFakeStore(fake)

	_, err := s.GetIdentityByID(context.Background(), fixedUUID)
	if !errors.Is(err, sentinel) {
		t.Fatalf("GetIdentityByID(otherErr): want wrapped %v, got %v", sentinel, err)
	}
}

func TestDeleteIdentity_FakeSuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{execTag: pgconn.NewCommandTag("DELETE 1")}
	s := newFakeStore(fake)

	if err := s.DeleteIdentity(context.Background(), "victim"); err != nil {
		t.Fatalf("DeleteIdentity: %v", err)
	}
	if len(fake.execArgs) != 1 || fake.execArgs[0] != "victim" {
		t.Errorf("DeleteIdentity bound args = %#v, want [\"victim\"]", fake.execArgs)
	}
}

func TestDeleteIdentity_FakeError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("delete boom")
	s := newFakeStore(&fakeDBTX{execErr: sentinel})
	if err := s.DeleteIdentity(context.Background(), "victim"); !errors.Is(err, sentinel) {
		t.Fatalf("DeleteIdentity(err): want wrapped %v, got %v", sentinel, err)
	}
}

func TestHasCapability_FakeMatch(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{queryRowVal: &fakeRow{values: []any{true}}}
	s := newFakeStore(fake)

	ok, err := s.HasCapability(context.Background(), fixedUUID, "web.fetch")
	if err != nil {
		t.Fatalf("HasCapability: %v", err)
	}
	if !ok {
		t.Error("HasCapability: want true, got false")
	}
	// Both id (pgtype.UUID) and capability (string) must be parameter-bound.
	if len(fake.queryArgs) != 2 {
		t.Fatalf("HasCapability bound %d args, want 2: %#v", len(fake.queryArgs), fake.queryArgs)
	}
	if _, ok := fake.queryArgs[0].(pgtype.UUID); !ok {
		t.Errorf("HasCapability arg[0] = %T, want pgtype.UUID", fake.queryArgs[0])
	}
	if fake.queryArgs[1] != "web.fetch" {
		t.Errorf("HasCapability arg[1] = %v, want \"web.fetch\"", fake.queryArgs[1])
	}
}

func TestHasCapability_FakeNoMatch(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{queryRowVal: &fakeRow{values: []any{false}}}
	s := newFakeStore(fake)

	ok, err := s.HasCapability(context.Background(), fixedUUID, "web.fetch")
	if err != nil {
		t.Fatalf("HasCapability: %v", err)
	}
	if ok {
		t.Error("HasCapability: want false, got true")
	}
}

func TestHasCapability_FakeBadUUID(t *testing.T) {
	t.Parallel()
	// Parse failure before any DB round-trip; the empty fake would nil-panic if reached.
	s := newFakeStore(&fakeDBTX{})
	if _, err := s.HasCapability(context.Background(), "not-a-uuid", "foo"); err == nil {
		t.Fatal("HasCapability(bad uuid): want parse error, got nil")
	}
}

func TestHasCapability_FakeScanError(t *testing.T) {
	t.Parallel()
	sentinel := errors.New("scan boom")
	fake := &fakeDBTX{queryRowVal: &fakeRow{scanErr: sentinel}}
	s := newFakeStore(fake)

	if _, err := s.HasCapability(context.Background(), fixedUUID, "foo"); !errors.Is(err, sentinel) {
		t.Fatalf("HasCapability(scanErr): want wrapped %v, got %v", sentinel, err)
	}
}

func TestGrantCapability_FakeSuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{execTag: pgconn.NewCommandTag("INSERT 0 1")}
	s := newFakeStore(fake)

	if err := s.GrantCapability(context.Background(), fixedUUID, "memory.read"); err != nil {
		t.Fatalf("GrantCapability: %v", err)
	}
	if len(fake.execArgs) != 2 {
		t.Fatalf("GrantCapability bound %d args, want 2: %#v", len(fake.execArgs), fake.execArgs)
	}
	if _, ok := fake.execArgs[0].(pgtype.UUID); !ok {
		t.Errorf("GrantCapability arg[0] = %T, want pgtype.UUID", fake.execArgs[0])
	}
	if fake.execArgs[1] != "memory.read" {
		t.Errorf("GrantCapability arg[1] = %v, want \"memory.read\"", fake.execArgs[1])
	}
}

func TestGrantCapability_FakeUniqueViolationSwallowed(t *testing.T) {
	t.Parallel()
	// A 23505 unique_violation is the belt-and-suspenders idempotency path: the grant is
	// a no-op, never an error.
	fake := &fakeDBTX{execErr: &pgconn.PgError{Code: "23505"}}
	s := newFakeStore(fake)

	if err := s.GrantCapability(context.Background(), fixedUUID, "memory.read"); err != nil {
		t.Fatalf("GrantCapability(23505): want nil (idempotent no-op), got %v", err)
	}
}

func TestGrantCapability_FakeNonUniqueError(t *testing.T) {
	t.Parallel()
	// A non-23505 DB error (e.g. 23503 FK violation) must surface, wrapped.
	fkErr := &pgconn.PgError{Code: "23503"}
	s := newFakeStore(&fakeDBTX{execErr: fkErr})

	err := s.GrantCapability(context.Background(), fixedUUID, "memory.read")
	if err == nil {
		t.Fatal("GrantCapability(23503): want error, got nil")
	}
	if !errors.Is(err, fkErr) {
		t.Errorf("GrantCapability(23503): want wrapped FK error, got %v", err)
	}
}

func TestGrantCapability_FakeWildcardRejectedBeforeDB(t *testing.T) {
	t.Parallel()
	// Wildcard rejection happens before any DB call; the empty fake would nil-panic if
	// Exec were reached, proving the short-circuit.
	s := newFakeStore(&fakeDBTX{})
	if err := s.GrantCapability(context.Background(), fixedUUID, "*"); !errors.Is(err, ErrWildcardManaged) {
		t.Fatalf("GrantCapability('*'): want ErrWildcardManaged, got %v", err)
	}
}

func TestGrantCapability_FakeInvalidNameRejectedBeforeDB(t *testing.T) {
	t.Parallel()
	s := newFakeStore(&fakeDBTX{})
	if err := s.GrantCapability(context.Background(), fixedUUID, "BadName"); !errors.Is(err, ErrInvalidCapability) {
		t.Fatalf("GrantCapability(invalid): want ErrInvalidCapability, got %v", err)
	}
}

func TestGrantCapability_FakeBadUUIDRejected(t *testing.T) {
	t.Parallel()
	// A valid name but malformed id fails in validateGrantInput's parseUUID branch.
	s := newFakeStore(&fakeDBTX{})
	if err := s.GrantCapability(context.Background(), "not-a-uuid", "memory.read"); err == nil {
		t.Fatal("GrantCapability(bad uuid): want parse error, got nil")
	}
}

func TestRevokeCapability_FakeSuccess(t *testing.T) {
	t.Parallel()
	fake := &fakeDBTX{execTag: pgconn.NewCommandTag("DELETE 1")}
	s := newFakeStore(fake)

	if err := s.RevokeCapability(context.Background(), fixedUUID, "memory.read"); err != nil {
		t.Fatalf("RevokeCapability: %v", err)
	}
	if len(fake.execArgs) != 2 {
		t.Fatalf("RevokeCapability bound %d args, want 2: %#v", len(fake.execArgs), fake.execArgs)
	}
	if fake.execArgs[1] != "memory.read" {
		t.Errorf("RevokeCapability arg[1] = %v, want \"memory.read\"", fake.execArgs[1])
	}
}

func TestRevokeCapability_FakeError(t *testing.T) {
	t.Parallel()
	// Unlike GrantCapability, RevokeCapability does NOT swallow 23505 — any DB error
	// surfaces, wrapped (a DELETE has no unique-violation idempotency to absorb).
	sentinel := &pgconn.PgError{Code: "23505"}
	s := newFakeStore(&fakeDBTX{execErr: sentinel})

	err := s.RevokeCapability(context.Background(), fixedUUID, "memory.read")
	if err == nil {
		t.Fatal("RevokeCapability(err): want error, got nil")
	}
	if !errors.Is(err, sentinel) {
		t.Errorf("RevokeCapability(err): want wrapped %v, got %v", sentinel, err)
	}
}

func TestRevokeCapability_FakeWildcardRejectedBeforeDB(t *testing.T) {
	t.Parallel()
	s := newFakeStore(&fakeDBTX{})
	if err := s.RevokeCapability(context.Background(), fixedUUID, "*"); !errors.Is(err, ErrWildcardManaged) {
		t.Fatalf("RevokeCapability('*'): want ErrWildcardManaged, got %v", err)
	}
}

func TestRevokeCapability_FakeInvalidNameRejectedBeforeDB(t *testing.T) {
	t.Parallel()
	s := newFakeStore(&fakeDBTX{})
	if err := s.RevokeCapability(context.Background(), fixedUUID, "Bad Name"); !errors.Is(err, ErrInvalidCapability) {
		t.Fatalf("RevokeCapability(invalid): want ErrInvalidCapability, got %v", err)
	}
}
