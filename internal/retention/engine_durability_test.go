package retention

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestEnginePlanCancellationAndDurabilityFailure(t *testing.T) {
	fixture := newEngineFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fixture.engine.Plan(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Plan error = %v", err)
	}
	fixture = newEngineFixture(t)
	wantErr := errors.New("save plan unavailable")
	fixture.store.saveErr = wantErr
	if _, err := fixture.engine.Plan(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("SavePlan error = %v", err)
	}
}

func TestEngineApplyDeadlineAndDurabilityFailures(t *testing.T) {
	if _, err := (&Engine{}).Apply(context.Background(), "token"); err == nil {
		t.Fatal("unconfigured Apply succeeded")
	}
	fixture := newEngineFixture(t)
	if _, err := fixture.engine.Apply(context.Background(), ""); !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("empty-token Apply error = %v", err)
	}

	fixture = newEngineFixture(t)
	base := time.Date(2026, 7, 22, 8, 0, 0, 0, time.UTC)
	calls := 0
	fixture.engine.Now = func() time.Time {
		calls++
		if calls >= 3 {
			return base.Add(2 * fixture.engine.Policy.MaxRunDuration)
		}
		return base
	}
	plan, err := fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.Apply(context.Background(), plan.Token); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline Apply error = %v", err)
	}

	wantErr := errors.New("durability unavailable")
	tests := map[string]func(*engineFixture){
		"authorize":          func(f *engineFixture) { f.store.authorizeErr = wantErr },
		"claim":              func(f *engineFixture) { f.store.claimErr = wantErr },
		"finalize operation": func(f *engineFixture) { f.store.finalizeOperationErr = wantErr },
		"retry": func(f *engineFixture) {
			f.revalidator.version = 2
			f.store.retryErr = wantErr
		},
		"fail": func(f *engineFixture) {
			f.revalidator.response = &Revalidation{Exists: true, Owned: false, Version: 1}
			f.store.failErr = wantErr
		},
		"absent record": func(f *engineFixture) {
			f.revalidator.response = &Revalidation{Exists: false, Owned: true, Version: 1}
			f.store.recordErr = wantErr
		},
		"absent finalize": func(f *engineFixture) {
			f.revalidator.response = &Revalidation{Exists: false, Owned: true, Version: 1}
			f.store.finalizeItemErr = wantErr
		},
		"removal failure record": func(f *engineFixture) {
			f.remover.err = errors.New("external unavailable")
			f.store.recordErr = wantErr
		},
		"removal success record": func(f *engineFixture) { f.store.recordErr = wantErr },
		"metadata finalize":      func(f *engineFixture) { f.store.finalizeItemErr = wantErr },
	}
	for name, configure := range tests {
		t.Run(name, func(t *testing.T) {
			fixture := newEngineFixture(t)
			plan, err := fixture.engine.Plan(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			configure(fixture)
			if _, err := fixture.engine.Apply(context.Background(), plan.Token); !errors.Is(err, wantErr) {
				t.Fatalf("Apply error = %v, want %v", err, wantErr)
			}
		})
	}

	fixture = newEngineFixture(t)
	plan, err = fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	denied := false
	fixture.store.authorizeAllowed = &denied
	if _, err := fixture.engine.Apply(context.Background(), plan.Token); !errors.Is(err, ErrTokenMismatch) {
		t.Fatalf("lost authorization Apply error = %v", err)
	}

	fixture = newEngineFixture(t)
	plan, err = fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	fixture.engine.Source = staticSource{{Action: ActionDeleteArtifact}}
	if _, err := fixture.engine.Apply(context.Background(), plan.Token); err == nil {
		t.Fatal("Apply authorized an invalid rebuilt plan")
	}

	fixture = newEngineFixture(t)
	plan, err = fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	denied = false
	fixture.store.authorizeAllowed = &denied
	fixture.store.reloadErr = wantErr
	if _, err := fixture.engine.Apply(context.Background(), plan.Token); !errors.Is(err, wantErr) {
		t.Fatalf("authorization reload error = %v", err)
	}

	fixture = newEngineFixture(t)
	plan, err = fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	denied = false
	fixture.store.authorizeAllowed = &denied
	fixture.store.authorizeObservedStatus = StatusDeleting
	if _, err := fixture.engine.Apply(context.Background(), plan.Token); err != nil {
		t.Fatalf("concurrent authorization resume failed: %v", err)
	}

	fixture = newEngineFixture(t)
	plan, err = fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	loopCtx, loopCancel := context.WithCancel(context.Background())
	fixture.store.authorizeHook = loopCancel
	if _, err := fixture.engine.Apply(loopCtx, plan.Token); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-authorization cancellation error = %v", err)
	}

	fixture = newEngineFixture(t)
	plan, err = fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	itemCtx, itemCancel := context.WithCancel(context.Background())
	fixture.store.claimHook = itemCancel
	if _, err := fixture.engine.Apply(itemCtx, plan.Token); !errors.Is(err, context.Canceled) {
		t.Fatalf("post-claim cancellation error = %v", err)
	}
}

