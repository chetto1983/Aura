package toolindex

import (
	"context"
	"path/filepath"
	"testing"

	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/db/migrations"
)

func newStateStore(t *testing.T) *SQLiteStateStore {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "state.db")
	db, err := auradb.Open(dbPath)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return NewSQLiteStateStore(db)
}

func TestStateStore_EmptyLoadAll(t *testing.T) {
	s := newStateStore(t)
	rows, err := s.LoadAll(context.Background())
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	if rows == nil {
		t.Fatal("LoadAll returned nil map (callers expect non-nil empty map)")
	}
	if len(rows) != 0 {
		t.Fatalf("expected empty, got %d", len(rows))
	}
}

func TestStateStore_RoundTrip(t *testing.T) {
	s := newStateStore(t)
	ctx := context.Background()
	row := StateRow{
		ToolName:    "search_memory",
		ContentHash: "deadbeef",
		PointID:     "01abcdef-0000-0000-0000-000000000000",
		EmbedModel:  "embeddinggemma:256d",
		IndexedAt:   "2026-05-13T12:00:00Z",
	}
	if err := s.Upsert(ctx, row); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	loaded, err := s.LoadAll(ctx)
	if err != nil {
		t.Fatalf("LoadAll: %v", err)
	}
	got, ok := loaded["search_memory"]
	if !ok {
		t.Fatal("row missing after upsert")
	}
	if got != row {
		t.Fatalf("row mismatch:\n  got:  %+v\n  want: %+v", got, row)
	}
}

func TestStateStore_UpsertReplaces(t *testing.T) {
	s := newStateStore(t)
	ctx := context.Background()
	first := StateRow{ToolName: "doc", ContentHash: "h1", PointID: "p1", EmbedModel: "m", IndexedAt: "t1"}
	second := StateRow{ToolName: "doc", ContentHash: "h2", PointID: "p2", EmbedModel: "m", IndexedAt: "t2"}
	if err := s.Upsert(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(ctx, second); err != nil {
		t.Fatal(err)
	}
	loaded, _ := s.LoadAll(ctx)
	if len(loaded) != 1 {
		t.Fatalf("expected 1 row, got %d", len(loaded))
	}
	if loaded["doc"] != second {
		t.Fatalf("expected replacement, got %+v", loaded["doc"])
	}
}

func TestStateStore_Remove(t *testing.T) {
	s := newStateStore(t)
	ctx := context.Background()
	row := StateRow{ToolName: "x", ContentHash: "h", PointID: "p", EmbedModel: "m", IndexedAt: "t"}
	if err := s.Upsert(ctx, row); err != nil {
		t.Fatal(err)
	}
	if err := s.Remove(ctx, "x"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	loaded, _ := s.LoadAll(ctx)
	if _, ok := loaded["x"]; ok {
		t.Fatal("row should have been removed")
	}
}

func TestStateStore_RemoveMissingIsNoop(t *testing.T) {
	s := newStateStore(t)
	if err := s.Remove(context.Background(), "never-existed"); err != nil {
		t.Fatalf("Remove on missing should be idempotent, got: %v", err)
	}
}

func TestStateStore_RejectsEmptyName(t *testing.T) {
	s := newStateStore(t)
	ctx := context.Background()
	if err := s.Upsert(ctx, StateRow{ToolName: ""}); err == nil {
		t.Fatal("expected error on empty tool_name")
	}
	if err := s.Remove(ctx, ""); err == nil {
		t.Fatal("expected error on empty tool_name in Remove")
	}
}
