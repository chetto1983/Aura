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
	fixture.engine.Source = staticSource(nil)
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

func TestEngineClassifiesRevalidationAndRemovalOutcomes(t *testing.T) {
	tests := []struct {
		name           string
		revalidation   *Revalidation
		revalidateErr  error
		removeErr      error
		wantCompleted  int
		wantRetryable  int
		wantFailed     int
		wantClass      string
		wantRemoveCall int
	}{
		{name: "external state absent", revalidation: &Revalidation{Exists: false, Owned: true, Version: 1}, wantCompleted: 1},
		{name: "ownership changed", revalidation: &Revalidation{Exists: true, Owned: false, Version: 1}, wantFailed: 1, wantClass: "ownership_mismatch"},
		{name: "version changed", revalidation: &Revalidation{Exists: true, Owned: true, Version: 2}, wantRetryable: 1, wantClass: "version_changed"},
		{name: "revalidation unavailable", revalidateErr: errors.New("unavailable"), wantRetryable: 1, wantClass: "revalidate_unavailable"},
		{name: "external removal unavailable", removeErr: errors.New("unavailable"), wantRetryable: 1, wantClass: "external_unavailable", wantRemoveCall: 1},
		{name: "replacement after revalidation", removeErr: ErrVersionConflict, wantRetryable: 1, wantClass: "version_changed", wantRemoveCall: 1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newEngineFixture(t)
			fixture.revalidator.response = tc.revalidation
			fixture.revalidator.err = tc.revalidateErr
			fixture.remover.err = tc.removeErr
			plan, err := fixture.engine.Plan(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			report, err := fixture.engine.Apply(context.Background(), plan.Token)
			if err != nil {
				t.Fatal(err)
			}
			if report.Completed != tc.wantCompleted || report.Retryable != tc.wantRetryable || report.Failed != tc.wantFailed {
				t.Fatalf("Apply report = %+v", report)
			}
			if tc.wantClass != "" && report.FailureClasses[tc.wantClass] != 1 {
				t.Fatalf("failure classes = %+v", report.FailureClasses)
			}
			if fixture.remover.calls != tc.wantRemoveCall {
				t.Fatalf("remove calls = %d, want %d", fixture.remover.calls, tc.wantRemoveCall)
			}
		})
	}
}

func TestEngineRejectsIncompleteConfigurationAndSourceFailure(t *testing.T) {
	if _, err := (&Engine{}).Plan(context.Background()); err == nil {
		t.Fatal("unconfigured engine planned")
	}
	fixture := newEngineFixture(t)
	fixture.engine.WorkerID = ""
	if _, err := fixture.engine.Plan(context.Background()); err == nil {
		t.Fatal("workerless engine planned")
	}
	fixture = newEngineFixture(t)
	fixture.engine.Policy.Version = ""
	if _, err := fixture.engine.Plan(context.Background()); err == nil {
		t.Fatal("invalid policy planned")
	}
	fixture = newEngineFixture(t)
	fixture.engine.Source = failingSource{}
	if _, err := fixture.engine.Plan(context.Background()); err == nil {
		t.Fatal("source failure planned")
	}
	if _, err := fixture.engine.Apply(context.Background(), "token"); !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("missing persisted token error = %v", err)
	}
}

func TestEngineFirstApplyFailsClosedWhenCandidateScanFails(t *testing.T) {
	fixture := newEngineFixture(t)
	plan, err := fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.Source = failingSource{}
	if _, err := fixture.engine.Apply(context.Background(), plan.Token); err == nil {
		t.Fatal("first apply succeeded without current candidate authorization")
	}
	if fixture.store.claims != 0 || len(*fixture.effects) != 0 || fixture.store.op.Status != StatusPlanned {
		t.Fatalf("failed authorization claims/effects/status = %d/%v/%s",
			fixture.store.claims, *fixture.effects, fixture.store.op.Status)
	}
}

