package retention

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestEnginePlanIsDryAndApplyRequiresExactToken(t *testing.T) {
	fixture := newEngineFixture(t)
	plan, err := fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(*fixture.effects) != 0 {
		t.Fatalf("dry-run effects = %v", *fixture.effects)
	}
	if _, err := fixture.engine.Apply(context.Background(), "wrong"); !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("wrong token error = %v", err)
	}
	if len(*fixture.effects) != 0 {
		t.Fatalf("wrong-token effects = %v", *fixture.effects)
	}
	report, err := fixture.engine.Apply(context.Background(), plan.Token)
	if err != nil {
		t.Fatal(err)
	}
	if report.Completed != 1 || !slices.Equal(*fixture.effects, []string{"remove", "metadata"}) {
		t.Fatalf("apply report/effects = %+v / %v", report, *fixture.effects)
	}
}

func TestEngineRevalidatesActivityImmediatelyBeforeRemoval(t *testing.T) {
	fixture := newEngineFixture(t)
	plan, err := fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fixture.revalidator.evidence.PendingApproval = true
	report, err := fixture.engine.Apply(context.Background(), plan.Token)
	if err != nil {
		t.Fatal(err)
	}
	if report.Retryable != 1 || report.FailureClasses[ActivityPendingApproval] != 1 {
		t.Fatalf("report = %+v", report)
	}
	if len(*fixture.effects) != 0 {
		t.Fatalf("protected candidate effects = %v", *fixture.effects)
	}
}

func TestEngineCrashAfterRemovalDoesNotRepeatDestructiveStep(t *testing.T) {
	fixture := newEngineFixture(t)
	fixture.finalizer.failOnce = errors.New("metadata unavailable")
	plan, err := fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	first, err := fixture.engine.Apply(context.Background(), plan.Token)
	if err != nil {
		t.Fatal(err)
	}
	if first.Retryable != 1 || fixture.remover.calls != 1 {
		t.Fatalf("first apply = %+v, removes=%d", first, fixture.remover.calls)
	}
	second, err := fixture.engine.Apply(context.Background(), plan.Token)
	if err != nil {
		t.Fatal(err)
	}
	if second.Completed != 1 || fixture.remover.calls != 1 {
		t.Fatalf("second apply = %+v, removes=%d", second, fixture.remover.calls)
	}
}

func TestEngineAlreadyAbsentIsIdempotentSuccess(t *testing.T) {
	fixture := newEngineFixture(t)
	fixture.remover.result = RemovalResult{Absent: true}
	plan, _ := fixture.engine.Plan(context.Background())
	report, err := fixture.engine.Apply(context.Background(), plan.Token)
	if err != nil || report.Completed != 1 {
		t.Fatalf("Apply() = %+v, %v", report, err)
	}
	if fixture.store.items[0].ArtifactResult != ArtifactAbsent {
		t.Fatalf("artifact result = %q", fixture.store.items[0].ArtifactResult)
	}
}

func TestEngineCancellationBeforeMarkHasNoMutation(t *testing.T) {
	fixture := newEngineFixture(t)
	plan, _ := fixture.engine.Plan(context.Background())
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	before := fixture.store.claims
	if _, err := fixture.engine.Apply(ctx, plan.Token); !errors.Is(err, context.Canceled) {
		t.Fatalf("Apply error = %v", err)
	}
	if fixture.store.claims != before || len(*fixture.effects) != 0 {
		t.Fatalf("canceled apply claims/effects = %d / %v", fixture.store.claims, *fixture.effects)
	}
}

type engineFixture struct {
	engine      *Engine
	store       *memoryOperationStore
	revalidator *fakeRevalidator
	remover     *fakeRemover
	finalizer   *fakeFinalizer
	effects     *[]string
}

func newEngineFixture(t *testing.T) *engineFixture {
	t.Helper()
	now := time.Date(2026, 7, 21, 12, 0, 0, 0, time.UTC)
	candidate := Candidate{IdentityID: "owner", ConversationID: "conversation", ArtifactID: "artifact", Version: 1, Action: ActionDeleteArtifact, Class: ClassTemporary, Bytes: 10}
	store := &memoryOperationStore{}
	effects := []string{}
	revalidator := &fakeRevalidator{version: 1}
	remover := &fakeRemover{effects: &effects, result: RemovalResult{Bytes: 10}}
	finalizer := &fakeFinalizer{effects: &effects}
	policy := DefaultPolicy(EnvironmentProduction)
	policy.BatchSize = 1
	return &engineFixture{
		store: store, revalidator: revalidator, remover: remover, finalizer: finalizer, effects: &effects,
		engine: &Engine{
			Policy: policy, Source: staticSource{candidate}, Store: store,
			Revalidator: revalidator, Remover: remover, Finalizer: finalizer,
			WorkerID: "worker", ActivityFreshness: time.Minute, Now: func() time.Time { return now },
		},
	}
}

