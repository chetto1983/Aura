package arcadedb

import (
	"bytes"
	"context"
	"encoding/json"
	"testing"
	"time"
)

// The vector belongs to the CREATE, not to a later sweep. These cover the seam that
// attaches it on the memory-batch write path -- the path every accepted capture and
// every public memory_batch call goes through -- and the fail-soft direction that
// must keep writing when the embedder is down.

// EmbedStatements stands in for the sidecar embedder. embedderDown reproduces the
// fail-soft contract (nil, never an error) rather than a second failure mode.
func (b *memoryBatchFakeBackend) EmbedStatements(_ context.Context, statements []string) map[string][]float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.embedCalls++
	if b.embedderDown || len(statements) == 0 {
		return nil
	}
	vectors := make(map[string][]float64, len(statements))
	for _, statement := range statements {
		vectors[statement] = make([]float64, vectorDimensions)
	}
	return vectors
}

// TestMemoryBatch_EmbedsCreatedFacts pins the vector to the CREATE, not to a later
// sweep. Only Client.UpsertFact used to embed synchronously; the batch path -- which
// is what every accepted capture and the public memory_batch tool go through -- built
// its new memoryBatchFact without ever setting Embedding, so createFact omitted the
// embedding clause and the fact reached the graph with no vector at all.
//
// Measured on the live stack 2026-09-02, which is why this test exists: a
// durable_artifact fact captured at 09:45:15 was still unreachable by a semantic
// memory_recall at 09:45:27 (the dense leg cannot score a fact that has no vector),
// and only became recallable when memory_embed_backfill logged "embedded":1 at
// 09:47:48 -- two and a half minutes later. ArcadeDB is not the cause and cannot be
// the cure: vectors written between graph rebuilds live in an in-memory delta buffer
// that "every query scores ... so a search never returns a corpus older than the last
// write" (arcadedb-docs, vector-embeddings.adoc). A vector attached at write time is
// searchable at once; the bug was that no vector was attached.
func TestMemoryBatch_EmbedsCreatedFacts(t *testing.T) {
	const identity = "identity-a"
	backend := newMemoryBatchFakeBackend(identity, memoryBatchTestState())
	request := MemoryBatchRequest{IdempotencyKey: "capture-1", Operations: []MemoryBatchOperation{
		memoryBatchTestUpsert("Davide", "lives_in", "Caraglio", "run-new"),
	}}

	if _, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity), request, now,
		defaultMemoryLimits, backend,
	); err != nil {
		t.Fatalf("applyMemoryBatch: %v", err)
	}

	final := backend.snapshot(identity)
	if len(final.Facts) != 1 {
		t.Fatalf("final facts = %d, want exactly one created fact", len(final.Facts))
	}
	for _, fact := range final.Facts {
		vector, ok := fact.Embedding.([]float64)
		if !ok {
			t.Fatalf("created fact Embedding = %#v (%T), want []float64 attached at write time",
				fact.Embedding, fact.Embedding)
		}
		if len(vector) != vectorDimensions {
			t.Fatalf("embedding width = %d, want %d", len(vector), vectorDimensions)
		}
	}
	if got := backend.embedCalls; got != 1 {
		t.Fatalf("embed calls = %d, want exactly one batched call outside the transaction", got)
	}
}

// TestMemoryBatch_EmbedderDownStillWrites keeps the write path fail-SOFT. An embedder
// that is down must degrade retrieval, never refuse a write: a fact that was not
// stored is lost, while a fact stored without its vector is still found lexically
// today and embedded by the backfill sweep later.
func TestMemoryBatch_EmbedderDownStillWrites(t *testing.T) {
	const identity = "identity-a"
	backend := newMemoryBatchFakeBackend(identity, memoryBatchTestState())
	backend.embedderDown = true
	request := MemoryBatchRequest{IdempotencyKey: "capture-2", Operations: []MemoryBatchOperation{
		memoryBatchTestUpsert("Davide", "lives_in", "Caraglio", "run-new"),
	}}

	if _, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity), request, now,
		defaultMemoryLimits, backend,
	); err != nil {
		t.Fatalf("applyMemoryBatch must not fail when the embedder is down: %v", err)
	}

	final := backend.snapshot(identity)
	if len(final.Facts) != 1 {
		t.Fatalf("final facts = %d, want the fact written without a vector", len(final.Facts))
	}
	for _, fact := range final.Facts {
		if fact.Embedding != nil {
			t.Fatalf("Embedding = %#v, want nil so EmbedMissingFacts picks it up", fact.Embedding)
		}
	}
}

// A batch result names each written fact by its KEY, never by echoing the
// statement back. The echo made a batch cost its own payload twice in the
// caller's context -- measured 2026-09-02 on a 102-fact import, where it was the
// largest single line item -- and bought nothing, since the caller wrote those
// statements. The key is what supersedes_fact_key needs later, so the smaller
// result is also the more useful one. Nothing asserted this shape before, so the
// echo could have come back unnoticed.
func TestMemoryBatchResultNamesFactsByKeyNotStatement(t *testing.T) {
	identity := "11111111-1111-4111-8111-111111111111"
	now := time.Date(2026, 9, 2, 12, 0, 0, 0, time.UTC)
	backend := newMemoryBatchFakeBackend(identity, memoryBatchTestState())
	statement := "Davide lives in Torino."
	request := MemoryBatchRequest{IdempotencyKey: "shape-1", Operations: []MemoryBatchOperation{
		memoryBatchTestUpsert("Davide", "lives_in", "Torino", "run-1"),
	}}
	request.Operations[0].Fact.Statement = statement

	result, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity), request, now,
		defaultMemoryLimits, backend,
	)
	if err != nil {
		t.Fatalf("ApplyMemoryBatch: %v", err)
	}
	if len(result.Operations) != 1 {
		t.Fatalf("operations = %+v, want one", result.Operations)
	}
	if key := result.Operations[0].FactKey; key == "" {
		t.Fatal("operation result carries no fact key -- supersedes_fact_key has nothing to use")
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if bytes.Contains(encoded, []byte(statement)) {
		t.Fatalf("batch result echoes the statement back: %s", encoded)
	}
}
