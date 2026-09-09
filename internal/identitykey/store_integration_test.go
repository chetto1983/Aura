//go:build db_integration

// Live-Postgres test for the per-identity OpenRouter key store. It proves what only a
// live engine can prove: cross-identity RLS isolation and ON DELETE CASCADE, on the
// same disposable-database discipline every db_integration test in this repo follows
// (never the live `aura` DB).
//
// Bring the stack up and run:
//
//	go test -tags db_integration -race -run TestIdentityLLMKeyRLSAndCascade ./internal/identitykey/
//
// A skipped tier must never pass as green (t.Fatal under $CI, per CLAUDE.md).
package identitykey

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/identityctx"
)

func identitykeyEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("identitykey integration requires %s under CI — a skipped tier must not pass as green", key)
		}
		t.Skipf("identitykey integration requires %s", key)
	}
	return v
}

func migratedKeyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := identitykeyEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, identitykeyEnvOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := identitykeyEnvOrSkip(t, "AURA_DB_URL")
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
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func seedKeyIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := uuid.NewString()
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user') ON CONFLICT DO NOTHING`,
		id, "identitykey-test-"+id[:8],
	); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	t.Cleanup(func() {
		cctx, ccancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer ccancel()
		_, _ = pool.Exec(cctx, `DELETE FROM aura.identities WHERE id = $1`, id)
	})
	return id
}

func keyStore(t *testing.T, pool *pgxpool.Pool) *Store {
	t.Helper()
	s, err := NewStore(pool, identitykeyEnvOrSkip(t, "AURA_AUTHULA_SECRET"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

// TestIdentityLLMKeyRLSAndCascade proves the two claims only a live engine can prove:
// identity A cannot read identity B's row through db.WithIdentityTx, and DELETE FROM
// aura.identities for A removes A's key row while B's row is untouched.
func TestIdentityLLMKeyRLSAndCascade(t *testing.T) {
	pool := migratedKeyPool(t)
	store := keyStore(t, pool)
	alice := seedKeyIdentity(t, pool)
	bob := seedKeyIdentity(t, pool)
	aliceCtx := identityctx.WithIdentityID(context.Background(), alice)
	bobCtx := identityctx.WithIdentityID(context.Background(), bob)

	if err := store.Save(aliceCtx, Record{
		Key: "sk-or-v1-alice-" + uuid.NewString(), Hash: "hash-alice", Label: "sk-or-v1-ali...ce1", LimitUSD: 10, LimitReset: "monthly",
	}); err != nil {
		t.Fatalf("Save as alice: %v", err)
	}
	if err := store.Save(bobCtx, Record{
		Key: "sk-or-v1-bob-" + uuid.NewString(), Hash: "hash-bob", Label: "sk-or-v1-bob...ob1", LimitUSD: 5, LimitReset: "monthly",
	}); err != nil {
		t.Fatalf("Save as bob: %v", err)
	}

	// Cross-identity isolation: bob's Load must never see alice's row.
	if _, err := store.Load(bobCtx); err != nil {
		t.Fatalf("bob Load (own key): %v", err)
	}
	gotBob, err := store.Load(bobCtx)
	if err != nil {
		t.Fatalf("bob Load: %v", err)
	}
	if gotBob.Hash != "hash-bob" {
		t.Fatalf("bob's own key hash = %q, want hash-bob (bob must never see alice's row)", gotBob.Hash)
	}
	if list, err := store.List(bobCtx); err != nil || len(list) != 1 || list[0].Hash != "hash-bob" {
		t.Fatalf("bob's list = %+v (err=%v), want exactly [hash-bob]", list, err)
	}

	gotAlice, err := store.Load(aliceCtx)
	if err != nil {
		t.Fatalf("alice Load: %v", err)
	}
	if gotAlice.Hash != "hash-alice" || gotAlice.Key == gotBob.Key {
		t.Fatalf("alice's own key = %+v, must not equal or leak bob's (%+v)", gotAlice, gotBob)
	}

	// Cascade: deleting alice's identity removes her key row; bob's survives.
	if _, err := pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id = $1`, alice); err != nil {
		t.Fatalf("delete alice identity: %v", err)
	}
	if _, err := store.Load(aliceCtx); !errors.Is(err, ErrNoKey) {
		t.Fatalf("alice's key after identity delete: err = %v, want ErrNoKey (cascade)", err)
	}
	if _, err := store.Load(bobCtx); err != nil {
		t.Fatalf("bob's key after alice's identity delete: %v (must survive)", err)
	}
}
