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

func TestRetentionHandlerNilEngineIsDisabled(t *testing.T) {
	summary, err := NewRetentionHandler(nil).Run(context.Background(), Job{})
	if err != nil || !strings.Contains(summary, "disabled") {
		t.Fatalf("Run() = %q, %v", summary, err)
	}
}

type fakeScheduledRetention struct {
	plan   retention.Plan
	report retention.ApplyReport
	token  string
}

type fakeOwnerExportSweeper struct {
	deleted int
	err     error
	now     time.Time
}

func (f *fakeOwnerExportSweeper) SweepExpired(_ context.Context, now time.Time) (int, error) {
	f.now = now
	return f.deleted, f.err
}

func (f *fakeScheduledRetention) Plan(context.Context) (retention.Plan, error) {
	return f.plan, nil
}

func (f *fakeScheduledRetention) Apply(_ context.Context, token string) (retention.ApplyReport, error) {
	f.token = token
	return f.report, nil
}
