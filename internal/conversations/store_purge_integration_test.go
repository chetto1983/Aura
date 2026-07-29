//go:build db_integration

package conversations

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

func TestPurgeConversationsDeletesOnlyOwnerAndArtifacts(t *testing.T) {
	ctx := context.Background()
	pool := migratedPool(t)
	runDir := t.TempDir()
	store := New(pool, Config{RunDir: runDir, TurnCapBytes: 65536})
	owner := uuid.New().String()
	other := uuid.New().String()
	seedIdentity(t, pool, owner, "purge-owner-"+owner[:8])
	seedIdentity(t, pool, other, "purge-other-"+other[:8])

	owned, err := store.Create(ctx, CreateParams{
		ID: uuid.New().String(), IdentityID: owner, Model: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	foreign, err := store.Create(ctx, CreateParams{
		ID: uuid.New().String(), IdentityID: other, Model: "test",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.AppendTurn(ctx, AppendTurnParams{
		ConversationID: owned.ID, Role: llm.RoleUser, Content: "private",
	}); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(runDir, "conversations", owned.ID, "tool.result")
	if err := os.MkdirAll(filepath.Dir(artifact), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(artifact, []byte("private"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := store.PurgeConversations(ctx, owner); err != nil {
		t.Fatalf("PurgeConversations: %v", err)
	}
	if _, err := store.Get(ctx, owned.ID); err == nil {
		t.Fatal("owned conversation survived purge")
	}
	if _, err := store.Get(ctx, foreign.ID); err != nil {
		t.Fatalf("foreign conversation was removed: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(artifact)); !os.IsNotExist(err) {
		t.Fatalf("owned artifact tree still exists: %v", err)
	}
	if err := store.PurgeConversations(ctx, owner); err != nil {
		t.Fatalf("idempotent retry: %v", err)
	}
}
