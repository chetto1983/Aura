package handlers

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/retention"
)

// KindRetentionSweep is the system-seeded bounded storage lifecycle job.
const KindRetentionSweep TaskKind = "retention_sweep"

const retentionMaxDuration = 5 * time.Minute

// RetentionPlanner applies the same Plan/Apply entrypoint used by the CLI.
type RetentionPlanner interface {
	Plan(context.Context) (retention.Plan, error)
	Apply(context.Context, string) (retention.ApplyReport, error)
}

type retentionHandler struct {
	engine RetentionPlanner
}

// NewRetentionHandler constructs the non-overlapping scheduled retention owner.
func NewRetentionHandler(engine RetentionPlanner) Handler {
	return retentionHandler{engine: engine}
}

func (h retentionHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindRetentionSweep, MaxDuration: retentionMaxDuration, ReschedulesOnRecovery: false}
}

func (h retentionHandler) Run(ctx context.Context, _ Job) (string, error) {
	if h.engine == nil {
		return "retention: disabled (no engine)", nil
	}
	plan, err := h.engine.Plan(ctx)
	if err != nil {
		return "", fmt.Errorf("retention plan: %w", err)
	}
	report, err := h.engine.Apply(ctx, plan.Token)
	if err != nil {
		return "", fmt.Errorf("retention apply: %w", err)
	}
	return fmt.Sprintf("retention ok: completed %d item(s), %d byte(s), retryable %d, failed %d",
		report.Completed, report.Bytes, report.Retryable, report.Failed), nil
}
