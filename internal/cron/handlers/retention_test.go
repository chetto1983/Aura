package handlers

import (
	"context"
	"strings"
	"testing"

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

func (f *fakeScheduledRetention) Plan(context.Context) (retention.Plan, error) {
	return f.plan, nil
}

func (f *fakeScheduledRetention) Apply(_ context.Context, token string) (retention.ApplyReport, error) {
	f.token = token
	return f.report, nil
}