func TestEngineRejectsFirstApplyPlanDrift(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*engineFixture, Candidate)
	}{
		{name: "policy version", mutate: func(f *engineFixture, _ Candidate) { f.engine.Policy.Version = "retention-v2" }},
		{name: "ttl removes member", mutate: func(f *engineFixture, original Candidate) {
			f.engine.Policy.TTL[ClassTemporary] = 48 * time.Hour
			f.engine.Source = sourceFunc(func(_ context.Context, policy Policy, _ time.Time) ([]Candidate, error) {
				if policy.TTL[ClassTemporary] >= 48*time.Hour {
					return nil, nil
				}
				return []Candidate{original}, nil
			})
		}},
		{name: "member removed", mutate: func(f *engineFixture, _ Candidate) { f.engine.Source = staticSource(nil) }},
		{name: "member added", mutate: func(f *engineFixture, original Candidate) {
			added := original
			added.ArtifactID = "artifact-two"
			f.engine.Source = staticSource{original, added}
		}},
		{name: "action changed", mutate: func(f *engineFixture, original Candidate) {
			original.Action = ActionDeleteMetadata
			f.engine.Source = staticSource{original}
		}},
		{name: "version changed", mutate: func(f *engineFixture, original Candidate) {
			original.Version++
			f.engine.Source = staticSource{original}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fixture := newEngineFixture(t)
			original := fixture.engine.Source.(staticSource)[0]
			plan, err := fixture.engine.Plan(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(fixture, original)
			if _, err := fixture.engine.Apply(context.Background(), plan.Token); !errors.Is(err, ErrTokenMismatch) {
				t.Fatalf("drift Apply error = %v, want token mismatch", err)
			}
			if fixture.store.claims != 0 || fixture.store.authorizations != 0 || len(*fixture.effects) != 0 || fixture.store.op.Status != StatusPlanned {
				t.Fatalf("drift mutated state claims/auth/effects/status=%d/%d/%v/%s",
					fixture.store.claims, fixture.store.authorizations, *fixture.effects, fixture.store.op.Status)
			}
		})
	}
}

