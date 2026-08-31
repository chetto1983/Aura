//go:build arcadedb_integration

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/arcadedb"
)

func TestAgentMemoryMCPLiveMixedTierRecall(t *testing.T) {
	verifyAgentMemoryLiveNoLeaks(t)
	sessions, identities, _ := newAgentMemoryLiveMCP(t, 1, "")
	identityID := identities[0]
	session := sessions[identityID]
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()

	callAgentMemoryLiveJSON[MemoryUpsertFactOutput](
		t, ctx, session, "memory_upsert_fact", map[string]any{
			"subject": "Aurora notebook", "predicate": "stored_in", "object": "Turin archive",
			"statement": "The aurora notebook is stored in the Turin archive.",
			"source":    map[string]any{"memory_ids": []string{"mixed-fact-source"}},
		})
	client := agentMemoryLiveTenantClient(t, ctx, identityID)
	content := "We discussed the aurora notebook route through Turin."
	if err := client.ApplyConversationProjection(ctx, arcadedb.ConversationProjection{
		IdentityID: identityID, ConversationID: "conversation-mixed-live",
		Turns: []arcadedb.ConversationTurnProjection{
			{
				IdentityID: identityID, ConversationID: "conversation-mixed-live", Seq: 1,
				Role: "assistant", Content: "I found the earlier travel note.",
				ContentHash: recallLiveContentHash("I found the earlier travel note."),
				OccurredAt:  time.Now().UTC().Add(-time.Minute),
				SourceRef:   "postgres://mixed-live/turn/1",
			},
			{
				IdentityID: identityID, ConversationID: "conversation-mixed-live", Seq: 2,
				Role: "user", Content: content, ContentHash: recallLiveContentHash(content),
				OccurredAt: time.Now().UTC(), SourceRef: "postgres://mixed-live/turn/2",
			},
		},
	}); err != nil {
		t.Fatalf("ApplyConversationProjection: %v", err)
	}

	output := callAgentMemoryLiveJSON[MemoryRecallOutput](
		t, ctx, session, "memory_recall", map[string]any{
			"query": "aurora notebook Turin", "limit": 5,
		})
	if output.Retrieval.Path != "hybrid" || output.Retrieval.EffectivePath != "mixed" {
		t.Fatalf("retrieval = %+v; evidence = %+v", output.Retrieval, output.Evidence)
	}
	if output.Retrieval.FactCount != 1 || output.Retrieval.ConversationCount != 1 {
		t.Fatalf("tier counts = %+v", output.Retrieval)
	}
	if len(output.Evidence) != 2 || len(output.Facts) != 1 {
		t.Fatalf("output = %+v", output)
	}
}

func agentMemoryLiveTenantClient(t *testing.T, ctx context.Context, identityID string) *arcadedb.Client {
	t.Helper()
	adminPassword := strings.TrimSpace(os.Getenv("ARCADEDB_ADMIN_PASSWORD"))
	if adminPassword == "" {
		adminPassword = os.Getenv("ARCADEDB_PASSWORD")
	}
	base := arcadedb.Config{
		BaseURL:  agentMemoryLiveEnv("ARCADEDB_URL", "http://127.0.0.1:2480"),
		Database: agentMemoryLiveEnv("ARCADEDB_DATABASE", "aura_memory"),
		User:     agentMemoryLiveEnv("ARCADEDB_ADMIN_USER", "root"), Password: adminPassword,
		Timeout: time.Minute,
	}
	admin, err := arcadedb.New(base)
	if err != nil {
		t.Fatalf("admin client: %v", err)
	}
	credentials, err := arcadedb.NewTenantCredentials()
	if err != nil {
		t.Fatalf("tenant credentials: %v", err)
	}
	embedder := arcadedb.NewSidecarEmbedder(
		agentMemoryLiveEnv("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081"),
		os.Getenv("AURA_EMBED_MODEL"), os.Getenv("AURA_EMBED_API_KEY"), time.Minute,
	)
	client, err := newTenants(base, admin, embedder, credentials).For(ctx, identityID)
	if err != nil {
		t.Fatalf("tenant client: %v", err)
	}
	return client
}

func recallLiveContentHash(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}
