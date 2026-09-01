//go:build arcadedb_integration

package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"

	officialmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/arcadedb"
	auramcp "github.com/chetto1983/aura/internal/mcp"
)

const agentMemoryBatchMarker = "AURA_AGENT_MEMORY_BATCH_JSON="

func TestAgentMemoryMCPLive_BatchAtomicity(t *testing.T) {
	verifyAgentMemoryLiveNoLeaks(t)
	sessions, identities, _, tenants := newAgentMemoryLiveMCPWithOptions(
		t, 2, "", agentMemoryLiveMCPOptions{strictDependencies: true},
	)
	ctx, cancel := context.WithTimeout(t.Context(), agentMemoryLiveTimeout)
	defer cancel()
	alphaID, betaID := identities[0], identities[1]
	alpha, beta := sessions[alphaID], sessions[betaID]
	assertAgentMemoryLiveTools(t, drainAgentMemoryLiveTools(t, ctx, alpha), "memory_batch")
	alphaClient, err := tenants.For(ctx, alphaID)
	if err != nil {
		t.Fatalf("alpha tenant: %v", err)
	}
	betaClient, err := tenants.For(ctx, betaID)
	if err != nil {
		t.Fatalf("beta tenant: %v", err)
	}

	beforeHash := agentMemoryBatchStateHash(t, ctx, alphaClient, "PublishedBatch")
	arguments := map[string]any{
		"idempotency_key": "published-batch",
		"operations": []any{map[string]any{
			"type": "upsert_fact",
			"fact": map[string]any{
				"subject": "PublishedBatch", "predicate": "likes", "object": "Coffee",
				"statement": "PublishedBatch likes coffee.",
				"source":    map[string]any{"memory_ids": []string{"published-source"}},
			},
		}},
	}
	first := callAgentMemoryLiveJSON[MemoryBatchOutput](t, ctx, alpha, "memory_batch", arguments)
	if first.Applied != 1 || first.Replayed {
		t.Fatalf("first batch = %+v", first)
	}
	committedHash := agentMemoryBatchStateHash(t, ctx, alphaClient, "PublishedBatch")
	if committedHash == beforeHash {
		t.Fatal("published batch did not change committed state")
	}

	invalid := map[string]any{
		"idempotency_key": "published-invalid",
		"operations": []any{
			map[string]any{"type": "upsert_fact", "fact": map[string]any{
				"subject": "PublishedBatch", "predicate": "visits", "object": "Rome",
				"statement": "PublishedBatch visits Rome.", "source": map[string]any{},
			}},
			map[string]any{"type": "forget", "forget": map[string]any{"subject": "missing-subject"}},
		},
	}
	assertAgentMemoryBatchError(t, ctx, alpha, invalid, "target_not_found")
	rollbackHash := agentMemoryBatchStateHash(t, ctx, alphaClient, "PublishedBatch")
	if rollbackHash != committedHash {
		t.Fatalf("rollback hash = %s, want %s", rollbackHash, committedHash)
	}

	replay := callAgentMemoryLiveJSON[MemoryBatchOutput](t, ctx, alpha, "memory_batch", arguments)
	replayHash := agentMemoryBatchStateHash(t, ctx, alphaClient, "PublishedBatch")
	if !replay.Replayed || replayHash != committedHash {
		t.Fatalf("replay = %+v hash=%s", replay, replayHash)
	}
	different := map[string]any{
		"idempotency_key": "published-batch",
		"operations": []any{map[string]any{
			"type": "forget", "forget": map[string]any{"subject": "PublishedBatch"},
		}},
	}
	assertAgentMemoryBatchError(t, ctx, alpha, different, "idempotency_conflict")

	betaBefore := agentMemoryBatchStateHash(t, ctx, betaClient, "PublishedBatch")
	hits, err := alphaClient.FactsAbout(ctx, "PublishedBatch", "likes", 10, time.Time{})
	if err != nil || len(hits) != 1 || hits[0].FactKey == "" {
		t.Fatalf("alpha fact = %+v, err=%v", hits, err)
	}
	crossIdentity := map[string]any{
		"idempotency_key": "cross-identity",
		"operations": []any{map[string]any{
			"type": "supersede_fact",
			"fact": map[string]any{
				"subject": "PublishedBatch", "predicate": "likes", "object": "Tea",
				"statement": "PublishedBatch likes tea.", "supersedes_fact_key": hits[0].FactKey,
				"source": map[string]any{},
			},
		}},
	}
	assertAgentMemoryBatchError(t, ctx, beta, crossIdentity, "target_not_found")
	betaAfter := agentMemoryBatchStateHash(t, ctx, betaClient, "PublishedBatch")
	alphaAfter := agentMemoryBatchStateHash(t, ctx, alphaClient, "PublishedBatch")
	if betaAfter != betaBefore || alphaAfter != committedHash {
		t.Fatalf("cross-identity changed state: alpha=%s beta=%s", alphaAfter, betaAfter)
	}

	evidence := map[string]any{
		"scenario": "batch_atomicity", "executed_count": 5,
		"before_hash": beforeHash, "committed_hash": committedHash,
		"rollback_hash": rollbackHash, "replay_hash": replayHash,
		"cross_identity_before_hash": betaBefore, "cross_identity_after_hash": betaAfter,
		"replayed": replay.Replayed, "logical_effects": len(hits),
		"first_error_code": "target_not_found", "idempotency_conflict_observed": true,
	}
	raw, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("encode batch evidence: %v", err)
	}
	t.Logf("%s%s", agentMemoryBatchMarker, raw)
}

func assertAgentMemoryBatchError(
	t *testing.T,
	ctx context.Context,
	session *officialmcp.ClientSession,
	arguments map[string]any,
	want string,
) {
	t.Helper()
	result, err := session.CallTool(ctx, &officialmcp.CallToolParams{Name: "memory_batch", Arguments: arguments})
	if err != nil {
		t.Fatalf("memory_batch transport: %v", err)
	}
	text, isError := auramcp.DecodeToolResult(result)
	if !isError || !strings.Contains(text, want) {
		t.Fatalf("memory_batch error = isError:%v %q, want %q", isError, text, want)
	}
}

func agentMemoryBatchStateHash(
	t *testing.T,
	ctx context.Context,
	client *arcadedb.Client,
	subject string,
) string {
	t.Helper()
	hits, err := client.FactsAbout(ctx, subject, "", 100, time.Time{})
	if err != nil {
		t.Fatalf("FactsAbout(%s): %v", subject, err)
	}
	raw, err := json.Marshal(hits)
	if err != nil {
		t.Fatalf("encode state: %v", err)
	}
	digest := sha256.Sum256(raw)
	return hex.EncodeToString(digest[:])
}
