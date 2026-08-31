package arcadedb

import (
	"context"
	"errors"
	"maps"
	"reflect"
	"sync"
	"testing"
	"time"
)

type memoryBatchFakeBackend struct {
	mu       sync.Mutex
	states   map[string]memoryBatchState
	receipts map[string]map[string]memoryBatchReceipt
	commits  int
}

func newMemoryBatchFakeBackend(identity string, state memoryBatchState) *memoryBatchFakeBackend {
	return &memoryBatchFakeBackend{
		states:   map[string]memoryBatchState{identity: cloneMemoryBatchTestState(state)},
		receipts: map[string]map[string]memoryBatchReceipt{},
	}
}

func (b *memoryBatchFakeBackend) Begin(_ context.Context, identity string) (memoryBatchTransaction, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return &memoryBatchFakeTx{
		backend:  b,
		identity: identity,
		state:    cloneMemoryBatchTestState(b.states[identity]),
		receipts: cloneMemoryBatchTestReceipts(b.receipts[identity]),
	}, nil
}

func (b *memoryBatchFakeBackend) snapshot(identity string) memoryBatchState {
	b.mu.Lock()
	defer b.mu.Unlock()
	return cloneMemoryBatchTestState(b.states[identity])
}

type memoryBatchFakeTx struct {
	backend  *memoryBatchFakeBackend
	identity string
	state    memoryBatchState
	receipts map[string]memoryBatchReceipt
	closed   bool
}

func (tx *memoryBatchFakeTx) LoadReceipt(_ context.Context, key string) (*memoryBatchReceipt, error) {
	receipt, ok := tx.receipts[key]
	if !ok {
		return nil, nil
	}
	copy := receipt
	return &copy, nil
}

func (tx *memoryBatchFakeTx) LoadState(context.Context) (memoryBatchState, error) {
	return cloneMemoryBatchTestState(tx.state), nil
}

func (tx *memoryBatchFakeTx) Persist(_ context.Context, _ memoryBatchState, after memoryBatchState) error {
	tx.state = cloneMemoryBatchTestState(after)
	return nil
}

func (tx *memoryBatchFakeTx) SaveReceipt(_ context.Context, key string, receipt memoryBatchReceipt) error {
	if tx.receipts == nil {
		tx.receipts = map[string]memoryBatchReceipt{}
	}
	tx.receipts[key] = receipt
	return nil
}

func (tx *memoryBatchFakeTx) Commit(context.Context) error {
	if tx.closed {
		return errors.New("transaction already closed")
	}
	tx.backend.mu.Lock()
	defer tx.backend.mu.Unlock()
	tx.backend.states[tx.identity] = cloneMemoryBatchTestState(tx.state)
	if tx.backend.receipts[tx.identity] == nil {
		tx.backend.receipts[tx.identity] = map[string]memoryBatchReceipt{}
	}
	maps.Copy(tx.backend.receipts[tx.identity], tx.receipts)
	tx.backend.commits++
	tx.closed = true
	return nil
}

func (tx *memoryBatchFakeTx) Rollback(context.Context) { tx.closed = true }

func cloneMemoryBatchTestState(state memoryBatchState) memoryBatchState {
	clone := memoryBatchState{Entities: map[string]string{}, Facts: map[string]memoryBatchFact{}}
	maps.Copy(clone.Entities, state.Entities)
	for key, fact := range state.Facts {
		fact.Sources = append([]FactSource(nil), fact.Sources...)
		clone.Facts[key] = fact
	}
	return clone
}

func cloneMemoryBatchTestReceipts(receipts map[string]memoryBatchReceipt) map[string]memoryBatchReceipt {
	clone := map[string]memoryBatchReceipt{}
	maps.Copy(clone, receipts)
	return clone
}

func memoryBatchTestStoredFact(subject, predicate, object, runID string) memoryBatchFact {
	validFrom := time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)
	fact := normalizeFact(Fact{
		Subject: subject, Predicate: predicate, Object: object,
		Statement: subject + " " + predicate + " " + object,
		Source:    FactSource{RunID: runID, WriterRole: WriterParent},
	})
	key := factIdentity(fact)
	return memoryBatchFact{
		RID: "#1:0", Fact: fact, Sources: []FactSource{fact.Source},
		ValidFrom: validFrom, CreatedAt: validFrom, FactKey: key,
	}
}

