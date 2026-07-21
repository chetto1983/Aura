package db

import (
	"os"
	"strings"
	"testing"
)

func TestIdempotencyOperationsMigrationContract(t *testing.T) {
	t.Parallel()

	up := compactSQL(t, "migrations/0043_idempotency_operations.up.sql")
	down := compactSQL(t, "migrations/0043_idempotency_operations.down.sql")

	wants := []string{
		"create table aura.idempotency_operations",
		"primary key (identity_id, operation_scope, operation_key)",
		"check (octet_length(payload_hash) = 32)",
		"check (state in ('in_progress', 'completed', 'indeterminate'))",
		"references aura.identities (id)",
		"create index idempotency_operations_replay_expiry_idx",
		"create index idempotency_operations_stale_in_progress_idx",
		"grant select, insert, update on aura.idempotency_operations to aura_app",
	}
	for _, want := range wants {
		if !strings.Contains(up, want) {
			t.Errorf("up migration missing %q", want)
		}
	}
	if strings.Contains(up, "alter table aura.tool_invocations") || strings.Contains(up, "delete from aura.tool_invocations") {
		t.Error("migration must not weaken or delete the append-only tool_invocations ledger")
	}
	if !strings.Contains(down, "drop table aura.idempotency_operations") {
		t.Error("down migration does not drop aura.idempotency_operations")
	}
}

func TestIdempotencyOperationsQueryContract(t *testing.T) {
	t.Parallel()

	queries := compactSQL(t, "queries/idempotency_operations.sql")
	for _, name := range []string{
		"-- name: trystartoperation :execrows",
		"-- name: getoperation :one",
		"-- name: completeoperation :execrows",
		"-- name: markoperationindeterminate :execrows",
		"-- name: listexpiredreplaybodies :many",
		"-- name: clearexpiredreplaybody :execrows",
	} {
		if !strings.Contains(queries, name) {
			t.Errorf("query file missing %q", name)
		}
	}

	assertSQLSectionContains(t, queries, "trystartoperation", "on conflict (identity_id, operation_scope, operation_key) do nothing")
	assertSQLSectionContains(t, queries, "completeoperation", "state = 'in_progress' and payload_hash =")
	assertSQLSectionContains(t, queries, "markoperationindeterminate", "state = 'in_progress' and payload_hash =")
	assertSQLSectionContains(t, queries, "listexpiredreplaybodies", "order by replay_expires_at asc, identity_id, operation_scope, operation_key limit")
	assertSQLSectionContains(t, queries, "clearexpiredreplaybody", "set replay_body = null, replay_preview = null, replay_sidecar_ref = null")

	clear := sqlSection(queries, "clearexpiredreplaybody")
	if strings.Contains(clear, "delete from") || strings.Contains(clear, "state = null") || strings.Contains(clear, "payload_hash = null") {
		t.Error("ClearExpiredReplayBody must preserve operation state, fingerprint, and row metadata")
	}
}

func compactSQL(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return strings.ToLower(strings.Join(strings.Fields(string(raw)), " "))
}

func assertSQLSectionContains(t *testing.T, queries, name, want string) {
	t.Helper()
	section := sqlSection(queries, name)
	if !strings.Contains(section, want) {
		t.Errorf("%s query missing %q", name, want)
	}
}

func sqlSection(queries, name string) string {
	start := strings.Index(queries, "-- name: "+name+" ")
	if start < 0 {
		return ""
	}
	rest := queries[start+len("-- name: "):]
	if next := strings.Index(rest, "-- name: "); next >= 0 {
		rest = rest[:next]
	}
	return rest
}
