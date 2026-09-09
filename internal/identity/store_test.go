//go:build db_integration

// Integration tests for internal/identity. Requires a running Postgres with the
// Phase-4 migrations applied (0004 seeds the `local` identity + '*' grant):
//
//	make db-up && aura db migrate           # or the WSL equivalent
//	AURA_DB_URL + AURA_DB_MIGRATE_URL + POSTGRES_PASSWORD set in env
//
// Run via:
//
//	go test -tags db_integration -race ./internal/identity -count=1
//
// No-skip-as-green: envOrSkip t.Fatals under $CI when the DSN is unset, so a
// skipped tier can never pass as green in the pipeline.
package identity

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// localID is the fixed seed UUID from migration 0004 (SPEC Req#5).
const localID = "00000000-0000-0000-0000-000000000001"

// envOrSkip mirrors internal/db/db_test.go: skip locally, fail-loud under CI so a
// missing DSN never reports a falsely-green integration job.
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

// migratedPool ensures roles + migrations are applied, then returns an aura_app
// pool ready for Store use. Closed via t.Cleanup.
func migratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := envOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, envOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := envOrSkip(t, "AURA_DB_URL")

	host := os.Getenv("PGHOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("PGPORT")
	if port == "" {
		port = "5432"
	}
	bootstrap := fmt.Sprintf("postgres://aura:%s@%s:%s/aura?sslmode=disable", pwd, host, port)

	if err := db.EnsureRoles(ctx, bootstrap, pwd); err != nil {
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

// cleanupCap removes any leftover ordinary grant so re-runs start clean. Never
// touches the seeded '*' grant.
func cleanupCap(t *testing.T, pool *pgxpool.Pool, capability string) {
	t.Helper()
	if err := db.WithIdentityTxRaw(context.Background(), pool, localID, func(tx pgx.Tx) error {
		_, e := tx.Exec(context.Background(),
			"DELETE FROM aura.capability_grants WHERE identity_id = $1 AND capability = $2", localID, capability)
		return e
	}); err != nil {
		t.Logf("cleanup grant %q (best-effort): %v", capability, err)
	}
}

// countGrants counts capability_grants rows for identityID inside an identity-scoped
// transaction. aura.capability_grants is fail-closed as of migration 0087, so a count on
// the bare pool returns 0 whether or not the row exists — which would make every
// assertion below pass or fail for the wrong reason. extra is an optional additional
// predicate ("AND capability = $2") with its bound argument.
func countGrants(t *testing.T, pool *pgxpool.Pool, identityID, extra string, args ...any) int {
	t.Helper()
	ctx := context.Background()
	var n int
	q := "SELECT count(*) FROM aura.capability_grants WHERE identity_id = $1 " + extra
	if err := db.WithIdentityTxRaw(ctx, pool, identityID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, q, append([]any{identityID}, args...)...).Scan(&n)
	}); err != nil {
		t.Fatalf("count capability_grants: %v", err)
	}
	return n
}

// TestSeed_LocalIdentityExplicitCapabilities asserts the migrated (RBAC-01, 0121) state:
// `local` holds no '*' row and holds exactly the six declared capabilities (identity.All()).
// Pre-0121 this test asserted a single seeded '*' row instead; renamed and rewritten
// because that assertion is now false — the wildcard is retired.
func TestSeed_LocalIdentityExplicitCapabilities(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	// Exactly one (local, system) identity with the fixed UUID.
	got, err := s.GetIdentityByName(ctx, "local")
	if err != nil {
		t.Fatalf("GetIdentityByName(local): %v", err)
	}
	if got.ID != localID || got.Kind != "system" {
		t.Errorf("seeded identity: want id=%s kind=system, got id=%s kind=%s", localID, got.ID, got.Kind)
	}

	// No wildcard row survives 0121.
	wildcardCount := countGrants(t, pool, localID, "AND capability = '*'")
	if wildcardCount != 0 {
		t.Errorf("seeded (...001, '*') grant: want 0 rows after 0121, got %d", wildcardCount)
	}

	// Exactly the declared six, no more, no fewer.
	explicitCount := countGrants(t, pool, localID, "")
	if explicitCount != len(All()) {
		t.Errorf("local explicit grant count: want %d (All()), got %d", len(All()), explicitCount)
	}
}

