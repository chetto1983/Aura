package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"reflect"
	"sync"
	"testing"
	"time"
)

type memoryBatchFakeBackend struct {
	mu                    sync.Mutex
	states                map[string]memoryBatchState
	receipts              map[string]map[string]memoryBatchReceipt
	commits               int
	mutatingCommits       int
	commitAttempts        int
	commitConflicts       int
	ambiguousCommits      int
	conflictStateMutation func(memoryBatchState) memoryBatchState
	persistErr            error
	beforeCommit          chan struct{}
	allowCommit           chan struct{}
	blockCommitOnce       sync.Once
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
	dirty    bool
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
	if tx.backend.persistErr != nil {
		return tx.backend.persistErr
	}
	tx.state = cloneMemoryBatchTestState(after)
	tx.dirty = true
	return nil
}

func (tx *memoryBatchFakeTx) SaveReceipt(_ context.Context, key string, receipt memoryBatchReceipt) error {
	if tx.receipts == nil {
		tx.receipts = map[string]memoryBatchReceipt{}
	}
	tx.receipts[key] = receipt
	tx.dirty = true
	return nil
}

func (tx *memoryBatchFakeTx) Commit(context.Context) error {
	if tx.closed {
		return errors.New("transaction already closed")
	}
	tx.backend.blockCommitOnce.Do(func() {
		if tx.backend.beforeCommit != nil {
			close(tx.backend.beforeCommit)
		}
		if tx.backend.allowCommit != nil {
			<-tx.backend.allowCommit
		}
	})
	tx.backend.mu.Lock()
	defer tx.backend.mu.Unlock()
	tx.backend.commitAttempts++
	if tx.backend.commitConflicts > 0 {
		tx.backend.commitConflicts--
		if tx.backend.conflictStateMutation != nil {
			tx.backend.states[tx.identity] = tx.backend.conflictStateMutation(
				cloneMemoryBatchTestState(tx.backend.states[tx.identity]))
		}
		tx.closed = true
		return &ServerError{Status: http.StatusServiceUnavailable, Detail: "please retry transaction"}
	}
	tx.publishLocked()
	if tx.backend.ambiguousCommits > 0 {
		tx.backend.ambiguousCommits--
		tx.closed = true
		return errors.New("commit response lost after server applied transaction")
	}
	tx.closed = true
	return nil
}

func (tx *memoryBatchFakeTx) publishLocked() {
	tx.backend.states[tx.identity] = cloneMemoryBatchTestState(tx.state)
	if tx.backend.receipts[tx.identity] == nil {
		tx.backend.receipts[tx.identity] = map[string]memoryBatchReceipt{}
	}
	maps.Copy(tx.backend.receipts[tx.identity], tx.receipts)
	tx.backend.commits++
	if tx.dirty {
		tx.backend.mutatingCommits++
	}
}

func (tx *memoryBatchFakeTx) Rollback(context.Context) { tx.closed = true }

