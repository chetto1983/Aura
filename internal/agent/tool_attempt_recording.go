package agent

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	tools "github.com/aura/aura/internal/agent/tools/registry"
)

// ToolAttemptRecordInput is the channel-neutral evidence needed to persist one
// tool execution observation without storing raw argument values.
type ToolAttemptRecordInput struct {
	RunID     string
	ToolName  string
	Arguments map[string]any
	StartedAt time.Time
	Elapsed   time.Duration
	Class     string
	Reason    string
	Err       error
}

// RecordToolAttempt persists a tool_attempts row when a repo and run_id are
// available. Persistence failures are logged but never change the tool result.
func RecordToolAttempt(ctx context.Context, logger *slog.Logger, repo attempts.Repo, input ToolAttemptRecordInput) {
	if repo == nil {
		return
	}
	runID := strings.TrimSpace(input.RunID)
	if runID == "" {
		if logger != nil {
			logger.Warn("tool_attempts_skipped_missing_run_id", "tool_name", input.ToolName)
		}
		return
	}
	startedAt := input.StartedAt
	if startedAt.IsZero() {
		startedAt = time.Now()
	}
	elapsed := input.Elapsed
	if elapsed < 0 {
		elapsed = 0
	}
	argsHash, argKeys := tools.ObservationArgsHash(input.Arguments)
	obs := tools.ToolObservation{
		RunID:         runID,
		ToolName:      input.ToolName,
		ToolKind:      tools.ToolKindOf(input.ToolName),
		AttemptN:      1,
		Outcome:       tools.BucketOf(input.Class),
		Class:         input.Class,
		Reason:        input.Reason,
		ArgsHash:      argsHash,
		ArgKeys:       argKeys,
		ErrorRedacted: redactToolError(input.Err),
		StartedAt:     startedAt,
		ElapsedMS:     elapsed.Milliseconds(),
	}
	if err := repo.Record(ctx, obs); err != nil && logger != nil {
		logger.Error("tool_attempts_write_failed", "err", err, "tool_name", obs.ToolName)
	}
}
