package arcadedb

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestMemoryBatch_SupersedeExactTarget(t *testing.T) {
	const identity = "identity-a"
	oldFact := memoryBatchTestStoredFact("Davide", "lives_in", "Torino", "run-old")
	backend := newMemoryBatchFakeBackend(identity, memoryBatchTestState(oldFact))
	replacement := memoryBatchTestUpsert("Davide", "lives_in", "Caraglio", "run-new")
	replacement.Type = MemoryBatchSupersedeFact
	replacement.Fact.TargetFactKey = oldFact.FactKey
	replacement.Fact.ValidFrom = oldFact.ValidFrom.Add(time.Hour)

	result, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity),
		MemoryBatchRequest{IdempotencyKey: "supersede-1", Operations: []MemoryBatchOperation{replacement}},
		now, defaultMemoryLimits, backend,
	)
	if err != nil {
		t.Fatalf("supersede: %v", err)
	}
	if len(result.Operations) != 1 || result.Operations[0].Superseded != 1 {
		t.Fatalf("result = %+v", result)
	}
	active := 0
	historical := 0
	for _, fact := range backend.snapshot(identity).Facts {
		if memoryBatchFactActive(fact, now) {
			active++
			if fact.Fact.Object != "Caraglio" {
				t.Fatalf("active replacement = %+v", fact)
			}
		} else {
			historical++
		}
	}
	if active != 1 || historical != 1 {
		t.Fatalf("active=%d historical=%d", active, historical)
	}
}

func TestMemoryBatch_AmbiguousSupersedeRollsBack(t *testing.T) {
	const identity = "identity-a"
	initial := memoryBatchTestState(
		memoryBatchTestStoredFact("Davide", "likes", "Coffee", "run-coffee"),
		memoryBatchTestStoredFact("Davide", "likes", "Tea", "run-tea"),
	)
	backend := newMemoryBatchFakeBackend(identity, initial)
	replacement := memoryBatchTestUpsert("Davide", "likes", "Water", "run-water")
	replacement.Type = MemoryBatchSupersedeFact

	_, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity),
		MemoryBatchRequest{IdempotencyKey: "supersede-ambiguous", Operations: []MemoryBatchOperation{replacement}},
		now, defaultMemoryLimits, backend,
	)
	var batchErr *MemoryBatchError
	if !errors.As(err, &batchErr) || batchErr.Index != 0 || batchErr.Code != "target_ambiguous" {
		t.Fatalf("error = %v", err)
	}
	if got := backend.snapshot(identity); !reflect.DeepEqual(got, initial) {
		t.Fatalf("ambiguous supersede changed state: got=%+v want=%+v", got, initial)
	}
}

func TestMemoryBatch_MergeFoldsCollisionAndProvenance(t *testing.T) {
	const identity = "identity-a"
	alias := memoryBatchTestStoredFact("D. Marchetto", "likes", "Coffee", "run-alias")
	canonical := memoryBatchTestStoredFact("Davide", "likes", "Coffee", "run-canonical")
	backend := newMemoryBatchFakeBackend(identity, memoryBatchTestState(alias, canonical))

	result, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity),
		MemoryBatchRequest{IdempotencyKey: "merge-1", Operations: []MemoryBatchOperation{{
			Type:  MemoryBatchMergeEntities,
			Merge: &MemoryBatchMerge{Source: "D. Marchetto", Target: "Davide"},
		}}}, now, defaultMemoryLimits, backend,
	)
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	if result.Operations[0].Moved != 1 || result.Operations[0].Dropped != 1 {
		t.Fatalf("merge result = %+v", result.Operations[0])
	}
	final := backend.snapshot(identity)
	if _, exists := final.Entities["D. Marchetto"]; exists || len(final.Facts) != 1 {
		t.Fatalf("merge final state = %+v", final)
	}
	for _, fact := range final.Facts {
		if fact.Fact.Subject != "Davide" || len(fact.Sources) != 2 {
			t.Fatalf("merged fact = %+v", fact)
		}
	}
}

func TestMemoryBatch_CompileRejectsMalformedUnion(t *testing.T) {
	operation := memoryBatchTestUpsert("Davide", "likes", "Coffee", "run-1")
	operation.Forget = &ForgetFilter{Subject: "Davide"}
	_, err := CompileMemoryBatch(MemoryBatchRequest{
		IdempotencyKey: "malformed-1", Operations: []MemoryBatchOperation{operation},
	}, defaultMemoryLimits)
	var batchErr *MemoryBatchError
	if !errors.As(err, &batchErr) || batchErr.Index != 0 || batchErr.Code != "malformed_operation" {
		t.Fatalf("error = %v", err)
	}
}
