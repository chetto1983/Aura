//go:build db_integration

// Integration tests for the atomic temp→tx→rename MCP config-write wrapper
// (WriteConfigWithAudit) against a live Postgres. Proves the D-04 all-or-nothing
// contract and the MCPW-01 concurrency edge: a successful write flips the config AND
// appends exactly one audit row, while an induced tx failure leaves the prior
// servers.json byte-identical with no audit row.
//
// Run via:
//
//	go test -tags db_integration ./internal/mcp/manager -run 'TestWriteConfigWithAuditAtomic|TestMCPAuditOnePerAction' -count=1
//
// migratedPool + envOrSkip are shared with audit_store_integration_test.go (same
// package, same build tag): no-skip-as-green still governs.
package manager

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
	"github.com/google/uuid"
)

// TestWriteConfigWithAuditAtomic proves the D-04 / MCPW-01 atomicity: an induced tx
// failure (a pre-cancelled context fails pool.Begin) leaves the prior servers.json
// byte-identical and writes NO audit row, while a clean write flips the config AND
// appends exactly one row.
func TestWriteConfigWithAuditAtomic(t *testing.T) {
	pool := migratedPool(t)
	store := NewMCPAuditStore(pool)
	ctx := context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "servers.json")
	if err := mcp.SaveManagedConfig(path, validDoc("alpha", "alpha-bin")); err != nil {
		t.Fatalf("seed prior config: %v", err)
	}
	priorBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read prior: %v", err)
	}

	server := "atomic-" + uuid.Must(uuid.NewV7()).String()[:8]

	// Induced tx failure: a pre-cancelled context fails pool.Begin inside db.WithTx, so
	// the audit INSERT never runs. The wrapper must discard the temp and leave the prior
	// config intact.
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	next := validDoc("beta", "beta-bin")
	if err := WriteConfigWithAudit(cancelled, pool, path, next, MCPAuditInsert{
		ActorIdentityID: "local", Action: "install", ServerName: server,
	}); err == nil {
		t.Fatal("WriteConfigWithAudit with a cancelled ctx must fail")
	}

	// Prior config byte-identical (no applied-but-unaudited write).
	afterBytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after failed write: %v", err)
	}
	if string(afterBytes) != string(priorBytes) {
		t.Fatal("an induced tx failure must leave the prior servers.json byte-identical")
	}
	// No temp turd left behind.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Errorf("a failed write must leave only the live config, found %d entries", len(entries))
	}
	// No audit row for this server.
	if rows, err := store.List(ctx, MCPAuditFilter{ServerName: server}); err != nil {
		t.Fatalf("List after failed write: %v", err)
	} else if len(rows) != 0 {
		t.Fatalf("a failed write must write no audit row, found %d", len(rows))
	}

	// A clean write flips the config AND appends exactly one row.
	if err := WriteConfigWithAudit(ctx, pool, path, next, MCPAuditInsert{
		ActorIdentityID: "local", Action: "install", ServerName: server,
	}); err != nil {
		t.Fatalf("WriteConfigWithAudit (clean): %v", err)
	}
	loaded, err := mcp.LoadManagedConfig(path)
	if err != nil {
		t.Fatalf("load after clean write: %v", err)
	}
	if _, ok := loaded.MCPServers["beta"]; !ok {
		t.Errorf("clean write did not flip the config: %+v", loaded.MCPServers)
	}
	if _, ok := loaded.MCPServers["alpha"]; ok {
		t.Error("clean write must replace the whole doc; alpha should be gone")
	}
	if rows, err := store.List(ctx, MCPAuditFilter{ServerName: server}); err != nil {
		t.Fatalf("List after clean write: %v", err)
	} else if len(rows) != 1 {
		t.Fatalf("a clean write must append exactly one audit row, found %d", len(rows))
	}
}

// TestMCPAuditOnePerAction asserts the one-named-action = one-audit-row contract across
// the six write verbs: each WriteConfigWithAudit appends exactly one row, and the six
// distinct actions all round-trip newest-first.
func TestMCPAuditOnePerAction(t *testing.T) {
	pool := migratedPool(t)
	store := NewMCPAuditStore(pool)
	ctx := context.Background()

	dir := t.TempDir()
	path := filepath.Join(dir, "servers.json")
	server := "peraction-" + uuid.Must(uuid.NewV7()).String()[:8]

	actions := []string{"install", "edit", "enable", "disable", "trust", "remove"}
	for _, action := range actions {
		in := MCPAuditInsert{ActorIdentityID: "local", Action: action, ServerName: server}
		if action == "trust" {
			in.Reason = "operator vetted the publisher"
		}
		if err := WriteConfigWithAudit(ctx, pool, path, validDoc(server, "bin"), in); err != nil {
			t.Fatalf("WriteConfigWithAudit(%s): %v", action, err)
		}
	}

	rows, err := store.List(ctx, MCPAuditFilter{ServerName: server})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(rows) != len(actions) {
		t.Fatalf("want exactly %d audit rows (one per action), got %d", len(actions), len(rows))
	}
	// Newest-first: the last action (`remove`) leads.
	if rows[0].Action != "remove" {
		t.Errorf("newest-first order: rows[0].Action=%q want remove", rows[0].Action)
	}
	// The `trust` row carries its reason; the others are NULL → "".
	for _, r := range rows {
		if r.Action == "trust" && r.Reason == "" {
			t.Error("trust row must carry a non-empty reason")
		}
		if r.Action != "trust" && r.Reason != "" {
			t.Errorf("%s row must have an empty reason, got %q", r.Action, r.Reason)
		}
	}
}
