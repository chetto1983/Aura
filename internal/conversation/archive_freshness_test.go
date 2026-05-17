package conversation_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/storage/freshness"
	"github.com/aura/aura/internal/testutil"
)

func TestBufferedAppender_BumpsFreshnessPending(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	ctx := context.Background()

	fs := freshness.NewStore(db)
	_, err := fs.Upsert(ctx, freshness.Row{
		ProjectionID:  "compact_memory_archive",
		Kind:          "sqlite",
		Status:        "fresh",
		SchemaVersion: 1,
		Version:       1,
	})
	if err != nil {
		t.Fatalf("seed projection row: %v", err)
	}

	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	appender := conversation.NewBufferedAppender(store, 10)
	appender.SetFreshnessStore(fs)

	_ = appender.Append(ctx, conversation.Turn{
		ChatID: 1, UserID: 1, TurnIndex: 0,
		Role: "user", Content: "hello",
	})
	if err := appender.Close(ctx); err != nil {
		t.Fatalf("Close: %v", err)
	}

	row, found, err := fs.Get(ctx, "compact_memory_archive")
	if err != nil || !found {
		t.Fatalf("Get projection: err=%v found=%v", err, found)
	}
	fmt.Printf("compact_memory_archive: PendingCount=%d Status=%s\n", row.PendingCount, row.Status)
	if row.PendingCount != 1 {
		t.Errorf("PendingCount = %d, want 1", row.PendingCount)
	}
}

func TestBufferedAppender_FreshnessNilSafe(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	appender := conversation.NewBufferedAppender(store, 10)
	// No SetFreshnessStore call — nil-safe path.
	_ = appender.Append(context.Background(), conversation.Turn{
		ChatID: 2, UserID: 1, TurnIndex: 0,
		Role: "user", Content: "nil-safe check",
	})
	if err := appender.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