func TestHasCapability_LocalExactMatchNoWildcard(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	// Post-0121, `local` holds the six declared names and nothing else — a name
	// outside that set is refused, never admitted by a wildcard that no longer exists.
	for _, cap := range All() {
		ok, err := s.HasCapability(ctx, localID, cap)
		if err != nil {
			t.Fatalf("HasCapability(%q): %v", cap, err)
		}
		if !ok {
			t.Errorf("HasCapability(local, %q): want true (declared, explicitly granted), got false", cap)
		}
	}
	for _, cap := range []string{"any_tool", "memory.read", "web.fetch"} {
		ok, err := s.HasCapability(ctx, localID, cap)
		if err != nil {
			t.Fatalf("HasCapability(%q): %v", cap, err)
		}
		if ok {
			t.Errorf("HasCapability(local, %q): want false (no wildcard expansion), got true", cap)
		}
	}
}

func TestHasCapability_ExactMatchWithoutWildcard(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := New(pool)

	// Create a NON-seeded identity that has no '*' grant so exact-match is
	// observable in isolation from the seeded local/'*' row.
	const name = "test_exact_match"
	if _, err := pool.Exec(ctx,
		"INSERT INTO aura.identities (id, name, kind) VALUES (gen_random_uuid(), $1, 'service') ON CONFLICT (name) DO NOTHING", name,
	); err != nil {
		t.Fatalf("insert test identity: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE name = $1", name)
	})
	got, err := s.GetIdentityByName(ctx, name)
	if err != nil {
		t.Fatalf("GetIdentityByName: %v", err)
	}

	// No grants yet → false.
	ok, err := s.HasCapability(ctx, got.ID, "foo")
	if err != nil {
		t.Fatalf("HasCapability(foo) pre-grant: %v", err)
	}
	if ok {
		t.Error("HasCapability(foo): want false before grant, got true")
	}

	// Grant exact → only that name matches.
	if err := s.GrantCapability(ctx, got.ID, "foo"); err != nil {
		t.Fatalf("GrantCapability(foo): %v", err)
	}
	if ok, err := s.HasCapability(ctx, got.ID, "foo"); err != nil || !ok {
		t.Errorf("HasCapability(foo) post-grant: want (true,nil), got (%v,%v)", ok, err)
	}
	if ok, err := s.HasCapability(ctx, got.ID, "bar"); err != nil || ok {
		t.Errorf("HasCapability(bar): want (false,nil), got (%v,%v)", ok, err)
	}
}

func TestGrantRevoke_Idempotent(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := New(pool)
	const cap = "idempotency.test"
	cleanupCap(t, pool, cap)
	t.Cleanup(func() { cleanupCap(t, pool, cap) })

	// Grant twice — second is a no-op, never an error; exactly one row.
	if err := s.GrantCapability(ctx, localID, cap); err != nil {
		t.Fatalf("first GrantCapability: %v", err)
	}
	if err := s.GrantCapability(ctx, localID, cap); err != nil {
		t.Fatalf("second GrantCapability (idempotent): %v", err)
	}
	n := countGrants(t, pool, localID, "AND capability = $2", cap)
	if n != 1 {
		t.Errorf("after two grants: want exactly 1 row, got %d", n)
	}

	// Revoke twice — second (absent) is a no-op, never an error.
	if err := s.RevokeCapability(ctx, localID, cap); err != nil {
		t.Fatalf("first RevokeCapability: %v", err)
	}
	if err := s.RevokeCapability(ctx, localID, cap); err != nil {
		t.Fatalf("second RevokeCapability (idempotent): %v", err)
	}
	n = countGrants(t, pool, localID, "AND capability = $2", cap)
	if n != 0 {
		t.Errorf("after revoke: want 0 rows, got %d", n)
	}
}

func TestGrantRevoke_WildcardRejected(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	if err := s.GrantCapability(ctx, localID, "*"); !errors.Is(err, ErrWildcardManaged) {
		t.Errorf("GrantCapability('*'): want ErrWildcardManaged, got %v", err)
	}
	if err := s.RevokeCapability(ctx, localID, "*"); !errors.Is(err, ErrWildcardManaged) {
		t.Errorf("RevokeCapability('*'): want ErrWildcardManaged, got %v", err)
	}

	// The rejection happens before any DB call, so it must not create a wildcard row —
	// post-0121 `local` never holds one at all.
	n := countGrants(t, pool, localID, "AND capability = '*'")
	if n != 0 {
		t.Errorf("'*' grant: want 0 rows after rejected grant/revoke, got %d", n)
	}
}

