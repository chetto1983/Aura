package swarm

import (
	"context"
	"encoding/json"
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

// swarmSpawnTool is the tool swarm_depth.go's workerRegistry drops from a
// worker's registry once nesting would exceed AURA_SWARM_MAX_DEPTH (D-08/D-10,
// SWARM-05) -- not unconditionally any more. See workerRegistry's doc comment
// for the depth-conditional grant that replaced flat v1's blanket strip.
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
	// Context is the SWARM-01 goal/context split (plan 51-03): the file paths,
	// error messages and constraints the caller supplies alongside the goal,
	// rendered into structuredBrief's own section rather than concatenated into
	// the objective. Shared across every goal in this call — one swarm_spawn
	// invocation, one context. Empty is the zero value and renders no section
	// (structuredBrief's own empty-context contract).
	Context string
	// Gateway is the parent's Phase-35 policy PEP, injected into each worker so a
	// headless swarm-child dispatch is enforced and keyed on the ORIGINATING
	// conversation UUID (ConvID), never the flat worker session (Open Q1). nil is a no-op.
	Gateway *gateway.Gateway

	// IdentityID/ParentRunID/Enqueuer are the SWARM-03/09 background-delegation
	// seam (delegation_queue.go). A nil Enqueuer means "no background path
	// configured" and Run falls through to the synchronous waves below byte-for-
	// byte unchanged -- every existing caller (RunnerAdapter, swarm-demo) that
	// does not set these three fields keeps today's behavior with zero
	// regression. IdentityID scopes the durable queue row; ParentRunID is
	// carried into the payload for provenance only (runChild does not read it).
	IdentityID  string
	ParentRunID string
	Enqueuer    *DelegationEnqueuer

	// ResumeTurns is the 51-06b resume seam (Task 2): when non-empty, runChild seeds
	// LlmAgentConfig.UserTurns with THIS instead of structuredBrief(goal) -- everything
	// else about the worker's construction stays byte-identical, so there is still
	// exactly one worker construction in the tree (the invariant plan 51-01 established
	// and plan 51-09 depends on). buildResumeTurns (delegation_resume.go) is the one
	// caller: the persisted DelegationResumeState.History plus one final RoleTool
	// message answering the pending ask_user call. Empty for every ordinary (never-
	// paused) swarm_spawn call -- zero regression.
	ResumeTurns []llm.Message
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

	// SWARM-03/SWARM-09: a top-level call (depth<=1, D-04's flat-v1 top level;
	// see delegation_queue.go's RunConfig doc) with a configured Enqueuer stops
	// holding the operator's turn hostage -- it durably enqueues one row per
	// goal (D-01, no new table) and returns a model-readable "queued" summary
	// IMMEDIATELY. No worker is constructed here; the daemon-resident
	// DelegationClaimLoop (delegation_queue.go, wired at cmd/aura) runs them out
	// of band and delivers the consolidated report via the shipped steer rail
	// (D-04). depth>1 (a future nested/worker-issued delegation, SWARM-04,
	// expanded in plan 51-05) is UNTOUCHED -- it always runs the synchronous
	// waves below, regardless of whether an Enqueuer happens to be configured.
	if rc.Enqueuer != nil && rc.Depth <= 1 {
		return EnqueueDelegation(ctx, rc.Enqueuer, rc.IdentityID, goals, DelegationPayload{
			Context:        rc.Context,
			ConversationID: rc.ConvID,
			ParentRunID:    rc.ParentRunID,
			Depth:          rc.Depth,
		})
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
// defer cancel(), the #61611 spawn-loop guard, a per-child WithCancel + defer
// cancel) but DIVERGES on error handling: a child error is captured into its report
// slot and the goroutine returns NIL, so egCtx never cancels siblings (D-02). The
// per-child deadline this comment used to describe is gone (D-03) — runChild owns
// the inactivity-based staleness timer instead (child_staleness.go).
func runWave(ctx context.Context, rc RunConfig, goals []string, reports []ChildReport, start, end int) {
	width := end - start
	eg, egCtx := errgroup.WithContext(ctx)
	egCtx, cancel := context.WithCancel(egCtx)
	defer cancel()

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
			// D-03: no per-child wall-clock deadline any more -- runChild itself owns
			// the inactivity-based staleness timer (child_staleness.go) as the ONE
			// place a worker's liveness is judged. This WithCancel is ordinary
			// leak-safety cancellation propagation (mirrors parallel.go's own
			// invariant), never a deadline.
			childCtx, ccancel := context.WithCancel(egCtx)
			defer ccancel()
			// The synchronous wave path has no use for the reconstructed history
			// (that is the background delegation path's own concern,
			// delegation_queue.go's runWithHeartbeat) -- discarded here.
			reports[idx], _ = runChild(childCtx, rc, childBudget, idx, goals[idx])
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

// runChild constructs one worker (the workerRegistry-derived, depth-conditional
// registry, flat SessionID, the D-07 brief) and drains its Event stream into a
// ChildReport. A worker error → {failed} (D-02); an ask_user pause Event →
// {needs_user_input} (D-04); the final LLM Event → {ok, summary}. Every event is
// dumped to the per-child transcript (D-18, best-effort). It NEVER returns an
// error — failures live in the report. Reused unchanged for a NESTED worker's own
// children (plan 51-05, SWARM-04/05): the caller's rc.Depth is whatever depth this
// particular Run call is at, so the depth-conditional registry and the
// gateway.WithDelegatedDispatch reservation below both apply identically at any
// nesting level.
//
// The second return value is the reconstructed accumulated history (51-06b Task 2):
// the seeded UserTurns plus every tool-call/tool-result pair the worker actually ran,
// in order, plus (on a fresh pause) the synthesized ask_user assistant message. It is
// captured via historyRecorder, an agent.Hook -- the ONLY exported extension point
// LlmAgentConfig accepts -- so this file never reads internal/agent's private
// a.history field and internal/agent stays untouched by this plan. The synchronous
// swarm_spawn wave (runWave) discards it; the background delegation path
// (delegation_queue.go's runWithHeartbeat) is the one caller that needs it, to persist
// DelegationResumeState when the worker pauses.
func runChild(ctx context.Context, rc RunConfig, budget *agent.Budget, idx int, goal string) (ChildReport, []llm.Message) {
	childID := fmt.Sprintf("w%d", idx+1)
	report := ChildReport{GoalIndex: idx, ChildID: childID, Status: StatusOK}

	registry, nestingClosed := workerRegistry(rc)
	briefContext := rc.Context
	if nestingClosed {
		briefContext = appendNestingClosedNotice(briefContext)
	}

	// userTurns is whatever this run's worker is ACTUALLY seeded with -- a resume's
	// persisted history + answer (rc.ResumeTurns) when resuming, or the ordinary fresh
	// brief otherwise. It is the reconstruction's own seed (below), so a SECOND pause
	// during a resumed run reconstructs correctly without special-casing: the seed for
	// THIS run is always exactly what was passed to NewLlmAgent this time.
	userTurns := rc.ResumeTurns
	if len(userTurns) == 0 {
		userTurns = []llm.Message{{Role: llm.RoleUser, Content: structuredBrief(goal, briefContext)}}
	}

	rec := newHistoryRecorder()
	worker := agent.NewLlmAgent(agent.LlmAgentConfig{
		Client:     rc.Client,
		LLM:        rc.LLM,
		Registry:   registry,
		PreviewCap: rc.Cfg.ToolPreviewCap,
		RunDir:     rc.Cfg.RunDir,
		SessionID:  fmt.Sprintf("%s-swarm-%s", rc.ConvID, childID), // FLAT — no slash (Pitfall 4)
		// The gateway ledger key is the ORIGINATING conversation UUID (rc.ConvID), NOT the
		// flat worker SessionID above — uuid.Parse fails on the flat session (Open Q1).
		LedgerConversationID: rc.ConvID,
		Gateway:              rc.Gateway,
		UserTurns:            userTurns,
		// FailOpen (not the Register/NewHookManager default FailClosed): a recorder bug
		// must degrade to "resume state incomplete", never abort a live worker turn --
		// this hook is a best-effort observer, not a security gate.
		HookManager: agent.NewHookManagerWithPolicy(agent.FailOpen, rec),
	})

	slog.Info("swarm.child.spawned", "child", childID, "goal_index", idx)
	started := time.Now()

	// workerCtx/cancel is the staleness handle (D-03): runChild is the ONE place a
	// worker is constructed for EVERY caller (the synchronous wave above AND the
	// background claim loop's runWithHeartbeat, delegation_run.go), so this is the
	// ONE place the inactivity deadline lives -- never duplicated per caller.
	workerCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	idleDur := time.Duration(rc.Cfg.SwarmChildIdleSec) * time.Second
	staleness := newChildStaleness(cancel, idleDur)
	defer staleness.Stop()

	ic := agent.InvocationContext{
		// A worker's events are consumed by the loop below and dumped to a per-child
		// transcript; they never reach the Runner that writes a dispatch's `end` row.
		// Marking the context tells the gateway to close the reservations it opens here
		// itself — without it every worker tool call orphaned a `start`, and the
		// reconciler stamped succeeded calls as indeterminate half an hour later
		// (spike 099: 5/5 worker calls, 0/3 parent calls).
		Ctx:       gateway.WithDelegatedDispatch(workerCtx),
		RequestID: uuid.Must(uuid.NewV7()),
		Budget:    budget,
	}

	var pauseCall *llm.ToolCall
	for ev, err := range worker.Run(ic) {
		if err != nil {
			report.Status, report.Error = StatusFailed, err.Error()
			slog.Warn("swarm.child.failed", "child", childID, "error", err)
			break
		}
		if ev == nil {
			continue
		}
		staleness.Progress() // a worker that streams is a worker that is alive
		_ = dumpTranscript(rc.Cfg.RunDir, rc.ConvID, childID, *ev)
		if ai := ev.Actions.AwaitingInput; ai != nil {
			report.Status = StatusNeedsUserInput
			report.Question = ai.Question
			report.Options = optionLabels(ai.Options)
			report.ToolCallID = ai.ToolCallID
			pauseCall = pauseAskUserCall(ai)
			continue
		}
		if ev.LLMResponse != nil && ev.LLMResponse.Content != "" {
			report.Summary = ev.LLMResponse.Content
		}
	}

	report = normalizeStaleReport(report, staleness.Stalled(), idleDur)

	history := append(append([]llm.Message(nil), userTurns...), rec.messages()...)
	if report.Status == StatusNeedsUserInput && pauseCall != nil {
		history = append(history, llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{*pauseCall}})
	}

	slog.Info("swarm.child.completed", "child", childID, "status", report.Status, "dur", time.Since(started))
	return report, history
}

// normalizeStaleReport applies the D-11 uniform-stall normalization (this package's
// own numbering -- NOT 51-CONTEXT.md's D-11): a worker whose OWN staleness timer
// fired is relabeled {failed, "stalled: no worker event for <idle>"} -- distinguishable
// in the report text from a budget trip and from a genuine tool error -- UNLESS it
// already produced a terminal success (StatusOK + a populated Summary), which
// survives untouched (WR-01, carried verbatim from the wall-clock version this
// replaces: a worker that streamed its final ok answer and was then reaped in the
// race window right after keeps its success). stalled is staleness.Stalled() --
// OUR OWN timer having fired -- never a generic ctx.Err(), so a worker cancelled for
// any OTHER reason (the parent turn ending, a budget trip) is never mislabeled as
// stalled: Amendment #154 measured the wall clock as catching exactly the wrong
// worker (an upstream stall, not slow work), and a generic ctx.Err() check would
// repeat that same false-positive shape.
//
// Extracted as a pure function so "a late reap never clobbers a completed success"
// is directly unit-testable without racing a real timer against a real event loop.
func normalizeStaleReport(report ChildReport, stalled bool, idle time.Duration) ChildReport {
	if stalled && (report.Status != StatusOK || report.Summary == "") {
		report.Status, report.Error = StatusFailed, fmt.Sprintf("stalled: no worker event for %s", idle)
		report.Summary = ""
	}
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

// pauseAskUserArgs is the reconstructed wire shape of the ask_user call that paused --
// close enough to what the model actually emitted (question/options/kind) for a rebuilt
// worker's history to stay wire-valid and legible, without needing the model's own
// original raw Arguments JSON (Actions.AwaitingInput does not carry it, by design --
// the agent stays DB-free and the Event model is deliberately minimal, D-A1-03).
type pauseAskUserArgs struct {
	Question string   `json:"question"`
	Options  []string `json:"options,omitempty"`
	Kind     string   `json:"kind,omitempty"`
}

// pauseAskUserCall synthesizes the assistant ask_user tool_calls entry that MUST
// precede the resume's injected RoleTool answer for the seeded history to be
// wire-valid (an OpenAI-compatible "tool" message must directly follow the assistant
// message whose tool_calls contains its matching id). ai.ToolCallID is REAL (the
// worker's own id, stamped by the dispatch loop before the Event is emitted); the
// Arguments JSON is a faithful reconstruction of what the model asked, not a byte-exact
// replay of what it originally emitted.
func pauseAskUserCall(ai *agent.AwaitingInput) *llm.ToolCall {
	if ai == nil || ai.ToolCallID == "" {
		return nil
	}
	args, err := json.Marshal(pauseAskUserArgs{
		Question: ai.Question,
		Options:  optionLabels(ai.Options),
		Kind:     ai.Kind,
	})
	if err != nil {
		args = []byte(`{}`)
	}
	call := llm.ToolCall{ID: ai.ToolCallID, Type: "function"}
	call.Function.Name = "ask_user"
	call.Function.Arguments = string(args)
	return &call
}

// historyRecorder is the ONLY exported extension point runChild needs to reconstruct a
// worker's accumulated history from outside internal/agent: agent.Hook.AfterTool fires,
// serially, in the SAME original-call order internal/agent's own a.history append does
// (dispatch.go's result loop is explicitly serial "so the wire contract and cache
// stability hold"), so no lock is needed here.
//
// It records the RAW tool-result preview (res.Preview, tools.ToolResult's own field),
// not the model-facing WRAPPED rendering internal/agent's private a.history actually
// stores for untrusted tools (renderToolResultForPrompt's nonce envelope, AG-052) --
// that wrap cannot be reproduced from outside the package (its nonce is
// crypto/rand-minted per call and never surfaces on any Event or hook). For the ONE
// pair this mechanism is load-bearing for, tool_search, this is a non-issue:
// tool_search is on internal/agent's own trustedToolNames allowlist, so its result is
// NEVER wrapped -- res.Preview there IS byte-identical to what a.history actually
// stores. For every OTHER tool, a resumed worker's replayed history carries that tool's
// raw output without the untrusted-envelope trust framing it originally had -- a
// documented, narrower scope than byte-perfect verbatim (recorded in the plan SUMMARY),
// not a silent gap.
type historyRecorder struct {
	msgs []llm.Message
}

func newHistoryRecorder() *historyRecorder { return &historyRecorder{} }

// messages returns a defensive copy of everything recorded so far.
func (r *historyRecorder) messages() []llm.Message {
	return append([]llm.Message(nil), r.msgs...)
}

func (r *historyRecorder) OnTurnStart(context.Context, agent.HookTurn) error { return nil }

func (r *historyRecorder) BeforeModel(context.Context, *llm.Request) (*agent.ModelHookResult, error) {
	return nil, nil
}

func (r *historyRecorder) BeforeTool(context.Context, llm.ToolCall) (*agent.ToolHookResult, error) {
	return nil, nil
}

// AfterTool never rewrites the result (always returns nil, nil) -- it is a pure
// observer, mirroring a passive audit hook rather than a policy one.
func (r *historyRecorder) AfterTool(_ context.Context, call llm.ToolCall, res tools.ToolResult) (*agent.ToolResultHookResult, error) {
	r.msgs = append(r.msgs,
		llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}},
		llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Content: res.Preview},
	)
	return nil, nil
}

func (r *historyRecorder) OnTurnEnd(context.Context, agent.HookTurn) error { return nil }

var _ agent.Hook = (*historyRecorder)(nil)
