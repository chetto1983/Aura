//go:build db_integration

package manager

// The read half of the MCP audit store is test support (audit_store_integration.go), so its
// unit tests build with it. They still need no database: fakeDBTX lives in audit_test.go.

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

// TestMCPAuditStoreListClampAndProject drives List + mcpAuditFromRow through a fake
// DBTX: the default-limit fallback (0 -> 100) and the filter binding, plus the
// NULL-vs-non-NULL projection of the reason column and newest-first row order.
func TestMCPAuditStoreListClampAndProject(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	id1, id2 := uuid.New(), uuid.New()
	now := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)

	rows := [][]any{
		mcpAuditRowValues(id1, now, "local", "trust", "calc", "vetted"),
		mcpAuditRowValues(id2, now.Add(-time.Hour), "local", "install", "calc", ""),
	}
	f := &fakeDBTX{queryRows: &fakeRows{rows: rows}}
	s := &MCPAuditStore{q: sqlc.New(f)}

	since := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	out, err := s.List(ctx, MCPAuditFilter{ServerName: "calc", Since: since}) // Limit 0 -> default 100
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("List len = %d, want 2", len(out))
	}
	// Default limit (100) bound as $1; name + since bound (never interpolated).
	if len(f.queryArgs) != 3 {
		t.Fatalf("List bound %d args, want 3", len(f.queryArgs))
	}
	if lim, ok := f.queryArgs[0].(int32); !ok || lim != 100 {
		t.Fatalf("limit arg = %v, want int32(100)", f.queryArgs[0])
	}
	if name, ok := f.queryArgs[1].(pgtype.Text); !ok || !name.Valid || name.String != "calc" {
		t.Fatalf("name arg = %v, want valid pgtype.Text 'calc'", f.queryArgs[1])
	}
	if ts, ok := f.queryArgs[2].(pgtype.Timestamptz); !ok || !ts.Valid || !ts.Time.Equal(since) {
		t.Fatalf("since arg = %v, want bound timestamptz %s", f.queryArgs[2], since)
	}

	// Row 0: non-NULL reason projects onto a plain string.
	if out[0].Reason != "vetted" || out[0].Action != "trust" || out[0].ID != id1.String() {
		t.Fatalf("row0 projection mismatch: %+v", out[0])
	}
	// Row 1: NULL reason projects to the empty string.
	if out[1].Reason != "" || out[1].Action != "install" {
		t.Fatalf("row1 NULL reason must project to empty string, got %+v", out[1])
	}
}

// TestMCPAuditStoreListClampMax pins the upper clamp: a limit beyond MaxInt32 clamps
// to MaxInt32 (the int32 bind cannot overflow).
func TestMCPAuditStoreListClampMax(t *testing.T) {
	t.Parallel()
	f := &fakeDBTX{queryRows: &fakeRows{rows: [][]any{}}}
	s := &MCPAuditStore{q: sqlc.New(f)}
	if _, err := s.List(context.Background(), MCPAuditFilter{Limit: 1 << 40}); err != nil {
		t.Fatalf("List: %v", err)
	}
	if lim, ok := f.queryArgs[0].(int32); !ok || lim != 1<<31-1 {
		t.Fatalf("limit clamp = %v, want int32 MaxInt32", f.queryArgs[0])
	}
}

// TestMCPAuditStoreListErrors drives the query-error and rows.Err paths so List
// surfaces the wrapped failure instead of a partial slice.
func TestMCPAuditStoreListErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	qErr := errors.New("connection reset")
	s := &MCPAuditStore{q: sqlc.New(&fakeDBTX{queryErr: qErr})}
	if _, err := s.List(ctx, MCPAuditFilter{Limit: 5}); !errors.Is(err, qErr) {
		t.Fatalf("List query error = %v, want wrapped connection reset", err)
	}

	rowsErr := errors.New("rows iteration failed")
	s = &MCPAuditStore{q: sqlc.New(&fakeDBTX{queryRows: &fakeRows{rows: [][]any{}, rowsErr: rowsErr}})}
	if _, err := s.List(ctx, MCPAuditFilter{Limit: 5}); !errors.Is(err, rowsErr) {
		t.Fatalf("List rows.Err = %v, want wrapped iteration failure", err)
	}
}