func memoryBatchTestState(facts ...memoryBatchFact) memoryBatchState {
	state := memoryBatchState{Entities: map[string]string{}, Facts: map[string]memoryBatchFact{}}
	for i, fact := range facts {
		state.Entities[fact.Fact.Subject] = fact.Fact.SubjectKind
		state.Entities[fact.Fact.Object] = fact.Fact.ObjectKind
		state.Facts[fact.RID+string(rune(i))] = fact
	}
	return state
}

func memoryBatchTestActor(identity string) MemoryBatchActor {
	return MemoryBatchActor{IdentityID: identity, WriterRole: WriterParent}
}

func memoryBatchTestUpsert(subject, predicate, object, runID string) MemoryBatchOperation {
	return MemoryBatchOperation{Type: MemoryBatchUpsertFact, Fact: &Fact{
		Subject: subject, Predicate: predicate, Object: object,
		Statement: subject + " " + predicate + " " + object,
		Source:    FactSource{RunID: runID, WriterRole: WriterParent},
	}}
}

func TestMemoryBatch_FinalStateTracer(t *testing.T) {
	const identity = "identity-a"
	oldFact := memoryBatchTestStoredFact("Davide", "lives_in", "Torino", "run-old")
	initial := memoryBatchTestState(oldFact)
	backend := newMemoryBatchFakeBackend(identity, initial)
	request := MemoryBatchRequest{IdempotencyKey: "correction-1", Operations: []MemoryBatchOperation{
		{Type: MemoryBatchForget, Forget: &ForgetFilter{Subject: "Davide", Predicate: "lives_in", Object: "Torino"}},
		memoryBatchTestUpsert("Davide", "lives_in", "Caraglio", "run-new"),
	}}

	result, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity), request, now,
		defaultMemoryLimits, backend,
	)
	if err != nil {
		t.Fatalf("ApplyMemoryBatch: %v", err)
	}
	if result.Applied != 2 || result.Replayed {
		t.Fatalf("result = %+v", result)
	}
	final := backend.snapshot(identity)
	if len(final.Facts) != 1 {
		t.Fatalf("final facts = %+v, want one replacement", final.Facts)
	}
	for _, fact := range final.Facts {
		if fact.Fact.Object != "Caraglio" {
			t.Fatalf("replacement object = %q, want Caraglio", fact.Fact.Object)
		}
	}
}

func TestMemoryBatch_RollbackFirstError(t *testing.T) {
	const identity = "identity-a"
	initial := memoryBatchTestState(memoryBatchTestStoredFact("Davide", "lives_in", "Torino", "run-old"))
	backend := newMemoryBatchFakeBackend(identity, initial)
	request := MemoryBatchRequest{IdempotencyKey: "invalid-1", Operations: []MemoryBatchOperation{
		memoryBatchTestUpsert("Davide", "likes", "Coffee", "run-new"),
		{Type: MemoryBatchForget, Forget: &ForgetFilter{Subject: "Missing"}},
	}}

	_, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity), request, now,
		defaultMemoryLimits, backend,
	)
	var batchErr *MemoryBatchError
	if !errors.As(err, &batchErr) {
		t.Fatalf("error = %v, want MemoryBatchError", err)
	}
	if batchErr.Index != 1 || batchErr.Code != "target_not_found" {
		t.Fatalf("first error = %+v", batchErr)
	}
	if got := backend.snapshot(identity); !reflect.DeepEqual(got, initial) {
		t.Fatalf("failed batch changed live state:\n got: %+v\nwant: %+v", got, initial)
	}
}

func TestMemoryBatch_IdempotentReplay(t *testing.T) {
	const identity = "identity-a"
	backend := newMemoryBatchFakeBackend(identity, memoryBatchTestState())
	request := MemoryBatchRequest{IdempotencyKey: "replay-1", Operations: []MemoryBatchOperation{
		memoryBatchTestUpsert("Davide", "likes", "Coffee", "run-new"),
	}}
	first, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity), request, now,
		defaultMemoryLimits, backend,
	)
	if err != nil {
		t.Fatalf("first batch: %v", err)
	}
	second, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity), request, now.Add(time.Minute),
		defaultMemoryLimits, backend,
	)
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if first.RequestHash == "" || second.RequestHash != first.RequestHash || !second.Replayed {
		t.Fatalf("first=%+v second=%+v", first, second)
	}
	if len(backend.snapshot(identity).Facts) != 1 || backend.commits != 2 {
		t.Fatalf("replay duplicated effect: facts=%d commits=%d", len(backend.snapshot(identity).Facts), backend.commits)
	}
}
