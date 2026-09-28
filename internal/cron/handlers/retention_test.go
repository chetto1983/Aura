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

func TestRetentionHandlerRunsSweepersAndRetriesFailure(t *testing.T) {
	sweeper := &fakeRetentionSweeper{deleted: 3}
	summary, err := NewRetentionHandler(nil, sweeper).Run(context.Background(), Job{})
	if err != nil || !strings.Contains(summary, "swept 3 item(s)") || sweeper.now.IsZero() {
		t.Fatalf("Run() = %q, %v; sweep now=%s", summary, err, sweeper.now)
	}

	want := errors.New("object store unavailable")
	sweeper.err = want
	if _, err := NewRetentionHandler(nil, sweeper).Run(context.Background(), Job{}); !errors.Is(err, want) {
		t.Fatalf("Run() error = %v, want retryable sweep failure", err)
	}
}

// The slot carries owner exports and asset deletes today; the summary counts every sweeper,
// and one the composition root could not build is skipped rather than called.
func TestRetentionHandlerSumsEverySweeperAndSkipsAMissingOne(t *testing.T) {
	exports := &fakeRetentionSweeper{deleted: 3}
	deletes := &fakeRetentionSweeper{deleted: 5}
	summary, err := NewRetentionHandler(nil, exports, nil, deletes).Run(context.Background(), Job{})
	if err != nil || !strings.Contains(summary, "swept 8 item(s)") || exports.calls != 1 || deletes.calls != 1 {
		t.Fatalf("Run() = %q, %v; calls exports=%d deletes=%d", summary, err, exports.calls, deletes.calls)
	}
}

// A sweep that fails part-way still removed what it removed: the asset delete sweep finishes
// every row it can and names the ones it could not.
func TestRetentionHandlerCountsWhatAFailingSweeperRemoved(t *testing.T) {
	partial := &fakeRetentionSweeper{deleted: 2, err: errors.New("asset a-stuck: garage unreachable")}
	summary, err := NewRetentionHandler(nil, partial).Run(context.Background(), Job{})
	if !errors.Is(err, partial.err) || !strings.Contains(summary, "swept 2 item(s)") {
		t.Fatalf("Run() = %q, %v", summary, err)
	}
}

func TestRetentionHandlerRunsSweepersWhenPlanFails(t *testing.T) {
	planErr := errors.New("candidate query unavailable")
	engine := &fakeScheduledRetention{planErr: planErr}
	sweeper := &fakeRetentionSweeper{deleted: 2}

	summary, err := NewRetentionHandler(engine, sweeper).Run(context.Background(), Job{})
	if !errors.Is(err, planErr) {
		t.Fatalf("Run() error = %v, want plan failure", err)
	}
	if sweeper.calls != 1 || sweeper.now.IsZero() || engine.applyCalls != 0 {
		t.Fatalf("plan failure calls: sweeper=%d apply=%d now=%s", sweeper.calls, engine.applyCalls, sweeper.now)
	}
	if !strings.Contains(summary, "swept 2 item(s)") {
		t.Fatalf("summary = %q", summary)
	}
}

func TestRetentionHandlerRunsSweepersWhenApplyFails(t *testing.T) {
	applyErr := errors.New("retention claim unavailable")
	sweepErr := errors.New("owner export store unavailable")
	engine := &fakeScheduledRetention{
		plan: retention.Plan{Token: strings.Repeat("b", 64)}, applyErr: applyErr,
	}
	failing := &fakeRetentionSweeper{err: sweepErr}
	succeeding := &fakeRetentionSweeper{deleted: 4}

	summary, err := NewRetentionHandler(engine, failing, succeeding).Run(context.Background(), Job{})
	if !errors.Is(err, applyErr) || !errors.Is(err, sweepErr) {
		t.Fatalf("Run() error = %v, want joined apply and sweep failures", err)
	}
	if engine.applyCalls != 1 || failing.calls != 1 || succeeding.calls != 1 {
		t.Fatalf("apply failure calls: apply=%d failing sweep=%d succeeding sweep=%d",
			engine.applyCalls, failing.calls, succeeding.calls)
	}
	if !strings.Contains(summary, "swept 4 item(s)") {
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

type fakeRetentionSweeper struct {
	deleted int
	err     error
	now     time.Time
	calls   int
}

func (f *fakeRetentionSweeper) SweepExpired(_ context.Context, now time.Time) (int, error) {
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