func TestEngineRecordsContentFreePlanAndApplyAudits(t *testing.T) {
	fixture := newEngineFixture(t)
	audit := &recordingAudit{}
	fixture.engine.Audit = audit
	fixture.engine.Now = nil
	plan, err := fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.Apply(context.Background(), plan.Token); err != nil {
		t.Fatal(err)
	}
	if len(audit.summaries) != 2 || audit.summaries[0].Mode != "plan" || audit.summaries[1].Mode != "apply" {
		t.Fatalf("audit summaries = %+v", audit.summaries)
	}
	recordRetentionBytes(context.Background(), 0)
}

func TestEngineRecordsPendingAndTerminalRetentionItems(t *testing.T) {
	fixture := newEngineFixture(t)
	metrics := &recordingRetentionMetrics{}
	fixture.engine.metrics = metrics

	plan, err := fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.engine.Apply(context.Background(), plan.Token); err != nil {
		t.Fatal(err)
	}

	want := []retentionItemMetric{
		{state: "pending", outcome: "accepted", count: 1},
		{state: "completed", outcome: "success", count: 1},
	}
	if !slices.Equal(metrics.items, want) {
		t.Fatalf("retention item metrics = %+v, want %+v", metrics.items, want)
	}
}

func TestEngineDoesNotDoubleCountPendingOnIdempotentPlanReplay(t *testing.T) {
	fixture := newEngineFixture(t)
	metrics := &recordingRetentionMetrics{}
	fixture.engine.metrics = metrics

	first, err := fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixture.engine.Plan(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if second.Token != first.Token {
		t.Fatalf("replayed plan token = %q, want %q", second.Token, first.Token)
	}
	want := []retentionItemMetric{{state: "pending", outcome: "accepted", count: 1}}
	if !slices.Equal(metrics.items, want) {
		t.Fatalf("replayed plan metrics = %+v, want one durable transition %+v", metrics.items, want)
	}
}

func TestEngineKeepsRetryableItemsInBacklogAndClosesFailedItems(t *testing.T) {
	t.Run("retryable", func(t *testing.T) {
		fixture := newEngineFixture(t)
		fixture.revalidator.err = errors.New("temporarily unavailable")
		metrics := &recordingRetentionMetrics{}
		fixture.engine.metrics = metrics
		plan, err := fixture.engine.Plan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.engine.Apply(context.Background(), plan.Token); err != nil {
			t.Fatal(err)
		}
		if len(metrics.items) != 1 || metrics.items[0].state != "pending" {
			t.Fatalf("retryable metrics = %+v, want pending only", metrics.items)
		}
	})

	t.Run("terminal failure", func(t *testing.T) {
		fixture := newEngineFixture(t)
		fixture.revalidator.response = &Revalidation{Exists: true, Owned: false, Version: 1}
		metrics := &recordingRetentionMetrics{}
		fixture.engine.metrics = metrics
		plan, err := fixture.engine.Plan(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fixture.engine.Apply(context.Background(), plan.Token); err != nil {
			t.Fatal(err)
		}
		want := []retentionItemMetric{
			{state: "pending", outcome: "accepted", count: 1},
			{state: "completed", outcome: "error", count: 1},
		}
		if !slices.Equal(metrics.items, want) {
			t.Fatalf("terminal failure metrics = %+v, want %+v", metrics.items, want)
		}
	})
}
