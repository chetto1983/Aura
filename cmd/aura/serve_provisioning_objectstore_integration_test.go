//go:build db_integration

// Live-Postgres proof (Amendment #88 Task 3) that EnsureForIdentity's Resolve/Put round-trip
// runs against the REAL aura.identity_object_store table via the real *objectstore.IdentityStore
// (not a fake resolver) — a real non-local identity provisions exactly once and re-resolves to
// its own aura-<id> bucket on a second call, and the local/shared identity resolves to the
// shared bucket with NO row written. The Garage admin side is a fakeMinter (from
// serve_provisioning_objectstore_test.go, same package) since dialing a live Garage Admin API
// is out of scope here — the point of this tier is the real DB write/read, not a real Garage.
//
// No-skip-as-green: objStoreProvEnvOrSkip t.Fatal's under $CI when the composed DSN env is
// unset, so a missing DB can never report this tier as falsely green.
//
// Run via:
//
//	go test -tags db_integration ./cmd/aura -run TestEnsureForIdentityDBIntegration -race -count=1
package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/objectstore"
)

// objStoreProvEnvOrSkip mirrors mcpAuditEnvOrSkip / the internal/objectstore
// identity_store_integration_test.go objEnvOrSkip: skip locally, t.Fatal under $CI so a
// missing DSN never reports this tier as falsely green.
func objStoreProvEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("EnsureForIdentity db_integration test requires %s, but it is unset under CI", key)
		}
		t.Skipf("EnsureForIdentity db_integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// objStoreProvMigratedPool ensures roles + migrations (to head) are applied against the
// composed AURA_DB_URL/AURA_DB_MIGRATE_URL, then returns an aura_app pool.
func objStoreProvMigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := objStoreProvEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := objStoreProvEnvOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := objStoreProvEnvOrSkip(t, "AURA_DB_URL")

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

// objStoreProvSeedIdentity inserts a non-local identity (FK target for
// identity_object_store) and registers cascading cleanup.
func objStoreProvSeedIdentity(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	id := uuid.NewString()
	name := "ensure-for-identity-" + id[:8]
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

// TestEnsureForIdentityDBIntegrationProvisionsRealRow proves a real non-local identity
// provisions exactly once (mints via the fakeMinter, persists via the REAL IdentityStore) and
// a second EnsureForIdentity call re-resolves the SAME row from Postgres without minting
// again.
func TestEnsureForIdentityDBIntegrationProvisionsRealRow(t *testing.T) {
	pool := objStoreProvMigratedPool(t)
	secret := objStoreProvEnvOrSkip(t, "AURA_AUTHULA_SECRET")
	const localID = "00000000-0000-0000-0000-000000000001"
	shared := objectstore.Credentials{Bucket: "aura-assets", AccessKey: "GKshared", SecretKey: "sharedsecret"}
	store, err := objectstore.NewIdentityStore(pool, secret, shared, localID)
	if err != nil {
		t.Fatalf("NewIdentityStore: %v", err)
	}

	id := objStoreProvSeedIdentity(t, pool)
	minter := &fakeMinter{
		bucketID:  "internal-bucket-" + id,
		accessKey: "AK-" + id,
		secretKey: "SK-" + id,
	}
	adapter := newObjectStoreProvisionAdapter(minter, store)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	got1, err := adapter.EnsureForIdentity(ctx, id)
	if err != nil {
		t.Fatalf("first EnsureForIdentity: %v", err)
	}
	wantBucket := "aura-" + id
	if got1.Bucket != wantBucket || got1.AccessKey != minter.accessKey || got1.SecretKey != minter.secretKey {
		t.Fatalf("first EnsureForIdentity = %+v, want bucket %q AK/SK %q/%q", got1, wantBucket, minter.accessKey, minter.secretKey)
	}
	if minter.createKeyCalls != 1 {
		t.Fatalf("first EnsureForIdentity: CreateKey called %d times, want 1", minter.createKeyCalls)
	}

	// Confirm the row actually landed in Postgres (not just an in-memory fake). The
	// read-back is IDENTITY-SCOPED: aura.identity_object_store fails closed under RLS
	// since 0087, and an unscoped SELECT returns zero rows rather than an error — which
	// reads exactly like "EnsureForIdentity never wrote anything".
	var dbBucket, dbAccessKey string
	if err := db.WithIdentityTxRaw(ctx, pool, id, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx,
			`SELECT bucket, access_key FROM aura.identity_object_store WHERE identity_id = $1`, id,
		).Scan(&dbBucket, &dbAccessKey)
	}); err != nil {
		t.Fatalf("read persisted row: %v", err)
	}
	if dbBucket != wantBucket || dbAccessKey != minter.accessKey {
		t.Fatalf("persisted row = bucket %q access_key %q, want %q / %q", dbBucket, dbAccessKey, wantBucket, minter.accessKey)
	}

	// A second call re-resolves the SAME persisted row from Postgres without re-minting.
	got2, err := adapter.EnsureForIdentity(ctx, id)
	if err != nil {
		t.Fatalf("second EnsureForIdentity: %v", err)
	}
	if got2 != got1 {
		t.Fatalf("second EnsureForIdentity = %+v, want unchanged %+v", got2, got1)
	}
	if minter.createKeyCalls != 1 {
		t.Fatalf("second EnsureForIdentity: CreateKey called %d times, want still 1 (no re-mint)", minter.createKeyCalls)
	}
	if minter.createBucketCalls != 1 {
		t.Fatalf("second EnsureForIdentity: CreateBucket called %d times, want still 1 (no re-mint)", minter.createBucketCalls)
	}
}

// TestEnsureForIdentityDBIntegrationSharedWritesNoRow proves the local/shared principal
// (D-11) resolves to the shared bucket via the REAL IdentityStore's fast path, WITHOUT
// writing any row to aura.identity_object_store and without ever touching the minter.
func TestEnsureForIdentityDBIntegrationSharedWritesNoRow(t *testing.T) {
	pool := objStoreProvMigratedPool(t)
	secret := objStoreProvEnvOrSkip(t, "AURA_AUTHULA_SECRET")
	const localID = "00000000-0000-0000-0000-000000000001"
	shared := objectstore.Credentials{Bucket: "aura-assets", AccessKey: "GKshared", SecretKey: "sharedsecret"}
	store, err := objectstore.NewIdentityStore(pool, secret, shared, localID)
	if err != nil {
		t.Fatalf("NewIdentityStore: %v", err)
	}
	minter := &fakeMinter{bucketID: "must-not-be-used", accessKey: "must-not", secretKey: "must-not"}
	adapter := newObjectStoreProvisionAdapter(minter, store)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	got, err := adapter.EnsureForIdentity(ctx, localID)
	if err != nil {
		t.Fatalf("EnsureForIdentity(shared): %v", err)
	}
	if got != shared {
		t.Fatalf("EnsureForIdentity(shared) = %+v, want %+v", got, shared)
	}
	if minter.createBucketCalls != 0 || minter.createKeyCalls != 0 {
		t.Fatalf("EnsureForIdentity(shared): minted anyway (CreateBucket=%d CreateKey=%d), want 0/0",
			minter.createBucketCalls, minter.createKeyCalls)
	}

	var count int
	if err := pool.QueryRow(ctx,
		`SELECT count(*) FROM aura.identity_object_store WHERE identity_id = $1`, localID,
	).Scan(&count); err != nil {
		t.Fatalf("count rows for local identity: %v", err)
	}
	if count != 0 {
		t.Fatalf("aura.identity_object_store has %d row(s) for the shared/local identity, want 0", count)
	}
}
