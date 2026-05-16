package agentnote_test

import (
	"context"
	"strings"
	"testing"

	"github.com/aura/aura/internal/agentnote"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/testutil"
)

// TestAgentNoteLifecycle is the integration test for US-P03:
// set → context injection → clear (GC event) → note gone.
func TestAgentNoteLifecycle(t *testing.T) {
	db := testutil.OpenTestDB(t, migrations.Run)
	store := agentnote.NewStore(db)
	ctx := context.Background()
	convID := "lifecycle-test-conv"

	// Step 1: set a note.
	if err := store.Set(ctx, convID, "TODO: verify X, Y, Z"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// Step 2: simulate "next run" — read note and inject into conversation context.
	noteContent, exists, err := store.Get(ctx, convID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !exists {
		t.Fatal("Get: expected note to exist after Set")
	}

	convCtx := conversation.NewContext(conversation.Config{MaxTokens: 4000})
	convCtx.SetSystemMessage("base prompt")
	if noteContent != "" {
		convCtx.SetAgentNote(noteContent)
	}

	msgs := convCtx.Messages()
	if len(msgs) == 0 || msgs[0].Role != "system" {
		t.Fatal("expected system message")
	}
	sysContent := msgs[0].Content
	if !strings.Contains(sysContent, "## Your current note (working memory)") {
		t.Fatalf("system message missing note section:\n%s", sysContent)
	}
	if !strings.Contains(sysContent, "TODO: verify X, Y, Z") {
		t.Fatalf("system message missing note content:\n%s", sysContent)
	}

	// Step 3: close event — GC the note.
	if err := store.Clear(ctx, convID); err != nil {
		t.Fatalf("Clear: %v", err)
	}

	// Step 4: new conversation — note must be absent.
	_, exists, err = store.Get(ctx, convID)
	if err != nil {
		t.Fatalf("Get after Clear: %v", err)
	}
	if exists {
		t.Fatal("Get after Clear: expected note to be gone (GC)")
	}
}