func cloneMemoryBatchTestState(state memoryBatchState) memoryBatchState {
	clone := memoryBatchState{Entities: map[string]string{}, Facts: map[string]memoryBatchFact{}}
	maps.Copy(clone.Entities, state.Entities)
	for key, fact := range state.Facts {
		fact.Sources = cloneFactSources(fact.Sources)
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
		fact.RID = fmt.Sprintf("#1:%d", i)
		state.Facts[fact.RID] = fact
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
	if backend.commits != 1 {
		t.Fatalf("commits = %d, want one atomic commit", backend.commits)
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

	t.Run("ambiguous commit outcome", func(t *testing.T) {
		ambiguous := newMemoryBatchFakeBackend(identity, memoryBatchTestState())
		ambiguous.ambiguousCommits = 1
		result, err := applyMemoryBatch(
			context.Background(), memoryBatchTestActor(identity), request, now,
			defaultMemoryLimits, ambiguous,
		)
		if err != nil {
			t.Fatalf("ambiguous outcome was not reconciled through its receipt: %v", err)
		}
		if !result.Replayed || len(ambiguous.snapshot(identity).Facts) != 1 {
			t.Fatalf("result=%+v state=%+v", result, ambiguous.snapshot(identity))
		}
		if ambiguous.mutatingCommits != 1 {
			t.Fatalf("mutating commits = %d, want exactly one", ambiguous.mutatingCommits)
		}
	})
}

func TestMemoryBatch_ConflictRetry(t *testing.T) {
	const identity = "identity-a"
	initial := memoryBatchTestState(memoryBatchTestStoredFact("Davide", "likes", "Coffee", "run-old"))
	backend := newMemoryBatchFakeBackend(identity, initial)
	backend.commitConflicts = 1
	backend.conflictStateMutation = func(state memoryBatchState) memoryBatchState {
		for key, fact := range state.Facts {
			fact.Sources = mergeFactSources(fact.Sources, FactSource{
				RunID: "run-concurrent", WriterRole: WriterParent,
			})
			state.Facts[key] = fact
		}
		return state
	}
	request := MemoryBatchRequest{IdempotencyKey: "conflict-1", Operations: []MemoryBatchOperation{
		{Type: MemoryBatchForget, Forget: &ForgetFilter{SourceRunID: "run-old"}},
	}}

	_, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity), request, now,
		defaultMemoryLimits, backend,
	)
	if err != nil {
		t.Fatalf("conflict retry: %v", err)
	}
	final := backend.snapshot(identity)
	if len(final.Facts) != 1 {
		t.Fatalf("retry reused stale deleted state: %+v", final)
	}
	for _, fact := range final.Facts {
		if len(fact.Sources) != 1 || fact.Sources[0].RunID != "run-concurrent" {
			t.Fatalf("retry did not recompile from fresh committed state: %+v", fact.Sources)
		}
	}
	if backend.commitAttempts != 2 || backend.mutatingCommits != 1 {
		t.Fatalf("attempts=%d mutating_commits=%d", backend.commitAttempts, backend.mutatingCommits)
	}
}

func TestMemoryBatch_LateRollback(t *testing.T) {
	const identity = "identity-a"
	initial := memoryBatchTestState(memoryBatchTestStoredFact("Davide", "likes", "Coffee", "run-old"))
	backend := newMemoryBatchFakeBackend(identity, initial)
	lateErr := errors.New("forced late persistence failure")
	backend.persistErr = lateErr
	request := MemoryBatchRequest{IdempotencyKey: "late-1", Operations: []MemoryBatchOperation{
		memoryBatchTestUpsert("Davide", "lives_in", "Caraglio", "run-new"),
	}}

	_, err := applyMemoryBatch(
		context.Background(), memoryBatchTestActor(identity), request, now,
		defaultMemoryLimits, backend,
	)
	if !errors.Is(err, lateErr) {
		t.Fatalf("error = %v, want exact late failure", err)
	}
	if got := backend.snapshot(identity); !reflect.DeepEqual(got, initial) {
		t.Fatalf("late failure changed live state: got=%+v want=%+v", got, initial)
	}
	if backend.commits != 0 {
		t.Fatalf("late failure committed %d times", backend.commits)
	}
}

func TestMemoryBatch_CrossIdentity(t *testing.T) {
	backend := newMemoryBatchFakeBackend("identity-a", memoryBatchTestState())
	backend.states["identity-b"] = memoryBatchTestState()
	for _, test := range []struct {
		identity string
		object   string
	}{
		{identity: "identity-a", object: "Coffee"},
		{identity: "identity-b", object: "Tea"},
	} {
		request := MemoryBatchRequest{IdempotencyKey: "shared-key", Operations: []MemoryBatchOperation{
			memoryBatchTestUpsert("Davide", "likes", test.object, "run-"+test.identity),
		}}
		if _, err := applyMemoryBatch(
			context.Background(), memoryBatchTestActor(test.identity), request, now,
			defaultMemoryLimits, backend,
		); err != nil {
			t.Fatalf("%s batch: %v", test.identity, err)
		}
	}
	for identity, object := range map[string]string{"identity-a": "Coffee", "identity-b": "Tea"} {
		state := backend.snapshot(identity)
		if len(state.Facts) != 1 {
			t.Fatalf("%s facts = %+v", identity, state.Facts)
		}
		for _, fact := range state.Facts {
			if fact.Fact.Object != object {
				t.Fatalf("%s observed foreign object %q", identity, fact.Fact.Object)
			}
		}
	}
	before := backend.snapshot("identity-a")
	_, err := applyMemoryBatch(
		context.Background(), MemoryBatchActor{IdentityID: "identity-a", WriterRole: WriterWorker},
		MemoryBatchRequest{IdempotencyKey: "worker-delete", Operations: []MemoryBatchOperation{
			{Type: MemoryBatchForget, Forget: &ForgetFilter{Subject: "Davide"}},
		}}, now, defaultMemoryLimits, backend,
	)
	var batchErr *MemoryBatchError
	if !errors.As(err, &batchErr) || batchErr.Code != "unauthorized_actor" {
		t.Fatalf("worker destructive error = %v", err)
	}
	if got := backend.snapshot("identity-a"); !reflect.DeepEqual(got, before) {
		t.Fatalf("unauthorized worker changed identity state: got=%+v want=%+v", got, before)
	}
}

