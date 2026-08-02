package swarm

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/panicobs"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

// budgetReserve is the number of steps left untouched for the PARENT's post-swarm
// synthesis turns (D-09 / A3). The child finalize() rides outside the budget gate,
// so this reserve protects the parent's aggregation turn, not the children.
const budgetReserve = 3

// swarmSpawnTool is the tool name dropped from every worker's registry (D-08/D-10):
// flat v1 forbids nested swarms.
const swarmSpawnTool = "swarm_spawn"

// RunConfig carries the inputs the ephemeral runner needs for one swarm_spawn call.
// Depth is the invocation depth (a parent-initiated spawn is depth 1); the D-10
// guard rejects depth >= AURA_SWARM_MAX_DEPTH. ConvID keys the per-child SessionID
// and the transcript directory.
type RunConfig struct {
	ParentBudget   *agent.Budget
	ParentRegistry *tools.Registry
	Client         llm.Client
	LLM            llm.Config
	Cfg            config.Config
	ConvID         string
	Depth          int
	// Gateway is the parent's Phase-35 policy PEP, injected into each worker so a
	// headless swarm-child dispatch is enforced and keyed on the ORIGINATING
	// conversation UUID (ConvID), never the flat worker session (Open Q1). nil is a no-op.
	Gateway *gateway.Gateway
}

// Run fans goals out as LlmAgent workers in budget-bounded leak-safe waves, isolates
// per-child failures (a failed/timed-out child becomes a {status:failed} report; its
// siblings are NEVER cancelled — D-02), and returns the ordered []ChildReport
// marshaled to JSON. A pre-flight failure (depth/goals-cap/budget) returns a
// model-readable "error: ..." string and NO worker is spawned. The returned error is
// reserved for a genuine marshal failure; domain rejections ride in the string so the
// model self-corrects (D-15).
func Run(ctx context.Context, rc RunConfig, goals []string) (string, error) {
	if msg, ok := preflight(rc, goals); !ok {
		return msg, nil
	}

	// AG-038: atomically RESERVE the parent's post-swarm synthesis budget before the
	// fan-out so a concurrent consumer cannot race it away (the old Remaining() check
	// was TOCTOU-best-effort). The reserved steps are withheld from the shared pool —
	// children cannot spend them — and released after the waves complete so the
	// parent's aggregation turn has them. A failed reservation rejects the spawn.
	if rc.ParentBudget != nil {
		if rem := rc.ParentBudget.Remaining(); rem < len(goals)+budgetReserve {
			return fmt.Sprintf(
				"error: insufficient budget — %d goals need %d steps but only %d remain; reduce goals or answer directly",
				len(goals), len(goals)+budgetReserve, rem), nil
		}
		if !rc.ParentBudget.TryReserve(budgetReserve) {
			return fmt.Sprintf(
				"error: insufficient budget — could not reserve %d steps for synthesis; reduce goals or answer directly",
				budgetReserve), nil
		}
		defer rc.ParentBudget.Release(budgetReserve)
	}

	reports := make([]ChildReport, len(goals))
	concurrent := max(rc.Cfg.MaxSwarmConcurrent, 1)

	for start := 0; start < len(goals); start += concurrent {
		end := min(start+concurrent, len(goals))
		runWave(ctx, rc, goals, reports, start, end)
	}

	return marshalReports(reports)
}

// preflight applies the three spawn-time rejections in order (D-10 depth, D-13 goals
// cap, D-09 budget). It returns (model-readable error string, false) on a rejection,
// or ("", true) when the spawn may proceed. NO worker is constructed on a rejection.
func preflight(rc RunConfig, goals []string) (string, bool) {
	if msg, ok := checkDepth(rc.Depth, maxDepth()); !ok {
		return "error: " + msg, false
	}
	if len(goals) == 0 {
		// Teach the arg shape instead of steering away: a deferred stub carries no
		// schema, so the model's first call often guesses the wrong key and lands
		// here with usable subtasks in hand (live E2E 2026-06-04: two empty calls,
		// then sequential fallback). Name the exact argument so it self-corrects,
		// and keep the answer-directly escape only for the genuinely-single task.
		return `error: no goals provided — pass the subtasks as {"goals":["<complete brief for subtask 1>","<complete brief for subtask 2>"]}; if there is only one simple task, answer the user directly instead of spawning a swarm`, false
	}
	if goalsCap := rc.Cfg.MaxSwarmGoals; goalsCap > 0 && len(goals) > goalsCap {
		return fmt.Sprintf(
			"error: too many goals — %d requested but the cap is %d; reduce the goals or run them in fewer, broader subtasks",
			len(goals), goalsCap), false
	}
	// The budget admission + atomic synthesis reservation (AG-038) is done in Run,
	// not here, so the reserve cannot be raced away between this check and the spawn.
	return "", true
}

