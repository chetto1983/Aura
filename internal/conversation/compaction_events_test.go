package conversation_test

import (
	"context"
	"testing"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/testutil"
)

func TestArchiveStore_RecordAndListCompactionsForTurn(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatalf("NewArchiveStore: %v", err)
	}
	ctx := context.Background()
	createdAt := time.Date(2026, 5, 24, 9, 10, 11, 0, time.UTC)

	if err := store.RecordCompaction(ctx, conversation.CompactionEvent{
		RunID:            "run-ctx-1",
		ChatID:           42,
		TurnIndex:        7,
		Iteration:        2,
		MessagesBefore:   18,
		MessagesAfter:    4,
		TokensBefore:     24000,
		TokensAfter:      1200,
		ThresholdTokens:  16000,
		CumulativeTokens: 25000,
		FocusPreview:     "Authorization: Bearer sample-token",
		ElapsedMS:        345,
		CreatedAt:        createdAt,
	}); err != nil {
		t.Fatalf("RecordCompaction: %v", err)
	}

	events, err := store.ListCompactionsForTurn(ctx, 42, 7)
	if err != nil {
		t.Fatalf("ListCompactionsForTurn: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	got := events[0]
	if got.RunID != "run-ctx-1" || got.ChatID != 42 || got.TurnIndex != 7 || got.Iteration != 2 {
		t.Fatalf("identity fields = %+v", got)
	}
	if got.TokensBefore != 24000 || got.TokensAfter != 1200 || got.CumulativeTokens != 25000 {
		t.Fatalf("token fields = %+v", got)
	}
	if got.FocusPreview != "[redacted]" {
		t.Fatalf("FocusPreview = %q, want redacted", got.FocusPreview)
	}
	if !got.CreatedAt.Equal(createdAt) {
		t.Fatalf("CreatedAt = %s, want %s", got.CreatedAt, createdAt)
	}
}