type staticSource []Candidate

func (s staticSource) Candidates(context.Context, Policy, time.Time) ([]Candidate, error) {
	return slices.Clone(s), nil
}

type fakeRevalidator struct {
	version  int64
	evidence ActivityEvidence
	err      error
}

func (f *fakeRevalidator) Revalidate(context.Context, Candidate) (Revalidation, error) {
	return Revalidation{Exists: true, Owned: true, Version: f.version, Activity: f.evidence}, f.err
}

type fakeRemover struct {
	effects *[]string
	result  RemovalResult
	err     error
	calls   int
}

func (f *fakeRemover) Remove(context.Context, Candidate) (RemovalResult, error) {
	f.calls++
	*f.effects = append(*f.effects, "remove")
	return f.result, f.err
}

type fakeFinalizer struct {
	effects  *[]string
	failOnce error
}

func (f *fakeFinalizer) Finalize(context.Context, Candidate) error {
	*f.effects = append(*f.effects, "metadata")
	if f.failOnce != nil {
		err := f.failOnce
		f.failOnce = nil
		return err
	}
	return nil
}

type memoryOperationStore struct {
	mu     sync.Mutex
	plan   Plan
	op     Operation
	items  []Item
	claims int
}

func (m *memoryOperationStore) SavePlan(_ context.Context, plan Plan, _ time.Time) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.op.ID != "" {
		return m.op, nil
	}
	m.plan = plan
	m.op = Operation{ID: "operation", Token: plan.Token, PolicyVersion: plan.PolicyVersion, Status: StatusPlanned, CandidateCount: len(plan.Candidates), PlannedBytes: plan.TotalBytes}
	for i, candidate := range plan.Candidates {
		m.items = append(m.items, Item{ID: string(rune('a' + i)), OperationID: m.op.ID, Candidate: candidate, Status: StatusPlanned, ArtifactResult: ArtifactPending})
	}
	return m.op, nil
}

func (m *memoryOperationStore) GetByToken(_ context.Context, token string) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if token != m.op.Token {
		return Operation{}, ErrTokenMismatch
	}
	return m.op, nil
}

func (m *memoryOperationStore) Claim(_ context.Context, operationID, owner string, cap int, now time.Time, lease time.Duration) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.claims++
	var out []Item
	for i := range m.items {
		item := &m.items[i]
		if len(out) == cap {
			break
		}
		if item.OperationID != operationID || (item.Status != StatusPlanned && item.Status != StatusRetryable) {
			continue
		}
		item.Status, item.ClaimOwner, item.LeaseExpiresAt = StatusDeleting, owner, now.Add(lease)
		out = append(out, *item)
	}
	return out, nil
}

func (m *memoryOperationStore) RecordArtifact(_ context.Context, id, owner, result string, bytes int64, failure string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.item(id)
	item.ArtifactResult, item.RemovedBytes, item.FailureClass = result, bytes, failure
	return nil
}

func (m *memoryOperationStore) FinalizeItem(_ context.Context, id, owner string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.item(id).Status = StatusCompleted
	return nil
}

func (m *memoryOperationStore) RetryItem(_ context.Context, id, owner, failure string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.item(id)
	item.Status, item.FailureClass = StatusRetryable, failure
	return nil
}

func (m *memoryOperationStore) FailItem(_ context.Context, id, owner, failure string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	item := m.item(id)
	item.Status, item.FailureClass = StatusFailed, failure
	return nil
}

func (m *memoryOperationStore) FinalizeOperation(_ context.Context, id string, _ time.Time) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.op.Status, m.op.CompletedCount, m.op.CompletedBytes, m.op.FailureCount = StatusCompleted, 0, 0, 0
	for _, item := range m.items {
		switch item.Status {
		case StatusCompleted:
			m.op.CompletedCount++
			m.op.CompletedBytes += item.RemovedBytes
		case StatusRetryable:
			m.op.Status = StatusRetryable
			m.op.FailureCount++
		case StatusFailed:
			m.op.Status = StatusFailed
			m.op.FailureCount++
		default:
			m.op.Status = StatusRetryable
		}
	}
	return m.op, nil
}

func (m *memoryOperationStore) item(id string) *Item {
	for i := range m.items {
		if m.items[i].ID == id {
			return &m.items[i]
		}
	}
	panic("missing fake item")
}