func TestGrant_InvalidNameRejected(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	for _, bad := range []string{"", "Foo", "1foo", "foo bar", ".bad"} {
		if err := s.GrantCapability(ctx, localID, bad); !errors.Is(err, ErrInvalidCapability) {
			t.Errorf("GrantCapability(%q): want ErrInvalidCapability, got %v", bad, err)
		}
	}
}

func TestDeleteIdentity_CascadesGrants(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	s := New(pool)

	// A throwaway identity with its own grant — deleting it must cascade the grant.
	const name = "test_cascade_victim"
	var id string
	if err := pool.QueryRow(ctx,
		"INSERT INTO aura.identities (id, name, kind) VALUES (gen_random_uuid(), $1, 'service') "+
			"ON CONFLICT (name) DO UPDATE SET name = EXCLUDED.name RETURNING id::text", name,
	).Scan(&id); err != nil {
		t.Fatalf("insert cascade victim: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE name = $1", name)
	})

	if err := s.GrantCapability(ctx, id, "cascade.me"); err != nil {
		t.Fatalf("GrantCapability: %v", err)
	}
	pre := countGrants(t, pool, id, "")
	if pre == 0 {
		t.Fatal("setup: expected at least one grant before delete")
	}

	if err := s.DeleteIdentity(ctx, name); err != nil {
		t.Fatalf("DeleteIdentity: %v", err)
	}
	post := countGrants(t, pool, id, "")
	if post != 0 {
		t.Errorf("after DeleteIdentity: want 0 cascaded grants, got %d", post)
	}
}

func TestListIdentities_IncludesSeed(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	ids, err := s.ListIdentities(ctx)
	if err != nil {
		t.Fatalf("ListIdentities: %v", err)
	}
	var found bool
	for _, id := range ids {
		if id.Name == "local" && id.ID == localID {
			found = true
		}
	}
	if !found {
		t.Errorf("ListIdentities: seeded local/%s not present in %d rows", localID, len(ids))
	}
}

func TestGetIdentityByName_NotFound(t *testing.T) {
	pool := migratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	s := New(pool)

	if _, err := s.GetIdentityByName(ctx, "definitely_absent_identity"); !errors.Is(err, ErrIdentityNotFound) {
		t.Errorf("GetIdentityByName(absent): want ErrIdentityNotFound, got %v", err)
	}
}

// TestStoreMethods_DBErrorWrapping drives every Store method against a canceled
// context so the real DB call fails, exercising each method's "%w" error-wrap
// branch (the branches a happy-path test never reaches). Mirrors
// internal/db/db_test.go TestPing_QueryErrorOnCanceledContext.
func TestStoreMethods_DBErrorWrapping(t *testing.T) {
	pool := migratedPool(t)
	s := New(pool)

	canceled, cancel := context.WithCancel(context.Background())
	cancel() // every subsequent query fails with context.Canceled

	if _, err := s.ListIdentities(canceled); err == nil {
		t.Error("ListIdentities(canceled): want error, got nil")
	}
	if _, err := s.GetIdentityByName(canceled, "local"); err == nil {
		t.Error("GetIdentityByName(canceled): want error, got nil")
	}
	if err := s.DeleteIdentity(canceled, "nobody"); err == nil {
		t.Error("DeleteIdentity(canceled): want error, got nil")
	}
	if _, err := s.HasCapability(canceled, localID, "foo"); err == nil {
		t.Error("HasCapability(canceled): want error, got nil")
	}
	if err := s.GrantCapability(canceled, localID, "foo"); err == nil {
		t.Error("GrantCapability(canceled): want error, got nil")
	}
	if err := s.RevokeCapability(canceled, localID, "foo"); err == nil {
		t.Error("RevokeCapability(canceled): want error, got nil")
	}

	// A malformed identity UUID must fail in HasCapability's parse branch before
	// any DB call, with no context dependency.
	if _, err := s.HasCapability(context.Background(), "not-a-uuid", "foo"); err == nil {
		t.Error("HasCapability(bad uuid): want parse error, got nil")
	}
}
