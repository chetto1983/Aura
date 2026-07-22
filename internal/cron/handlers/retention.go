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

// OwnerExportSweeper physically removes expired durable owner-export archives.
type OwnerExportSweeper interface {
	SweepExpired(context.Context, time.Time) (int, error)
}

type retentionHandler struct {
	engine       RetentionPlanner
	ownerExports []OwnerExportSweeper
}

// NewRetentionHandler constructs the non-overlapping scheduled retention owner.
func NewRetentionHandler(engine RetentionPlanner, ownerExports ...OwnerExportSweeper) Handler {
	return retentionHandler{engine: engine, ownerExports: ownerExports}
}

func (h retentionHandler) Meta() HandlerMeta {
	return HandlerMeta{Kind: KindRetentionSweep, MaxDuration: retentionMaxDuration, ReschedulesOnRecovery: false}
}

func (h retentionHandler) Run(ctx context.Context, _ Job) (string, error) {
	if h.engine == nil && len(h.ownerExports) == 0 {
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
	exportsDeleted := 0
	for _, sweeper := range h.ownerExports {
		if sweeper == nil {
			continue
		}
		deleted, err := sweeper.SweepExpired(ctx, time.Now().UTC())
		if err != nil {
			runErrors = append(runErrors, fmt.Errorf("owner export retention: %w", err))
			continue
		}
		exportsDeleted += deleted
	}
	summary := fmt.Sprintf("retention completed: completed %d item(s), %d byte(s), retryable %d, failed %d, owner exports deleted %d",
		report.Completed, report.Bytes, report.Retryable, report.Failed, exportsDeleted)
	return summary, errors.Join(runErrors...)
}
