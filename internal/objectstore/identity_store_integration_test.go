//go:build db_integration

// Live-Postgres test for the per-identity credential resolver. It proves the
// encrypt-at-rest round-trip through the real aura.identity_object_store table, the
// fail-closed miss (an unprovisioned identity resolves to NOTHING, never the shared
// bucket), and idempotent Put/Delete. Bring the stack up and run:
//
//	go test -tags db_integration -run TestIdentityObjectStore ./internal/objectstore/
//
// A skipped tier must never pass as green (t.Fatal under $CI, per CLAUDE.md).
package objectstore

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func objEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("objectstore identity integration requires %s under CI — a skipped tier must not pass as green", key)
		}
		t.Skipf("objectstore identity integration requires %s", key)
	}
	return v
}

func migratedObjPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := objEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := objEnvOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := objEnvOrSkip(t, "AURA_DB_URL")
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

// seedIdentity inserts a non-local identity (FK target for identity_object_store) and
// registers cleanup that cascades its object-store row.
func seedIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := uuid.NewString()
	name := "objstore-test-" + id[:8]
	if _, err := pool.Exec(ctx,
		`INSERT INTO aura.identities (id, name, kind) VALUES ($1, $2, 'user') ON CONFLICT DO NOTHING`,
		id, name,
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

func TestIdentityObjectStoreRoundTrip(t *testing.T) {
	pool := migratedObjPool(t)
	secret := objEnvOrSkip(t, "AURA_AUTHULA_SECRET")
	store, err := NewIdentityStore(pool, secret,
		Credentials{Bucket: "aura-assets", AccessKey: "GKshared", SecretKey: "sharedsecret"},
		"00000000-0000-0000-0000-000000000001")
	if err != nil {
		t.Fatalf("NewIdentityStore: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	id := seedIdentity(t, pool)
	bucket := "aura-" + id
	want := Credentials{Bucket: bucket, AccessKey: "GK" + id[:10], SecretKey: "super-secret-" + id}

	// Persist (encrypt at rest) then resolve (decrypt) round-trips exactly.
	if err := store.Put(ctx, id, want.Bucket, want.AccessKey, want.SecretKey); err != nil {
		t.Fatalf("Put: %v", err)
	}
	idCtx := identityctx.WithIdentityID(ctx, id)
	got, err := store.Resolve(idCtx)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if got != want {
		t.Fatalf("Resolve = %+v, want %+v", got, want)
	}

	// The secret is stored as ciphertext, never plaintext. The read is identity-scoped for
	// the same reason the store's own is: since migration 0087 aura.identity_object_store is
	// fail-closed, so a bare app-pool SELECT is refused every row and this assertion would
	// read "no rows" rather than the ciphertext it exists to inspect.
	var enc []byte
	if err := db.WithIdentityTxRaw(ctx, pool, id, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT secret_key_enc FROM aura.identity_object_store WHERE identity_id = $1`, id,
		).Scan(&enc)
	}); err != nil {
		t.Fatalf("read ciphertext: %v", err)
	}
	if string(enc) == want.SecretKey {
		t.Error("secret_key_enc stored as plaintext — must be encrypted at rest")
	}

	// Put is idempotent (ON CONFLICT DO NOTHING): a re-run does not error or change state.
	if err := store.Put(ctx, id, want.Bucket, want.AccessKey, want.SecretKey); err != nil {
		t.Fatalf("Put (idempotent): %v", err)
	}
	if got, err := store.Resolve(idCtx); err != nil || got != want {
		t.Fatalf("Resolve after idempotent Put = %+v, %v", got, err)
	}

	// Fail closed: an unprovisioned identity resolves to NOTHING (pgx.ErrNoRows), never
	// the shared or a foreign bucket.
	absentCtx := identityctx.WithIdentityID(ctx, uuid.NewString())
	if _, err := store.Resolve(absentCtx); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("Resolve(absent) err = %v, want pgx.ErrNoRows (fail closed)", err)
	}

	// Delete, then resolve fails closed; Delete is idempotent.
	if err := store.Delete(ctx, id); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := store.Resolve(idCtx); !errors.Is(err, pgx.ErrNoRows) {
		t.Errorf("Resolve after Delete err = %v, want pgx.ErrNoRows", err)
	}
	if err := store.Delete(ctx, id); err != nil {
		t.Errorf("Delete (idempotent) = %v, want nil", err)
	}
}
