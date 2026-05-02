package conversation_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/scheduler"
)

func TestArchiveStore_AppendAndList(t *testing.T) {
	db := scheduler.NewTestDB(t)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatal(err)
	}

	turn := conversation.Turn{
		ChatID: 42, UserID: 7, TurnIndex: 0,
		Role: "user", Content: "hello",
	}
	if err := store.Append(context.Background(), turn); err != nil {
		t.Fatal(err)
	}

	got, err := store.ListByChat(context.Background(), 42, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].Content != "hello" {
		t.Fatalf("content mismatch: %q", got[0].Content)
	}
}

func TestArchiveStore_AppendIdempotentTurnIndex(t *testing.T) {
	db := scheduler.NewTestDB(t)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatal(err)
	}

	turn := conversation.Turn{
		ChatID: 1, UserID: 1, TurnIndex: 0,
		Role: "user", Content: "first",
	}
	if err := store.Append(context.Background(), turn); err != nil {
		t.Fatal(err)
	}

	// Same (chat_id, turn_index) — must return ErrDuplicateTurn.
	err = store.Append(context.Background(), turn)
	if !errors.Is(err, conversation.ErrDuplicateTurn) {
		t.Fatalf("want ErrDuplicateTurn, got %v", err)
	}
}

func TestArchiveStore_ListByChat_Empty(t *testing.T) {
	db := scheduler.NewTestDB(t)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatal(err)
	}

	got, err := store.ListByChat(context.Background(), 999, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil {
		t.Fatal("want empty slice, got nil")
	}
	if len(got) != 0 {
		t.Fatalf("want 0, got %d", len(got))
	}
}

func TestArchiveStore_Get_NotFound(t *testing.T) {
	db := scheduler.NewTestDB(t)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatal(err)
	}

	_, err = store.Get(context.Background(), 9999)
	if !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("want sql.ErrNoRows, got %v", err)
	}
}

func TestArchiveStore_AppendWithToolCalls(t *testing.T) {
	db := scheduler.NewTestDB(t)
	store, err := conversation.NewArchiveStore(db)
	if err != nil {
		t.Fatal(err)
	}

	toolCallsJSON := `[{"id":"tc1","type":"function","function":{"name":"search_wiki","arguments":"{\"query\":\"test\"}"}}]`
	turn := conversation.Turn{
		ChatID: 5, UserID: 3, TurnIndex: 0,
		Role:      "assistant",
		Content:   "",
		ToolCalls: toolCallsJSON,
	}
	if err := store.Append(context.Background(), turn); err != nil {
		t.Fatal(err)
	}

	got, err := store.ListByChat(context.Background(), 5, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
	if got[0].ToolCalls != toolCallsJSON {
		t.Fatalf("tool_calls mismatch: got %q, want %q", got[0].ToolCalls, toolCallsJSON)
	}
}
