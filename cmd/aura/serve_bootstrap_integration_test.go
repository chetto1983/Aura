//go:build db_integration

// Live proof (RBAC-01, RBAC-08) that createAuraFirstOperatorTx writes the declared
// six-capability set — never the retired '*' wildcard — and that the identity-audit
// row's GrantedCapabilities is byte-identical to identity.All()'s source order.
//
// No-skip-as-green: bootstrapEnvOrSkip t.Fatal's under $CI when the composed DSN env
// is unset, so a missing DB can never report this tier as falsely green.
//
// Run via:
//
//	go test -tags db_integration ./cmd/aura -run TestBootstrapGrantsExplicitSet -race -count=1
package main

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/dbtest"
	"github.com/chetto1983/aura/internal/identity"
)

// bootstrapEnvOrSkip mirrors every other db_integration tier in this package: skip
// locally, t.Fatal under $CI so a missing DSN never reports this tier as falsely green.
func bootstrapEnvOrSkip(t *testing.T, key string) string {
	t.Helper()
	v := os.Getenv(key)
	if v == "" {
		if os.Getenv("CI") != "" {
			t.Fatalf("integration test requires %s, but it is unset under CI", key)
		}
		t.Skipf("integration test requires %s; set it and re-run (e.g. via .env + make db-up)", key)
	}
	return v
}

// bootstrapMigratedPool ensures roles + migrations (to head) are applied against the
// composed AURA_DB_URL/AURA_DB_MIGRATE_URL — NEVER the live `aura` DB in CI (the
// disposable-DB discipline every db_integration test in this repo follows) — then
// returns an aura_app pool. Closed via t.Cleanup.
func bootstrapMigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := bootstrapEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := dbtest.MigrateURL(t, bootstrapEnvOrSkip(t, "AURA_DB_MIGRATE_URL"))
	appURL := bootstrapEnvOrSkip(t, "AURA_DB_URL")

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

// TestBootstrapGrantsExplicitSet drives createAuraFirstOperatorTx directly against a
// real transaction on a live, migrated database (extends serve_bootstrap_test.go's
// unit-tier coverage, which cannot exercise the fail-closed RLS scoping createAuraFirst-
// OperatorTx depends on). A fresh bootstrap must write exactly the six declared
// capability rows — none of them '*' — and the identity-audit row's GrantedCapabilities
// must be those same six names, in the same source order identity.All() returns.
func TestBootstrapGrantsExplicitSet(t *testing.T) {
	pool := bootstrapMigratedPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	identityName := "bootstrap-test-" + uuid.NewString() + "@example.test"
	authulaUserID := "authula-" + uuid.NewString()

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()

	newID, err := createAuraFirstOperatorTx(ctx, tx, identityName, authulaUserID, "q", "a")
	if err != nil {
		t.Fatalf("createAuraFirstOperatorTx: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}
	committed = true
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "DELETE FROM aura.identities WHERE id = $1::uuid", newID)
	})

	// Exactly the six declared rows, none of them '*'.
	//
	// Read inside a tx scoped to the new identity, because aura.capability_grants is
	// RLS-protected (migration 0087, USING identity_id = app.current_identity). A plain
	// pool.Query carries no setting, so the policy matches nothing and the rows the
	// bootstrap just wrote are invisible: measured 2026-09-10 as aura_app against the live
	// database, 0 rows unscoped and 6 scoped, for the same identity. That is the policy
	// working, and reading it as evidence about the WRITE is what this test used to do.
	readTx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin read tx: %v", err)
	}
	defer func() { _ = readTx.Rollback(ctx) }()
	if err := db.SetTxIdentity(ctx, readTx, newID); err != nil {
		t.Fatalf("scope read tx: %v", err)
	}
	rows, err := readTx.Query(ctx,
		"SELECT capability FROM aura.capability_grants WHERE identity_id = $1::uuid ORDER BY capability", newID)
	if err != nil {
		t.Fatalf("list grants: %v", err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("scan grant: %v", err)
		}
		got = append(got, c)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate grants: %v", err)
	}
	want := []string{
		"agent.run", "governance.read", "governance.write",
		"identity.create", "identity.delete", "share.public",
	}
	if len(got) != len(want) {
		t.Fatalf("bootstrap grants = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("bootstrap grants[%d] = %q, want %q", i, got[i], want[i])
		}
		if got[i] == identity.Wildcard {
			t.Errorf("bootstrap grants[%d] is the retired wildcard", i)
		}
	}

	// The identity-audit row's GrantedCapabilities is the same six names, same order.
	var auditCaps []string
	if err := pool.QueryRow(ctx,
		`SELECT granted_capabilities FROM aura.identity_audit WHERE new_identity_id = $1::uuid`,
		newID,
	).Scan(&auditCaps); err != nil {
		t.Fatalf("read identity_audit: %v", err)
	}
	if len(auditCaps) != len(identity.All()) {
		t.Fatalf("audit granted_capabilities = %v, want %v", auditCaps, identity.All())
	}
	for i, want := range identity.All() {
		if auditCaps[i] != want {
			t.Errorf("audit granted_capabilities[%d] = %q, want %q", i, auditCaps[i], want)
		}
	}
}
