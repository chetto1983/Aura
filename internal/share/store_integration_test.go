//go:build db_integration

// Integration tests for internal/share.Store against a real Postgres-backed
// aura.shared_links (migration 0040). Requires a running Postgres with the migrations
// applied:
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race -p 1 -count=1 ./internal/share/
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset — a skipped
// integration test must never pass green. R-13 caution: `local` holds the `*` wildcard, so
// any capability assertion against it passes vacuously — every test here seeds a FRESH,
// non-wildcard identity (name = "share-store-"+t.Name()) instead.
package share

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func envOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI — "+
				"a skipped integration test must not pass as green; wire it in ci.yml", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// bootstrapURL composes the superuser DSN EnsureRoles needs from the password.
func bootstrapURL(t *testing.T, pwd string) string {
	t.Helper()
	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	return fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)
}

// migratedPool ensures roles + every migration is applied, then returns an aura_app pool.
// Closed via t.Cleanup.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := envOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := envOrSkip(t, "AURA_DB_URL")

	if err := db.EnsureRoles(ctx, bootstrapURL(t, pwd), pwd); err != nil {
		t.Fatalf("EnsureRoles: %v", err)
	}
	if _, err := db.Migrate(ctx, migrateURL); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	pool, err := db.Open(ctx, &db.Config{URL: appURL})
	if err != nil {
		t.Fatalf("Open (aura_app): %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// seedIdentity inserts a fresh, non-wildcard identity (R-13) named after the running test —
// parallel runs never collide, and no capability/owner assertion here ever passes
// vacuously the way it would against the seeded `local` operator. aura.identities.name is
// UNIQUE (migration 0004), and several tests here seed two or more identities in the SAME
// test function (e.g. owner + a foreign identity), so t.Name() alone is not enough to
// dedupe within one test — a fresh UUID suffix is appended per call. Its ON DELETE CASCADE
// (0004/0005/0040) means t.Cleanup deleting the identity also removes every conversation and
// shared_links row it owns, so per-test conversation seeding needs no separate cleanup.
func seedIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()
	name := "share-store-" + t.Name() + "-" + uuid.Must(uuid.NewV7()).String()
	var id string
	if err := pool.QueryRow(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES (gen_random_uuid(), $1, 'user') RETURNING id::text`,
		name,
	).Scan(&id); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, id)
	})
	return id
}

// seedConversation inserts a fresh conversation owned by identityID.
func seedConversation(t *testing.T, pool *pgxpool.Pool, identityID string) string {
	t.Helper()
	convID := uuid.Must(uuid.NewV7()).String()
	if _, err := pool.Exec(context.Background(),
		`INSERT INTO aura.conversations (id, identity_id, model, status) VALUES ($1, $2, 'test-model', 'active')`,
		convID, identityID,
	); err != nil {
		t.Fatalf("seed conversation: %v", err)
	}
	return convID
}

// newLink builds a minimal, valid Link for tier, owned by ownerID/convID.
func newLink(ownerID, convID, tier string, expiresAt *time.Time) Link {
	return Link{
		ID:              uuid.Must(uuid.NewV7()),
		OwnerIdentityID: uuid.MustParse(ownerID),
		ConversationID:  uuid.MustParse(convID),
		Tier:            tier,
		SnapshotID:      uuid.Must(uuid.NewV7()),
		SnapshotBucket:  "share-test-bucket",
		ExpiresAt:       expiresAt,
	}
}

func futurePtr(d time.Duration) *time.Time {
	tm := time.Now().UTC().Add(d)
	return &tm
}

func pastPtr(d time.Duration) *time.Time {
	tm := time.Now().UTC().Add(-d)
	return &tm
}

// pgErrCode extracts the Postgres SQLSTATE from err, or "" if err does not wrap a
// *pgconn.PgError — used so CHECK/UNIQUE violation assertions key on the SQLSTATE, never a
// message match.
func pgErrCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}

// TestShareStoreOwnerGate proves the 404-not-403 owner gate (D-06): a foreign identity's
// GetForIdentity on another owner's link returns ErrShareNotFound — not a row, not a
// distinct error — while the true owner reads it back intact.
func TestShareStoreOwnerGate(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	ownerA := seedIdentity(t, pool)
	ownerB := seedIdentity(t, pool)
	convA := seedConversation(t, pool, ownerA)

	link := newLink(ownerA, convA, "internal", nil)
	if err := store.Insert(ctx, link, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, err := store.GetForIdentity(ctx, link.ID.String(), ownerB); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("GetForIdentity(foreign owner) = %v, want ErrShareNotFound", err)
	}
	got, err := store.GetForIdentity(ctx, link.ID.String(), ownerA)
	if err != nil {
		t.Fatalf("GetForIdentity(true owner): %v", err)
	}
	if got.ID != link.ID {
		t.Fatalf("GetForIdentity(true owner) returned id %s, want %s", got.ID, link.ID)
	}
}

// TestShareRevokeThen404 proves revoke closes the public resolve path: after
// RevokeForIdentity, ResolveByToken 404s on the same token.
func TestShareRevokeThen404(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	_, hash, err := Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	link := newLink(owner, conv, "public", futurePtr(24*time.Hour))
	if err := store.Insert(ctx, link, hash[:]); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if err := store.RevokeForIdentity(ctx, link.ID.String(), owner, time.Now().UTC()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := store.ResolveByToken(ctx, hash[:]); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveByToken(revoked) = %v, want ErrShareNotFound", err)
	}
}

// TestShareExpiredLazy404 proves the lazy predicate IS the fail-closed gate (OQ3): a link
// whose expires_at is already in the past 404s on ResolveByToken with the expiry sweep NEVER
// invoked anywhere in this test — the resolve predicate alone must close the window.
func TestShareExpiredLazy404(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	_, hash, err := Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	// expires_at is set in the past at INSERT time: the tier-shape CHECK requires only
	// NOT NULL, not "in the future", so this row legitimately represents an already-due
	// link that no sweep has processed yet.
	link := newLink(owner, conv, "public", pastPtr(time.Hour))
	if err := store.Insert(ctx, link, hash[:]); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, err := store.ResolveByToken(ctx, hash[:]); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveByToken(expired, no sweep) = %v, want ErrShareNotFound", err)
	}
}

// TestShareResolveLiveByIDBearer proves the D-10 property: ResolveLiveByID takes no owner
// argument at all — the store method literally cannot filter by an owner it is never
// given — yet still returns A's internal link.
func TestShareResolveLiveByIDBearer(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	link := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, link, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}

	got, err := store.ResolveLiveByID(ctx, link.ID)
	if err != nil {
		t.Fatalf("ResolveLiveByID: %v", err)
	}
	if got.OwnerIdentityID.String() != owner {
		t.Fatalf("ResolveLiveByID owner = %s, want %s", got.OwnerIdentityID, owner)
	}
}

// TestShareResolveLiveByIDRejectsPublicTier proves tier = 'internal' is folded into the SQL
// predicate: a public link's id never resolves via ResolveLiveByID, so there is no second,
// id-addressed path to a public snapshot bypassing ResolveByToken's token predicate.
func TestShareResolveLiveByIDRejectsPublicTier(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	_, hash, err := Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	link := newLink(owner, conv, "public", futurePtr(24*time.Hour))
	if err := store.Insert(ctx, link, hash[:]); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, err := store.ResolveLiveByID(ctx, link.ID); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveLiveByID(public tier id) = %v, want ErrShareNotFound", err)
	}
}

// TestShareResolveLiveByIDLazyLiveness proves ResolveLiveByID carries the IDENTICAL lazy
// predicate as ResolveByToken: a revoked internal link and an expired internal link both
// 404 with the sweep never run, and both return the SAME ErrShareNotFound sentinel an
// unknown id returns — no tier or state oracle anywhere in the response.
func TestShareResolveLiveByIDLazyLiveness(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	revoked := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, revoked, nil); err != nil {
		t.Fatalf("insert revoked-to-be: %v", err)
	}
	if err := store.RevokeForIdentity(ctx, revoked.ID.String(), owner, time.Now().UTC()); err != nil {
		t.Fatalf("revoke: %v", err)
	}

	// An internal link's expires_at is optional (D-10 "revocable + optional expiry"); the
	// tier-shape CHECK places no constraint on it, only on token_hash, so this row is a
	// legitimate already-due internal link.
	expired := newLink(owner, conv, "internal", pastPtr(time.Hour))
	if err := store.Insert(ctx, expired, nil); err != nil {
		t.Fatalf("insert expired: %v", err)
	}

	if _, err := store.ResolveLiveByID(ctx, revoked.ID); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveLiveByID(revoked internal) = %v, want ErrShareNotFound", err)
	}
	if _, err := store.ResolveLiveByID(ctx, expired.ID); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveLiveByID(expired internal, no sweep) = %v, want ErrShareNotFound", err)
	}
	if _, err := store.ResolveLiveByID(ctx, uuid.Must(uuid.NewV7())); !errors.Is(err, ErrShareNotFound) {
		t.Fatalf("ResolveLiveByID(unknown id) = %v, want ErrShareNotFound", err)
	}
}

// TestSharePublicRequiresExpiry proves the migration 0040 shared_links_tier_shape CHECK is
// enforced by the DATABASE: a public link with a NULL expires_at, or a NULL token_hash, is
// rejected by Postgres itself. Asserted on the SQLSTATE (23514), never a message match.
func TestSharePublicRequiresExpiry(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	t.Run("public with NULL expires_at", func(t *testing.T) {
		_, hash, err := Mint()
		if err != nil {
			t.Fatalf("mint: %v", err)
		}
		link := newLink(owner, conv, "public", nil)
		err = store.Insert(ctx, link, hash[:])
		if err == nil {
			t.Fatal("insert public link with NULL expires_at succeeded, want a CHECK violation")
		}
		if code := pgErrCode(err); code != "23514" {
			t.Fatalf("insert public/NULL-expiry SQLSTATE = %q, want 23514: %v", code, err)
		}
	})

	t.Run("public with NULL token_hash", func(t *testing.T) {
		link := newLink(owner, conv, "public", futurePtr(time.Hour))
		err := store.Insert(ctx, link, nil)
		if err == nil {
			t.Fatal("insert public link with NULL token_hash succeeded, want a CHECK violation")
		}
		if code := pgErrCode(err); code != "23514" {
			t.Fatalf("insert public/NULL-hash SQLSTATE = %q, want 23514: %v", code, err)
		}
	})
}

// TestSharedLinksCascade proves the migration 0040 FK ON DELETE CASCADE: deleting a
// conversation removes its shared_links rows. This is the BACKSTOP only — the FK cannot
// drop Garage bytes (R-10), which is why the D-15 Runner lifecycle hook (plan 37F-11) stays
// mandatory; this test proves the DB-level half of that belt-and-suspenders pair.
func TestSharedLinksCascade(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	link := newLink(owner, conv, "internal", nil)
	if err := store.Insert(ctx, link, nil); err != nil {
		t.Fatalf("insert: %v", err)
	}

	if _, err := pool.Exec(ctx, `DELETE FROM aura.conversations WHERE id = $1`, conv); err != nil {
		t.Fatalf("delete conversation: %v", err)
	}

	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM aura.shared_links WHERE id = $1`, link.ID).Scan(&count); err != nil {
		t.Fatalf("count shared_links: %v", err)
	}
	if count != 0 {
		t.Fatalf("shared_links row survived conversation delete: count=%d, want 0 (FK cascade)", count)
	}
}

// TestShareTokenUniqueness proves shared_links_token_hash_idx rejects a duplicate hash.
// Asserted on the SQLSTATE (23505), never a message match.
func TestShareTokenUniqueness(t *testing.T) {
	pool := migratedPool(t)
	store := New(pool)
	ctx := context.Background()

	owner := seedIdentity(t, pool)
	conv := seedConversation(t, pool, owner)

	_, hash, err := Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	first := newLink(owner, conv, "public", futurePtr(time.Hour))
	if err := store.Insert(ctx, first, hash[:]); err != nil {
		t.Fatalf("insert first: %v", err)
	}

	second := newLink(owner, conv, "public", futurePtr(time.Hour))
	err = store.Insert(ctx, second, hash[:])
	if err == nil {
		t.Fatal("insert duplicate token_hash succeeded, want a unique violation")
	}
	if code := pgErrCode(err); code != "23505" {
		t.Fatalf("insert duplicate token_hash SQLSTATE = %q, want 23505: %v", code, err)
	}
}