func TestMemoryBatch_NoPartialObserver(t *testing.T) {
	const identity = "identity-a"
	initial := memoryBatchTestState(memoryBatchTestStoredFact("Davide", "lives_in", "Torino", "run-old"))
	backend := newMemoryBatchFakeBackend(identity, initial)
	backend.beforeCommit = make(chan struct{})
	backend.allowCommit = make(chan struct{})
	request := MemoryBatchRequest{IdempotencyKey: "observer-1", Operations: []MemoryBatchOperation{
		{Type: MemoryBatchForget, Forget: &ForgetFilter{Subject: "Davide"}},
		memoryBatchTestUpsert("Davide", "lives_in", "Caraglio", "run-new"),
	}}
	result := make(chan error, 1)
	go func() {
		_, err := applyMemoryBatch(
			context.Background(), memoryBatchTestActor(identity), request, now,
			defaultMemoryLimits, backend,
		)
		result <- err
	}()

	select {
	case <-backend.beforeCommit:
	case <-time.After(5 * time.Second):
		t.Fatal("batch did not reach the commit barrier")
	}
	if observed := backend.snapshot(identity); !reflect.DeepEqual(observed, initial) {
		t.Fatalf("observer saw an intermediate state: got=%+v want=%+v", observed, initial)
	}
	close(backend.allowCommit)
	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("batch: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("batch did not finish after commit was released")
	}
	final := backend.snapshot(identity)
	if len(final.Facts) != 1 {
		t.Fatalf("final state = %+v", final)
	}
	for _, fact := range final.Facts {
		if fact.Fact.Object != "Caraglio" {
			t.Fatalf("final object = %q", fact.Fact.Object)
		}
	}
}

func TestMemoryBatch_NativeDateTimeFormat(t *testing.T) {
	parsed, err := parseMemoryBatchTime("2026-08-31 13:00:07")
	if err != nil {
		t.Fatalf("parse native DATETIME: %v", err)
	}
	want := time.Date(2026, 8, 31, 13, 0, 7, 0, time.UTC)
	if !parsed.Equal(want) {
		t.Fatalf("parsed = %s, want %s", parsed, want)
	}
}

func TestMemoryBatchError_Error(t *testing.T) {
	err := &MemoryBatchError{Index: 0, Code: "test_code", Err: fmt.Errorf("test error")}
	if err.Error() != "arcadedb: memory batch operation 0 (test_code): test error; live state unchanged" {
		t.Errorf("Error() = %q, want expected string", err.Error())
	}
	// Test nil error
	var nilErr *MemoryBatchError
	if nilErr.Error() != "" {
		t.Errorf("nil Error() = %q, want empty string", nilErr.Error())
	}
}

func TestMemoryBatchError_Unwrap(t *testing.T) {
	innerErr := fmt.Errorf("inner error")
	err := &MemoryBatchError{Index: 1, Code: "code", Err: innerErr}
	if err.Unwrap() != innerErr {
		t.Errorf("Unwrap() = %v, want %v", err.Unwrap(), innerErr)
	}
	// Test nil error
	var nilErr *MemoryBatchError
	if nilErr.Unwrap() != nil {
		t.Errorf("nil Unwrap() = %v, want nil", nilErr.Unwrap())
	}
}

func TestMemoryBatchEnvelopeError(t *testing.T) {
	innerErr := fmt.Errorf("envelope error")
	err := memoryBatchEnvelopeError("env_code", innerErr)
	if err.Index != -1 {
		t.Errorf("Index = %d, want -1", err.Index)
	}
	if err.Code != "env_code" {
		t.Errorf("Code = %q, want %q", err.Code, "env_code")
	}
	if err.Err != innerErr {
		t.Errorf("Err = %v, want %v", err.Err, innerErr)
	}
}
