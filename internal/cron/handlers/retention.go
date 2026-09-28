package handlers

import (
	"context"
	"errors"
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

// Sweeper removes one bounded batch of what has come due by now and reports how many items
// it removed: expired owner-export archives, and assets whose delete did not finish. A failure
// is returned for the job to report; whatever it left is swept on the next run.
type Sweeper interface {
	SweepExpired(context.Context, time.Time) (int, error)
}

type retentionHandler struct {
	engine   RetentionPlanner
	sweepers []Sweeper
}

// NewRetentionHandler constructs the non-overlapping scheduled retention owner. A nil sweeper
// is skipped.
func NewRetentionHandler(engine RetentionPlanner, sweepers ...Sweeper) Handler {
	return retentionHandler{engine: engine, sweepers: sweepers}
}

func (h retentionHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindRetentionSweep, MaxDuration: retentionMaxDuration, ReschedulesOnRecovery: false}
}

func (h retentionHandler) Run(ctx context.Context, _ Job) (string, error) {
	if h.engine == nil && len(h.sweepers) == 0 {
		return "retention: disabled (no engine)", nil
	}
	report := retention.ApplyReport{}
	var runErrors []error
	if h.engine != nil {
		plan, err := h.engine.Plan(ctx)
		if err != nil {
			runErrors = append(runErrors, fmt.Errorf("retention plan: %w", err))
		} else {
			report, err = h.engine.Apply(ctx, plan.Token)
			if err != nil {
				runErrors = append(runErrors, fmt.Errorf("retention apply: %w", err))
			}
		}
	}
	swept := 0
	for _, sweeper := range h.sweepers {
		if sweeper == nil {
			continue
		}
		removed, err := sweeper.SweepExpired(ctx, time.Now().UTC())
		swept += removed
		if err != nil {
			runErrors = append(runErrors, fmt.Errorf("retention sweep: %w", err))
		}
	}
	summary := fmt.Sprintf("retention completed: completed %d item(s), %d byte(s), retryable %d, failed %d, swept %d item(s)",
		report.Completed, report.Bytes, report.Retryable, report.Failed, swept)
	return summary, errors.Join(runErrors...)
}
