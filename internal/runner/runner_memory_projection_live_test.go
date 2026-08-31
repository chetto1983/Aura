//go:build db_integration && arcadedb_integration

package runner

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

func liveProjectionCount(t *testing.T, client *arcadedb.Client, identityID, conversationID string) int {
	t.Helper()
	rows, err := client.Query(context.Background(),
		"SELECT count(*) AS n FROM ConversationTurn WHERE identity_id = :identity_id AND conversation_id = :conversation_id",
		map[string]any{"identity_id": identityID, "conversation_id": conversationID})
	if err != nil {
		t.Fatalf("count live projections: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("projection count rows = %v", rows)
	}
	n, err := strconv.Atoi(fmt.Sprint(rows[0]["n"]))
	if err != nil {
		t.Fatalf("projection count %v: %v", rows[0]["n"], err)
	}
	return n
}

func liveProjectionClient(t *testing.T) *arcadedb.Client {
	t.Helper()
	base := envOrSkip(t, "ARCADEDB_URL")
	password := envOrSkip(t, "ARCADEDB_PASSWORD")
	user := strings.TrimSpace(os.Getenv("ARCADEDB_ADMIN_USER"))
	if user == "" {
		user = "root"
	}
	admin, err := arcadedb.New(arcadedb.Config{
		BaseURL: base, Database: "unused", User: user, Password: password,
	})
	if err != nil {
		t.Fatalf("build ArcadeDB admin: %v", err)
	}
	database := fmt.Sprintf("aura_projection_%d", time.Now().UnixNano())
	if _, err := admin.CreateDatabase(context.Background(), database); err != nil {
		t.Fatalf("create disposable ArcadeDB database: %v", err)
	}
	t.Cleanup(func() {
		if _, err := admin.DropDatabase(context.Background(), database); err != nil {
			t.Errorf("drop disposable ArcadeDB database %s: %v", database, err)
		}
	})
	client, err := arcadedb.New(arcadedb.Config{
		BaseURL: base, Database: database, User: user, Password: password,
	})
	if err != nil {
		t.Fatalf("build disposable ArcadeDB client: %v", err)
	}
	if err := client.EnsureMemorySchema(context.Background()); err != nil {
		t.Fatalf("initialize disposable memory schema: %v", err)
	}
	return client
}

func TestConversationProjectionLiveCrashRecovery(t *testing.T) {
	pool := migratedRunnerPool(t)
	convStore := conversations.New(pool, conversations.Config{
		RunDir: t.TempDir(), TurnCapBytes: 65536,
	})
	conversationID := newIntegrationConversation(t, pool, convStore)
	ctx := ownerCtx()
	if err := convStore.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: conversationID, Role: llm.RoleUser,
		Content: "live crash window cobalt notebook",
	}); err != nil {
		t.Fatalf("commit authoritative turn: %v", err)
	}

	sink := liveProjectionClient(t)
	if got := liveProjectionCount(t, sink, localIdentityID, conversationID); got != 0 {
		t.Fatalf("pre-restart derived rows = %d, want the injected crash gap", got)
	}
	projector := NewConversationProjector(convStore, sink, 1)
	t.Cleanup(func() { _ = projector.Close(context.Background()) })
	reconciler := NewDeleteReconciler(&Runner{Conv: convStore}, time.Hour)
	reconciler.SetConversationProjection(
		projector, &projectionIdentityRoster{ids: []string{localIdentityID}},
	)
	for attempt := range 2 {
		if err := reconciler.ReconcileConversationProjection(ctx); err != nil {
			t.Fatalf("live reconcile attempt %d: %v", attempt+1, err)
		}
	}
	if got := liveProjectionCount(t, sink, localIdentityID, conversationID); got != 1 {
		t.Fatalf("post-restart derived rows = %d, want exactly 1 after replay", got)
	}
	rows, err := sink.Query(context.Background(),
		"SELECT content, source_ref FROM ConversationTurn WHERE identity_id = :identity_id AND conversation_id = :conversation_id",
		map[string]any{"identity_id": localIdentityID, "conversation_id": conversationID})
	if err != nil {
		t.Fatalf("read live projection: %v", err)
	}
	if len(rows) != 1 || fmt.Sprint(rows[0]["content"]) != "live crash window cobalt notebook" ||
		strings.TrimSpace(fmt.Sprint(rows[0]["source_ref"])) == "" {
		t.Fatalf("live projection lost authoritative content/provenance: %v", rows)
	}
}