// runWave runs goals[start:end] concurrently and collects each into reports[i]. It
// copies parallel.go's leak-safety invariants VERBATIM (errgroup.WithContext,
// defer cancel(), the #61611 spawn-loop guard, a per-child WithTimeout + defer
// cancel) but DIVERGES on error handling: a child error is captured into its report
// slot and the goroutine returns NIL, so egCtx never cancels siblings (D-02).
func runWave(ctx context.Context, rc RunConfig, goals []string, reports []ChildReport, start, end int) {
	width := end - start
	eg, egCtx := errgroup.WithContext(ctx)
	egCtx, cancel := context.WithCancel(egCtx)
	defer cancel()

	childTimeout := time.Duration(rc.Cfg.SwarmChildTimeoutSec) * time.Second

	for i := start; i < end; i++ {
		idx := i
		childBudget := rc.ParentBudget.Child(width)
		eg.Go(func() error {
			defer func() {
				if r := recover(); r != nil {
					panicobs.Record(panicobs.SiteSwarmWave)
					reports[idx] = panicChildReport(idx, r)
				}
			}()
			if egCtx.Err() != nil { // #61611 spawn-loop guard
				return nil
			}
			childCtx, ccancel := context.WithTimeout(egCtx, childTimeout)
			defer ccancel()
			reports[idx] = runChild(childCtx, rc, childBudget, idx, goals[idx])
			return nil // D-02: a child failure NEVER cancels siblings
		})
	}
	_ = eg.Wait()
}

func panicChildReport(idx int, recovered any) ChildReport {
	return ChildReport{
		GoalIndex: idx,
		ChildID:   fmt.Sprintf("w%d", idx+1),
		Status:    StatusFailed,
		Error:     fmt.Sprintf("panic: %v", recovered),
	}
}

// runChild constructs one worker (parent registry minus swarm_spawn, flat SessionID,
// the D-07 brief) and drains its Event stream into a ChildReport. A worker error →
// {failed} (D-02); an ask_user pause Event → {needs_user_input} (D-04); the final
// LLM Event → {ok, summary}. Every event is dumped to the per-child transcript
// (D-18, best-effort). It NEVER returns an error — failures live in the report.
func runChild(ctx context.Context, rc RunConfig, budget *agent.Budget, idx int, goal string) ChildReport {
	childID := fmt.Sprintf("w%d", idx+1)
	report := ChildReport{GoalIndex: idx, ChildID: childID, Status: StatusOK}

	worker := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     rc.Client,
		LLM:        rc.LLM,
		Registry:   tools.Without(rc.ParentRegistry, swarmSpawnTool),
		PreviewCap: rc.Cfg.ToolPreviewCap,
		RunDir:     rc.Cfg.RunDir,
		SessionID:  fmt.Sprintf("%s-swarm-%s", rc.ConvID, childID), // FLAT — no slash (Pitfall 4)
		// The gateway ledger key is the ORIGINATING conversation UUID (rc.ConvID), NOT the
		// flat worker SessionID above — uuid.Parse fails on the flat session (Open Q1).
		LedgerConversationID: rc.ConvID,
		Gateway:              rc.Gateway,
		UserTurns:            []llm.Message{{Role: llm.RoleUser, Content: structuredBrief(goal)}},
	})

	slog.Info("swarm.child.spawned", "child", childID, "goal_index", idx)
	started := time.Now()

	ic := agent.InvocationContext{
		Ctx:       ctx,
		RequestID: uuid.Must(uuid.NewV7()),
		Budget:    budget,
	}

	for ev, err := range worker.Run(ic) {
		if err != nil {
			report.Status, report.Error = StatusFailed, err.Error()
			slog.Warn("swarm.child.failed", "child", childID, "error", err)
			break
		}
		if ev == nil {
			continue
		}
		_ = dumpTranscript(rc.Cfg.RunDir, rc.ConvID, childID, *ev)
		if ai := ev.Actions.AwaitingInput; ai != nil {
			report.Status = StatusNeedsUserInput
			report.Question = ai.Question
			report.Options = optionLabels(ai.Options)
			report.ToolCallID = ai.ToolCallID
			continue
		}
		if ev.LLMResponse != nil && ev.LLMResponse.Content != "" {
			report.Summary = ev.LLMResponse.Content
		}
	}

	// D-11: normalize a deadline trip to a uniform {failed, "timeout"} — but NOT when
	// the worker already produced a terminal success (StatusOK + a populated Summary).
	// A worker that streamed its final ok answer and was then descheduled past the
	// deadline keeps its success rather than being clobbered into a spurious timeout
	// failure (WR-01). A worker that surfaced the cancellation as a stream error
	// (StatusFailed with the raw ctx error) is re-labeled to the uniform "timeout"; a
	// worker that produced nothing terminal (still default StatusOK, empty Summary) is
	// failed as a timeout.
	if ctx.Err() == context.DeadlineExceeded && (report.Status != StatusOK || report.Summary == "") {
		report.Status, report.Error = StatusFailed, "timeout"
		report.Summary = ""
	}

	slog.Info("swarm.child.completed", "child", childID, "status", report.Status, "dur", time.Since(started))
	return report
}

// optionLabels projects the Event-model PauseOption values onto the flat []string
// the ChildReport carries (the parent re-offers them via its own ask_user, D-05).
func optionLabels(opts []agent.PauseOption) []string {
	if len(opts) == 0 {
		return nil
	}
	out := make([]string, len(opts))
	for i, o := range opts {
		out[i] = o.Label
	}
	return out
}
