package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/retention"
)

func TestRetentionHandlerUsesSharedPlanApplyEngine(t *testing.T) {
	engine := &fakeScheduledRetention{
		plan:   retention.Plan{Token: strings.Repeat("a", 64)},
		report: retention.ApplyReport{Completed: 2, Bytes: 42, Retryable: 1},
	}
	handler := NewRetentionHandler(engine)
	if meta := handler.Meta(); meta.Kind != KindRetentionSweep || meta.MaxDuration != retentionMaxDuration || meta.ReschedulesOnRecovery {
		t.Fatalf("Meta() = %+v", meta)
	}
	summary, err := handler.Run(context.Background(), Job{})
	if err != nil {
		t.Fatal(err)
	}
	if engine.token != engine.plan.Token || !strings.Contains(summary, "completed 2") || !strings.Contains(summary, "42 byte") {
		t.Fatalf("token/summary = %q / %q", engine.token, summary)
	}
}

func TestRetentionHandlerSweepsOwnerExportsAndRetriesFailure(t *testing.T) {
	sweeper := &fakeOwnerExportSweeper{deleted: 3}
	summary, err := NewRetentionHandler(nil, sweeper).Run(context.Background(), Job{})
	if err != nil || !strings.Contains(summary, "owner exports deleted 3") || sweeper.now.IsZero() {
		t.Fatalf("Run() = %q, %v; sweep now=%s", summary, err, sweeper.now)
	}

	want := errors.New("object store unavailable")
	sweeper.err = want
	if _, err := NewRetentionHandler(nil, sweeper).Run(context.Background(), Job{}); !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want retryable sweep failure", err)
	}
}

func TestRetentionHandlerSweepsOwnerExportsWhenPlanFails(t *testing.T) {
	planErr := errors.New("candidate query unavailable")
	engine := &fakeScheduledRetention{planErr: planErr}
	sweeper := &fakeOwnerExportSweeper{deleted: 2}

	summary, err := NewRetentionHandler(engine, sweeper).Run(context.Background(), Job{})
	if !errors.Is(err, planErr) {
		t.Fatalf("Run() error = %v, want plan failure", err)
	}
	if sweeper.calls != 1 || sweeper.now.IsZero() || engine.applyCalls != 0 {
		t.Fatalf("plan failure calls: sweeper=%d apply=%d now=%s", sweeper.calls, engine.applyCalls, sweeper.now)
	}
	if !strings.Contains(summary, "owner exports deleted 2") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestRetentionHandlerSweepsOwnerExportsWhenApplyFails(t *testing.T) {
	applyErr := errors.New("retention claim unavailable")
	exportErr := errors.New("owner export store unavailable")
	engine := &fakeScheduledRetention{
		plan: retention.Plan{Token: strings.Repeat("b", 64)}, applyErr: applyErr,
	}
	failing := &fakeOwnerExportSweeper{err: exportErr}
	succeeding := &fakeOwnerExportSweeper{deleted: 4}

	summary, err := NewRetentionHandler(engine, failing, succeeding).Run(context.Background(), Job{})
	if !errors.Is(err, applyErr) || !errors.Is(err, exportErr) {
		t.Fatalf("Run() error = %v, want joined apply and export failures", err)
	}
	if engine.applyCalls != 1 || failing.calls != 1 || succeeding.calls != 1 {
		t.Fatalf("apply failure calls: apply=%d failing sweep=%d succeeding sweep=%d",
			engine.applyCalls, failing.calls, succeeding.calls)
	}
	if !strings.Contains(summary, "owner exports deleted 4") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestRetentionHandlerNilEngineIsDisabled(t *testing.T) {
	summary, err := NewRetentionHandler(nil).Run(context.Background(), Job{})
	if err != nil || !strings.Contains(summary, "disabled") {
		t.Fatalf("Run() = %q, %v", summary, err)
	}
}

type fakeScheduledRetention struct {
	plan       retention.Plan
	report     retention.ApplyReport
	planErr    error
	applyErr   error
	token      string
	applyCalls int
}

type fakeOwnerExportSweeper struct {
	deleted int
	err     error
	now     time.Time
	calls   int
}

func (f *fakeOwnerExportSweeper) SweepExpired(_ context.Context, now time.Time) (int, error) {
	f.calls++
	f.now = now
	return f.deleted, f.err
}

func (f *fakeScheduledRetention) Plan(context.Context) (retention.Plan, error) {
	return f.plan, f.planErr
}

func (f *fakeScheduledRetention) Apply(_ context.Context, token string) (retention.ApplyReport, error) {
	f.applyCalls++
	f.token = token
	return f.report, f.applyErr
}
