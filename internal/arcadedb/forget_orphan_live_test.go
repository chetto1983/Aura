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

// An entity no fact holds but another fact's statement names stays while that fact does:
// its MENTIONS edge is the only link the graph has to it, and nothing re-creates the
// entity. A MENTIONS edge goes with the fact it came from, so it never holds a name
// after its fact is gone (operator's ruling on the final review, 2026-11-28).
func mentionedEntity(t *testing.T) (*Client, func(predicate string) string) {
	t.Helper()
	client := disposableMemoryClient(t)
	ctx := context.Background()
	for _, fact := range []Fact{
		{Subject: "MenAcme", Predicate: "part_of", Object: "MenGroup", Source: FactSource{RunID: "men-r1", WriterRole: WriterParent}},
		{Subject: "MenMonica", Predicate: "met", Object: "MenDavide", Source: FactSource{RunID: "men-r2", WriterRole: WriterParent}},
	} {
		fact.Statement = fact.Subject + " " + fact.Predicate + " " + fact.Object + " at MenAcme."
		if _, err := client.UpsertFact(ctx, fact, time.Now().UTC()); err != nil {
			t.Fatalf("seed fact: %v", err)
		}
	}
	ridOf := func(predicate string) string {
		rows, err := client.Query(ctx, "SELECT @rid AS rid FROM "+factEdgeType+" WHERE predicate = :p",
			map[string]any{"p": predicate})
		if err != nil || len(rows) != 1 {
			t.Fatalf("rid of %s: %v %v", predicate, rows, err)
		}
		return rowString(rows[0], "rid")
	}
	if _, err := client.Command(ctx, mentionCreateStatement, map[string]any{
		"source": "MenMonica", "target": "MenAcme", "fact_key": "men-met", "fact_rid": ridOf("met"),
	}); err != nil {
		t.Fatalf("seed MENTIONS: %v", err)
	}
	return client, ridOf
}

func entityExists(t *testing.T, client *Client, name string) bool {
	t.Helper()
	classes, err := client.entityClassScan(context.Background(), name)
	if err != nil {
		t.Fatalf("entityClassScan: %v", err)
	}
	_, exists := classes[name]
	return exists
}

func TestForgetLive_KeepsAnEntityAnotherFactStillNames(t *testing.T) {
	client, _ := mentionedEntity(t)
	ctx := context.Background()

	first, err := client.Forget(ctx, ForgetFilter{SourceRunID: "men-r1"})
	if err != nil {
		t.Fatalf("forget run 1: %v", err)
	}
	if first.Entities != 1 || !entityExists(t, client, "MenAcme") || entityExists(t, client, "MenGroup") {
		t.Fatalf("forget run 1 = %+v: want MenGroup removed and MenAcme kept by its mention", first)
	}
	second, err := client.Forget(ctx, ForgetFilter{SourceRunID: "men-r2"})
	if err != nil {
		t.Fatalf("forget run 2: %v", err)
	}
	if second.Entities != 3 || entityExists(t, client, "MenAcme") || entityExists(t, client, "MenMonica") {
		t.Fatalf("forget run 2 = %+v: want Monica, Davide and the no-longer-named MenAcme removed", second)
	}
}

func TestMemoryBatchLive_KeepsAnEntityAnotherFactStillNames(t *testing.T) {
	client, _ := mentionedEntity(t)
	ctx := context.Background()
	actor := MemoryBatchActor{IdentityID: "men", WriterRole: WriterParent}
	forget := func(key string, filter ForgetFilter) (MemoryBatchResult, error) {
		return client.ApplyMemoryBatch(ctx, actor, MemoryBatchRequest{IdempotencyKey: key,
			Operations: []MemoryBatchOperation{{Type: MemoryBatchForget, Forget: &filter}}}, time.Now().UTC())
	}

	first, err := forget("men-1", ForgetFilter{SourceRunID: "men-r1"})
	if err != nil {
		t.Fatalf("forget run 1: %v", err)
	}
	if first.Operations[0].Entities != 1 || !entityExists(t, client, "MenAcme") {
		t.Fatalf("forget run 1 = %+v: want only MenGroup counted and MenAcme kept", first.Operations)
	}
	second, err := forget("men-3", ForgetFilter{SourceRunID: "men-r2"})
	if err != nil {
		t.Fatalf("forget run 2: %v", err)
	}
	if second.Operations[0].Entities != 3 || entityExists(t, client, "MenAcme") {
		t.Fatalf("forget run 2 = %+v: want Monica, Davide and the no-longer-named MenAcme removed",
			second.Operations)
	}
}

// Naming an entity is asking for it to go. A MENTIONS edge keeps a name only through a
// removal that did not name it; asked by name, the entity goes with its MENTIONS edges, and
// the facts whose statements name it stay. Measured on the lab VM on 2026-11-28: with the
// edge holding the name against an explicit forget, the only way to clear "Email" was to
// delete the operator's real preference that mentions it.
func TestForgetLive_ExplicitForgetRemovesAMentionedNameButNotItsFacts(t *testing.T) {
	for _, viaBatch := range []bool{false, true} {
		client, _ := mentionedEntity(t)
		ctx := context.Background()
		if _, err := client.Forget(ctx, ForgetFilter{SourceRunID: "men-r1"}); err != nil {
			t.Fatalf("strand MenAcme: %v", err)
		}
		removed := 0
		if viaBatch {
			got, err := client.ApplyMemoryBatch(ctx, MemoryBatchActor{IdentityID: "men", WriterRole: WriterParent},
				MemoryBatchRequest{IdempotencyKey: "men-explicit", Operations: []MemoryBatchOperation{
					{Type: MemoryBatchForget, Forget: &ForgetFilter{Entity: "MenAcme"}},
				}}, time.Now().UTC())
			if err != nil {
				t.Fatalf("batch forget MenAcme: %v", err)
			}
			removed = got.Operations[0].Entities
		} else {
			dry, err := client.Forget(ctx, ForgetFilter{Entity: "MenAcme", DryRun: true})
			if err != nil || dry.Entities != 1 {
				t.Fatalf("dry run = %+v, %v; want 1 entity", dry, err)
			}
			got, err := client.Forget(ctx, ForgetFilter{Entity: "MenAcme"})
			if err != nil {
				t.Fatalf("forget MenAcme: %v", err)
			}
			removed = got.Entities
		}
		if removed != 1 || entityExists(t, client, "MenAcme") {
			t.Fatalf("batch=%v: removed %d, MenAcme exists=%v; want it gone", viaBatch, removed,
				entityExists(t, client, "MenAcme"))
		}
		hits, err := client.FactsAbout(ctx, "MenMonica", "", 10, time.Time{}, FactsAboutDirect)
		if err != nil || len(hits) != 1 {
			t.Fatalf("batch=%v: the fact naming MenAcme = %+v, %v; want it kept", viaBatch, hits, err)
		}
	}
}
