//go:build db_integration

package conversations

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/secret"
)

func TestAppendTurnRedactionAtConversationSurfaces(t *testing.T) {
	configured := "configured-at-rest-secret-0123456789"
	discovered := "agent-discovered-working-secret-9876543210"
	secret.ConfigureExactRedactor([]string{"AURA_DB_TOKEN=" + configured})
	t.Cleanup(func() { secret.ConfigureExactRedactor(os.Environ()) })

	pool := migratedPool(t)
	tag, err := pool.Exec(context.Background(),
		`INSERT INTO aura.identities (id, name, kind)
		 VALUES ($1, 'local', 'system') ON CONFLICT (id) DO NOTHING`,
		localID,
	)
	if err != nil {
		t.Fatalf("ensure local identity: %v", err)
	}
	if tag.RowsAffected() == 1 {
		t.Cleanup(func() {
			_, _ = pool.Exec(context.Background(), `DELETE FROM aura.identities WHERE id=$1`, localID)
		})
	}
	runDir := t.TempDir()
	store := New(pool, Config{RunDir: runDir, TurnCapBytes: 96})
	conversationID := newConversation(t, store)
	ctx := context.Background()

	if err := store.AppendTurn(ctx, AppendTurnParams{
		ConversationID: conversationID,
		Seq:            1,
		Role:           llm.RoleTool,
		Content:        configured + " " + discovered,
	}); err != nil {
		t.Fatalf("AppendTurn inline: %v", err)
	}
	spilledInput := configured + " " + discovered + " " + strings.Repeat("x", 256)
	if err := store.AppendTurn(ctx, AppendTurnParams{
		ConversationID: conversationID,
		Seq:            2,
		Role:           llm.RoleTool,
		Content:        spilledInput,
	}); err != nil {
		t.Fatalf("AppendTurn spilled: %v", err)
	}

	var inline string
	if err := pool.QueryRow(ctx,
		"SELECT content FROM aura.conversation_turns WHERE conversation_id=$1 AND seq=1",
		conversationID,
	).Scan(&inline); err != nil {
		t.Fatalf("read inline turn: %v", err)
	}
	assertConfiguredSecretAbsent(t, "inline turn", inline, configured, discovered)

	var spilledPath string
	if err := pool.QueryRow(ctx,
		"SELECT content_sidecar_path FROM aura.conversation_turns WHERE conversation_id=$1 AND seq=2",
		conversationID,
	).Scan(&spilledPath); err != nil {
		t.Fatalf("read spilled turn: %v", err)
	}
	spilled, err := os.ReadFile(spilledPath)
	if err != nil {
		t.Fatalf("read turn sidecar: %v", err)
	}
	assertConfiguredSecretAbsent(t, "turn sidecar", string(spilled), configured, discovered)
}

func assertConfiguredSecretAbsent(t *testing.T, surface, got, configured, discovered string) {
	t.Helper()
	if strings.Contains(got, configured) {
		t.Fatalf("%s persisted the configured secret: %q", surface, got)
	}
	if !strings.Contains(got, secret.ConfiguredValuePlaceholder) {
		t.Fatalf("%s is missing the configured-secret placeholder: %q", surface, got)
	}
	if !strings.Contains(got, discovered) {
		t.Fatalf("%s over-redacted agent-discovered working data: %q", surface, got)
	}
}