func TestEngineRejectsExpiredPlanBeforeFirstClaim(t *testing.T) {
	fixture := newEngineFixture(t)
	plan, err := fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	created := fixture.store.op.CreatedAt
	fixture.engine.Now = func() time.Time { return created.Add(defaultPlanValidity) }
	if _, err := fixture.engine.Apply(context.Background(), plan.Token); !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("expired Apply error = %v, want token mismatch", err)
	}
	if fixture.store.claims != 0 || fixture.store.authorizations != 0 || len(*fixture.effects) != 0 {
		t.Fatalf("expired plan claims/auth/effects=%d/%d/%v", fixture.store.claims, fixture.store.authorizations, *fixture.effects)
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

type retentionItemMetric struct {
	state   string
	outcome string
	count   int64
}

type recordingRetentionMetrics struct {
	items []retentionItemMetric
	bytes int64
}

func (r *recordingRetentionMetrics) recordItems(_ context.Context, state, outcome string, count int64) {
	r.items = append(r.items, retentionItemMetric{state: state, outcome: outcome, count: count})
}

func (r *recordingRetentionMetrics) recordBytes(_ context.Context, bytes int64) {
	r.bytes += bytes
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

type sourceFunc func(context.Context, Policy, time.Time) ([]Candidate, error)

func (f sourceFunc) Candidates(ctx context.Context, policy Policy, now time.Time) ([]Candidate, error) {
	return f(ctx, policy, now)
}

type fakeRevalidator struct {
	version  int64
	evidence ActivityEvidence
	err      error
	response *Revalidation
}

func (f *fakeRevalidator) Revalidate(context.Context, Candidate) (Revalidation, error) {
	if f.response != nil {
		return *f.response, f.err
	}
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
	mu                      sync.Mutex
	plan                    Plan
	op                      Operation
	items                   []Item
	claims                  int
	authorizations          int
	saveErr                 error
	authorizeErr            error
	authorizeAllowed        *bool
	authorizeObservedStatus Status
	authorizeHook           func()
	claimErr                error
	claimHook               func()
	reloadErr               error
	getByTokenCalls         int
	recordErr               error
	finalizeItemErr         error
	retryErr                error
	failErr                 error
	finalizeOperationErr    error
}

func (m *memoryOperationStore) SavePlan(_ context.Context, plan Plan, now time.Time) (Operation, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.saveErr != nil {
		return Operation{}, false, m.saveErr
	}
	if m.op.ID != "" {
		return m.op, false, nil
	}
	m.plan = plan
	m.op = Operation{ID: "operation", Token: plan.Token, PolicyVersion: plan.PolicyVersion, Status: StatusPlanned, CandidateCount: len(plan.Candidates), PlannedBytes: plan.TotalBytes, CreatedAt: now}
	for i, candidate := range plan.Candidates {
		m.items = append(m.items, Item{ID: string(rune('a' + i)), OperationID: m.op.ID, Candidate: candidate, Status: StatusPlanned, ArtifactResult: ArtifactPending})
	}
	return m.op, true, nil
}

func (m *memoryOperationStore) Authorize(_ context.Context, operationID, token, policyVersion string, _ time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.authorizeErr != nil {
		return false, m.authorizeErr
	}
	if m.authorizeAllowed != nil && !*m.authorizeAllowed {
		if m.authorizeObservedStatus != "" {
			m.op.Status = m.authorizeObservedStatus
		}
		return false, nil
	}
	if m.op.ID != operationID || m.op.Token != token || m.op.PolicyVersion != policyVersion || m.op.Status != StatusPlanned {
		return false, nil
	}
	m.authorizations++
	m.op.Status = StatusDeleting
	if m.authorizeHook != nil {
		m.authorizeHook()
	}
	return true, nil
}

func (m *memoryOperationStore) GetByToken(_ context.Context, token string) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.getByTokenCalls++
	if m.getByTokenCalls > 1 && m.reloadErr != nil {
		return Operation{}, m.reloadErr
	}
	if token != m.op.Token {
		return Operation{}, ErrTokenMismatch
	}
	return m.op, nil
}

func (m *memoryOperationStore) Claim(_ context.Context, operationID, owner string, cap int, now time.Time, lease time.Duration) ([]Item, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.claimErr != nil {
		return nil, m.claimErr
	}
	if m.claimHook != nil {
		m.claimHook()
	}
	m.claims++
	if m.op.ID != operationID || (m.op.Status != StatusDeleting && m.op.Status != StatusRetryable) {
		return nil, nil
	}
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
	if m.recordErr != nil {
		return m.recordErr
	}
	item := m.item(id)
	item.ArtifactResult, item.RemovedBytes, item.FailureClass = result, bytes, failure
	return nil
}

func (m *memoryOperationStore) FinalizeItem(_ context.Context, id, owner string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.finalizeItemErr != nil {
		return m.finalizeItemErr
	}
	m.item(id).Status = StatusCompleted
	return nil
}

func (m *memoryOperationStore) RetryItem(_ context.Context, id, owner, failure string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.retryErr != nil {
		return m.retryErr
	}
	item := m.item(id)
	item.Status, item.FailureClass = StatusRetryable, failure
	return nil
}

func (m *memoryOperationStore) FailItem(_ context.Context, id, owner, failure string, _ time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failErr != nil {
		return m.failErr
	}
	item := m.item(id)
	item.Status, item.FailureClass = StatusFailed, failure
	return nil
}

func (m *memoryOperationStore) FinalizeOperation(_ context.Context, id string, _ time.Time) (Operation, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.finalizeOperationErr != nil {
		return Operation{}, m.finalizeOperationErr
	}
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

type failingSource struct{}

func (failingSource) Candidates(context.Context, Policy, time.Time) ([]Candidate, error) {
	return nil, errors.New("candidate source unavailable")
}

type recordingAudit struct{ summaries []AuditSummary }

func (r *recordingAudit) Record(_ context.Context, summary AuditSummary) {
	r.summaries = append(r.summaries, summary)
}
