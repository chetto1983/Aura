//go:build db_integration

// Live proof (MCPH-07, task 3) that a real CLI MCP mutation — routed through
// runMCPCommand with a live *pgxpool.Pool, exactly like main.go's runMCPDispatch wires
// it — appends exactly one aura.mcp_audit row per mutation via manager.WriteConfigWithAudit,
// with a cli:<os-username> actor and (for trust) a non-null reason. A second identical
// trust mutation appends a SECOND distinct row (append-only, migration 0022's triggers
// forbid UPDATE/DELETE) while the config write itself is idempotent (same effective
// trust class + reason).
//
// No-skip-as-green: mcpAuditEnvOrSkip t.Fatal's under $CI when the composed DSN env is
// unset, so a missing DB can never report this tier as falsely green.
//
// Run via:
//
//	go test -tags db_integration ./cmd/aura -run TestMCPCLIAudit -race -count=1
package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
)

// mcpAuditEnvOrSkip mirrors recovery_integration_test.go's recoveryEnvOrSkip / the
// internal/mcp/manager audit_store_integration_test.go envOrSkip: skip locally, t.Fatal
// under $CI so a missing DSN never reports this tier as falsely green.
func mcpAuditEnvOrSkip(t *testing.T, key string) string {
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

// mcpAuditMigratedPool ensures roles + migrations (to head) are applied against the
// composed AURA_DB_URL/AURA_DB_MIGRATE_URL — NEVER the live `aura` DB in CI (the
// disposable-DB discipline every db_integration test in this repo follows) — then
// returns an aura_app pool ready for the CLI mutation under test. Closed via t.Cleanup.
func mcpAuditMigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	pwd := mcpAuditEnvOrSkip(t, "POSTGRES_PASSWORD")
	migrateURL := mcpAuditEnvOrSkip(t, "AURA_DB_MIGRATE_URL")
	appURL := mcpAuditEnvOrSkip(t, "AURA_DB_URL")

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

// TestMCPCLIAuditTrustAppendsOneRow is the db_integration audit-row proof (MCPH-07):
// a CLI `add` then `trust --reason` mutation, invoked through runMCPCommand with a live
// pool exactly as main.go's runMCPDispatch wires it, appends exactly one mcp_audit row
// per mutation with a cli:-prefixed actor and the trust row's non-null reason. A repeat
// identical trust appends a SECOND distinct row (append-only) while leaving the config's
// effective trust state unchanged (idempotent write).
func TestMCPCLIAuditTrustAppendsOneRow(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pool := mcpAuditMigratedPool(t)
	path := withTempMCPConfig(t)
	store := mcpmanager.NewMCPAuditStore(pool)

	name := "audit-" + uuid.Must(uuid.NewV7()).String()

	var out bytes.Buffer
	if err := runMCPCommand(ctx, pool, []string{"add", name, "--", "node", "server.js"}, &out); err != nil {
		t.Fatalf("mcp add: %v", err)
	}
	if rows, err := store.List(ctx, mcpmanager.MCPAuditFilter{ServerName: name}); err != nil {
		t.Fatalf("List after add: %v", err)
	} else if len(rows) != 1 {
		t.Fatalf("want exactly 1 add audit row for %s, got %d", name, len(rows))
	} else if !strings.HasPrefix(rows[0].ActorIdentityID, "cli:") {
		t.Fatalf("add row actor_identity_id = %q, want a cli: prefix", rows[0].ActorIdentityID)
	}

	out.Reset()
	const reason = "operator vetted the publisher"
	if err := runMCPCommand(ctx, pool, []string{"trust", name, "--reason", reason}, &out); err != nil {
		t.Fatalf("mcp trust: %v", err)
	}

	trustRows, err := store.List(ctx, mcpmanager.MCPAuditFilter{ServerName: name})
	if err != nil {
		t.Fatalf("List after trust: %v", err)
	}
	// add + trust = 2 rows total for this server; exactly one of them is the trust action.
	var firstTrust mcpmanager.MCPAuditRow
	trustCount := 0
	for _, r := range trustRows {
		if r.Action == "trust" {
			trustCount++
			firstTrust = r
		}
	}
	if trustCount != 1 {
		t.Fatalf("want exactly 1 trust audit row for %s, got %d (rows=%+v)", name, trustCount, trustRows)
	}
	if !strings.HasPrefix(firstTrust.ActorIdentityID, "cli:") {
		t.Fatalf("trust row actor_identity_id = %q, want a cli: prefix", firstTrust.ActorIdentityID)
	}
	if firstTrust.Reason != reason {
		t.Fatalf("trust row reason = %q, want %q (a CLI trust audit row must never be NULL/empty)", firstTrust.Reason, reason)
	}

	// Repeat-mutation / idempotency probe (#14, MCPH-07): a second identical trust
	// appends a SECOND distinct audit row (append-only — migration 0022's triggers
	// forbid UPDATE/DELETE), while the config write is idempotent: the effective trust
	// class + reason are unchanged.
	out.Reset()
	if err := runMCPCommand(ctx, pool, []string{"trust", name, "--reason", reason}, &out); err != nil {
		t.Fatalf("mcp trust (repeat): %v", err)
	}
	afterRepeat, err := store.List(ctx, mcpmanager.MCPAuditFilter{ServerName: name})
	if err != nil {
		t.Fatalf("List after repeat trust: %v", err)
	}
	repeatTrustCount := 0
	seenIDs := map[string]bool{firstTrust.ID: true}
	distinctSecond := false
	for _, r := range afterRepeat {
		if r.Action != "trust" {
			continue
		}
		repeatTrustCount++
		if !seenIDs[r.ID] {
			distinctSecond = true
			seenIDs[r.ID] = true
		}
	}
	if repeatTrustCount != 2 {
		t.Fatalf("want exactly 2 trust audit rows after a repeat mutation (append-only), got %d", repeatTrustCount)
	}
	if !distinctSecond {
		t.Fatal("repeat mutation must append a DISTINCT second row, not reuse the first")
	}

	doc, err := mcp.LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("load config after repeat trust: %v", err)
	}
	server, ok := doc.MCPServers[name]
	if !ok {
		t.Fatalf("server %q missing from config after repeat trust", name)
	}
	if server.Trust.Class != mcp.TrustTrustedLocal {
		t.Fatalf("trust class after repeat = %q, want %q (idempotent effective config)", server.Trust.Class, mcp.TrustTrustedLocal)
	}
	if server.Trust.Reason != reason {
		t.Fatalf("trust reason after repeat = %q, want %q (idempotent effective config)", server.Trust.Reason, reason)
	}
}
