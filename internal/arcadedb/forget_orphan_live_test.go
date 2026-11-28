//go:build arcadedb_integration

package arcadedb

import (
	"context"
	"errors"
	"testing"
	"time"
)

// Measured on the lab VM on 2026-11-28: six entities with no FACT edge that neither
// memory_forget nor memory_batch could remove. Each carried 3 or 4 TOUCHED edges, the
// reasoning-trace edge the runner writes after every memory call that names an entity,
// and the prune counted those as a reason to keep the vertex.
func touchedOrphans(t *testing.T, names ...string) *Client {
	t.Helper()
	client := disposableMemoryClient(t)
	ctx := context.Background()
	const runID = "orphan-run"
	for i := 0; i+1 < len(names); i += 2 {
		fact := Fact{
			Subject: names[i], Predicate: "relates_to", Object: names[i+1],
			Statement: names[i] + " relates to " + names[i+1] + ".",
			Source:    FactSource{RunID: runID, WriterRole: WriterParent},
		}
		if _, err := client.UpsertFact(ctx, fact, time.Now().UTC()); err != nil {
			t.Fatalf("seed fact: %v", err)
		}
	}
	if _, err := client.Forget(ctx, ForgetFilter{SourceRunID: runID, KeepOrphans: true}); err != nil {
		t.Fatalf("strand the entities: %v", err)
	}
	if _, err := client.Command(ctx,
		"INSERT INTO "+reasoningStepType+" SET identity_id = 'orphan', trace_id = 'orphan-trace',"+
			" step_index = 0, provider_summary = 'touched', created_at = sysdate()", nil); err != nil {
		t.Fatalf("seed reasoning step: %v", err)
	}
	for _, name := range names {
		if _, err := client.Command(ctx, createReasoningTouchedStatement, map[string]any{
			"identity_id": "orphan", "trace_id": "orphan-trace", "step_index": 0, "entity_name": name,
		}); err != nil {
			t.Fatalf("seed TOUCHED edge to %s: %v", name, err)
		}
	}
	return client
}

func requireEntityGone(t *testing.T, client *Client, name string) {
	t.Helper()
	classes, err := client.entityClassScan(context.Background(), name)
	if err != nil {
		t.Fatalf("entityClassScan: %v", err)
	}
	if held, exists := classes[name]; exists {
		t.Fatalf("%s still exists as %s after it was reported removed", name, held)
	}
}

func TestForgetLive_RemovesAnEntityOnlyReasoningTouches(t *testing.T) {
	client := touchedOrphans(t, "OrphanPaolo", "OrphanTorino")
	ctx := context.Background()

	dry, err := client.Forget(ctx, ForgetFilter{Entity: "OrphanPaolo", DryRun: true})
	if err != nil {
		t.Fatalf("dry run: %v", err)
	}
	if dry.Entities != 1 {
		t.Fatalf("dry run = %+v, want 1 entity", dry)
	}
	got, err := client.Forget(ctx, ForgetFilter{Entity: "OrphanPaolo"})
	if err != nil {
		t.Fatalf("Forget: %v", err)
	}
	if got.Entities != 1 {
		t.Fatalf("Forget = %+v, want 1 entity", got)
	}
	requireEntityGone(t, client, "OrphanPaolo")
}

func TestMemoryBatchLive_ForgetsAnEntityWithNoFacts(t *testing.T) {
	client := touchedOrphans(t, "OrphanGo", "OrphanPython")
	ctx := context.Background()
	actor := MemoryBatchActor{IdentityID: "orphan", WriterRole: WriterParent}

	got, err := client.ApplyMemoryBatch(ctx, actor, MemoryBatchRequest{
		IdempotencyKey: "orphan-forget",
		Operations: []MemoryBatchOperation{
			{Type: MemoryBatchForget, Forget: &ForgetFilter{Entity: "OrphanGo"}},
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatalf("ApplyMemoryBatch: %v", err)
	}
	if len(got.Operations) != 1 || got.Operations[0].Entities != 1 {
		t.Fatalf("result = %+v, want one entity removed", got.Operations)
	}
	requireEntityGone(t, client, "OrphanGo")

	_, err = client.ApplyMemoryBatch(ctx, actor, MemoryBatchRequest{
		IdempotencyKey: "orphan-forget-nobody",
		Operations: []MemoryBatchOperation{
			{Type: MemoryBatchForget, Forget: &ForgetFilter{Entity: "OrphanNobody"}},
		},
	}, time.Now().UTC())
	var batchErr *MemoryBatchError
	if !errors.As(err, &batchErr) || batchErr.Code != "target_not_found" {
		t.Fatalf("forget of a name that does not exist = %v, want target_not_found", err)
	}
}
