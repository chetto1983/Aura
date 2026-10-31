package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// errAgentJobNoCredential is the fail-closed case: neither a Resolver+identity, a
// Runtime, nor an already-set Client is available (T-02-08b). A misconfigured
// composition root used to fall through silently to a stale deployment client
// captured at boot; this refuses the run instead.
var errAgentJobNoCredential = errors.New("agent_job: no LLM credential source configured")

// agentJobMaxDuration is the fallback wall-clock budget when AgentDeps.MaxDuration is
// unset — mirrors the swarm worker's default inactivity deadline (AURA_SWARM_CHILD_IDLE_SEC=120).
const agentJobMaxDuration = 120 * time.Second

// autoRejectMarker is the synthesized RoleTool answer injected when an agent_job
// invokes ask_user with no human responder (D-25). Load-bearing literal: the
// auto-reject test asserts the marker appears in the audit summary.
const autoRejectMarker = "<auto-rejected: scheduled job has no human responder>"

// maxAutoRejects bounds the inject-and-continue loop independently of the step budget
// (a model could re-ask after each rejection): after this many auto-rejects the job
// finalizes with whatever summary it has, so the handler always terminates <30s
// regardless of model behavior (D-25 never-blocks).
const maxAutoRejects = 8

// A stored recurring goal otherwise looks like a fresh scheduling request (lab, 2026-10-12).
// Keep this outside the shared system prefix and retain it across auto-reject resumes.
const agentJobExecutionContext = `You are running an already scheduled agent job now.
Execute the goal for the current occurrence. Any recurring cadence in the goal is already configured: do not create, duplicate, or change that schedule unless the goal explicitly asks to manage scheduled tasks.
Return the outcome in your final response; the scheduler handles notification using the task's configured route. Do not send a separate notification just to deliver this result.
No human is available during this run. If a check or dependency fails, report that limitation honestly rather than promising future action.`

// AgentJobHandler runs a scheduled agent_job: a fresh, tool-bound, budget-bounded
// LlmAgent constructed DIRECTLY (mirroring swarm.runChild, NEVER runner.Turn —
// amendment #23), with its step budget INHERITED from the agent_job_runs row (D-24)
// and ask_user auto-rejected via inject-and-continue (D-25). It holds the shared
// runtime deps (client/LLM/registry); the per-run goal + budget arrive in the Job.
type AgentJobHandler struct {
	Deps AgentDeps
}

// agentJobPayload is the agent_job task payload shape: {"goal": "...", "keep_results": true}.
// KeepResults is the daily briefing's acceptance switch (agentjob_results.go).
type agentJobPayload struct {
	Goal        string `json:"goal"`
	KeepResults bool   `json:"keep_results,omitempty"`
}

// Meta declares the agent_job contract: a missed agent_job DOES reschedule on
// recovery (D-18: a recurring agent job that slipped a window re-arms its cadence),
// bounded by the wall-clock MaxDuration.
func (h AgentJobHandler) Meta() HandlerMeta {
	d := h.Deps.MaxDuration
	if d <= 0 {
		d = agentJobMaxDuration
	}
	return HandlerMeta{Kind: KindAgentJob, MaxDuration: d, ReschedulesOnRecovery: true}
}

// Run constructs the ephemeral LlmAgent, drains its Event stream, and returns the
// final assistant content as the audit summary. The step budget is taken from the
// row (Job.StepBudget) so a job scheduled with step_budget=10 terminates at 10, not
// the default 25 (SC#4 / D-24). On an ask_user pause it auto-rejects and re-runs
// within the SAME shared budget (D-25), never blocking; the wall-clock MaxDuration
// bounds the whole job end-to-end.
func (h AgentJobHandler) Run(ctx context.Context, job Job) (string, error) {
	payload := parseAgentJobPayload(job.Payload)
	goal := payload.Goal
	if goal == "" {
		return "", fmt.Errorf("agent_job: payload has no goal")
	}
	var kept *resultLog
	if payload.KeepResults {
		var err error
		if kept, err = openResultLog(h.Deps.RunDir, job.RunID); err != nil {
			return "", err
		}
		defer func() {
			if err := kept.Close(); err != nil {
				slog.Warn("agent_job.keep_results.close", "run", job.RunID, "err", err)
			}
		}()
	}

	runCtx, cancel := context.WithTimeout(ctx, h.Meta().MaxDuration)
	defer cancel()
	// The credential is resolved ONCE per run, before the retry loop, so every
	// auto-reject re-Run within one job stays on the SAME client (mirrors the
	// pre-CRED-07 "one scheduled run retains one route even if the operator
	// changes Settings while it is active" behavior — a later run resolves fresh).
	client, cfg, err := h.resolveLLM(runCtx)
	if err != nil {
		return "", fmt.Errorf("agent_job: resolve llm credential: %w", err)
	}
	budget, err := newJobBudget(job.StepBudget, cfg)
	if err != nil {
		return "", fmt.Errorf("agent_job: budget: %w", err)
	}

	prior := []llm.Message{
		{Role: llm.RoleSystem, Content: agentJobExecutionContext},
		{Role: llm.RoleUser, Content: goal},
	}
	var summary strings.Builder
	askedMutating := false
	loc := h.Deps.clockLocation(runCtx)

	for attempt := 0; attempt <= maxAutoRejects; attempt++ {
		worker := newAgentWorker(h.Deps, client, cfg, loc, job.RunID, job.OriginConversationID, prior)
		content, pause, asked, runErr := drain(runCtx, worker, budget, h.mutatingTool, kept)
		askedMutating = askedMutating || asked
		if runErr != nil {
			return summary.String(), runFailure(runCtx, runErr, askedMutating)
		}
		if content != "" {
			appendLine(&summary, content)
		}
		if pause == nil {
			return summary.String(), nil
		}
		// D-25 inject-and-continue: synthesize the assistant ask_user turn + the
		// auto-rejected RoleTool answer keyed to the pause's tool_call_id, then re-Run a
		// fresh LlmAgent with the extended history within the remaining shared budget.
		appendLine(&summary, autoRejectMarker)
		slog.Info("agent_job.ask_user.auto_rejected", "run", job.RunID, "question", pause.Question)
		prior = append(prior,
			assistantAskUserTurn(pause),
			llm.Message{Role: llm.RoleTool, ToolCallID: pause.ToolCallID, Content: autoRejectMarker},
		)
	}
	// Bounded out — the model kept asking; finalize with the marker trail (never block).
	return summary.String(), nil
}

// resolveLLM picks the client+config this run's worker(s) use, in priority
// order (T-02-08b/CRED-07):
//
//  1. h.Deps.Resolver, when non-nil: the job's OWNING identity's own
//     credential, keyed on identityctx.IdentityID(ctx) — a scheduled run has no
//     HTTP principal, so this reads the identity cron's scheduledOperationContext
//     already bound to ctx from the task row, NOT a per-request principal
//     (internal/cron/dispatch.go's scheduledOperationContext, called before Run).
//     An empty identity (a system/no-identity task) falls through to Runtime.
//  2. h.Deps.Runtime, when non-nil: a fresh process-wide snapshot.
//  3. h.Deps.Client, when non-nil: an already-set client (the shape every
//     pre-CRED-07 test in this package constructs directly).
//  4. Neither of the above: errAgentJobNoCredential — the fail-closed case a
//     nil Runtime used to skip past silently via the deployment client
//     serve_dispatch.go captured at boot; that capture is gone, so this is now
//     a real refusal instead.
func (h AgentJobHandler) resolveLLM(ctx context.Context) (llm.Client, llm.Config, error) {
	if h.Deps.Resolver != nil {
		if identityID := identityctx.IdentityID(ctx); identityID != "" {
			snapshot, err := h.Deps.Resolver.SnapshotFor(ctx, identityID)
			if err != nil {
				return nil, llm.Config{}, err
			}
			return snapshot.Client, snapshot.Config, nil
		}
	}
	if h.Deps.Runtime != nil {
		snapshot := h.Deps.Runtime.Snapshot()
		return snapshot.Client, snapshot.Config, nil
	}
	if h.Deps.Client != nil {
		return h.Deps.Client, h.Deps.LLM, nil
	}
	return nil, llm.Config{}, errAgentJobNoCredential
}

// drain runs one LlmAgent invocation to completion, returning the final assistant
// content, the FIRST ask_user pause (nil when the run finished without one), whether the
// model asked for a tool that mutating reports as changing state, and a terminal error. A
// pause stops the drain so the caller can inject-and-continue; the agent's own loop
// terminates the run on a pause (llm_agent.go emitPauses returns), so there is nothing left
// to drain after it.
//
// The dispatch is unobserved: no Runner sees these events, so the gateway closes every ledger
// row it opens. Unmarked, a job's calls stayed open and read as live runs, holding off the
// updater's quiet-spell apply (lab VM, 2026-10-14). It is not a worker's dispatch, which would
// also take a per-run MCP session.
func drain(ctx context.Context, worker *agent.LlmAgent, budget *agent.Budget, mutating func(tool string) bool, kept *resultLog) (content string, pause *agent.AwaitingInput, askedMutating bool, err error) {
	ic := agent.InvocationContext{
		Ctx:       tools.WithUnobservedDispatch(ctx),
		RequestID: uuid.Must(uuid.NewV7()),
		Budget:    budget,
	}
	for ev, runErr := range worker.Run(ic) {
		if runErr != nil {
			return content, nil, askedMutating, runErr
		}
		if ev == nil {
			continue
		}
		if err := kept.record(ev); err != nil {
			return content, nil, askedMutating, err
		}
		for _, tool := range eventTools(ev) {
			askedMutating = askedMutating || mutating(tool)
		}
		if ai := ev.Actions.AwaitingInput; ai != nil {
			return content, ai, askedMutating, nil
		}
		if ev.LLMResponse != nil && ev.LLMResponse.Content != "" {
			content = ev.LLMResponse.Content
		}
	}
	return content, nil, askedMutating, nil
}

// eventTools names the tools an event announces: a call the model asked for, or one starting.
func eventTools(ev *agent.Event) []string {
	var names []string
	if ev.LLMResponse != nil {
		for _, call := range ev.LLMResponse.ToolCalls {
			names = append(names, call.Function.Name)
		}
	}
	if inv := ev.Actions.ToolInvocation; inv != nil {
		names = append(names, inv.ToolName)
	}
	return names
}

// mutatingTool reports whether a tool may change state, by its spec. A name the registry does
// not know counts as mutating: the retry must never assume a call left nothing behind.
func (h AgentJobHandler) mutatingTool(name string) bool {
	if h.Deps.Registry == nil {
		return true
	}
	tool, ok := h.Deps.Registry.Get(name)
	return !ok || tool.Spec().Mutating
}

// runFailure wraps a failed run. It marks the failure retryable when the model failed on a
// transient error before it asked for any tool that changes state: a read-only tool such as a
// web search leaves nothing behind, so firing the job again later cannot repeat an effect
// (prd.md §15). After a mutating tool the failure is reported, never retried, because the
// tool may have acted and a fresh run could act twice.
func runFailure(jobCtx context.Context, err error, askedMutating bool) error {
	err = fmt.Errorf("agent_job run: %w", err)
	if askedMutating || !transientFailure(jobCtx, err) {
		return err
	}
	return retryableBeforeEffectsError{err: err}
}

// transientFailure reports whether a later run may not hit err. Besides the agent's own rule it
// counts a model call that ran out of its own time while the job still had time: jobCtx is the
// parent of every call and the wallclock budget ends a turn as an event, so such a deadline is
// the call's timeout, typically a request the network dropped mid-flight (measured 2026-10-09).
// The job's own deadline is not transient.
func transientFailure(jobCtx context.Context, err error) bool {
	if agent.IsTransientLLMError(err) {
		return true
	}
	return errors.Is(err, context.DeadlineExceeded) && jobCtx.Err() == nil
}

// retryableBeforeEffectsError is the mark the cron dispatcher reads, through the method, to
// re-arm the task (internal/cron/transient_retry.go). This package cannot import cron.
type retryableBeforeEffectsError struct{ err error }

func (e retryableBeforeEffectsError) Error() string { return e.err.Error() }
func (e retryableBeforeEffectsError) Unwrap() error { return e.err }

// RetryableBeforeEffects tells the dispatcher this run may be fired again.
func (e retryableBeforeEffectsError) RetryableBeforeEffects() bool { return true }

// newJobBudget builds the agent_job budget with MaxSteps INHERITED from the row
// (D-24): a positive Job.StepBudget overrides the runtime profile's loop budget
// (amendment #188), which in turn overrides the AURA_LOOP_* env/builtin default;
// a zero/absent step_budget means the task never set one. The wallclock follows
// the profile the same way; dedup stays at its env/default value.
func newJobBudget(stepBudget int, cfg llm.Config) (*agent.Budget, error) {
	opts := agent.BudgetOptionsFromConfig(cfg)
	if stepBudget > 0 {
		s := stepBudget
		opts.MaxSteps = &s
	}
	return agent.NewBudget(opts)
}

// assistantAskUserTurn reconstructs the assistant tool_calls message that triggered a
// pause, so the injected RoleTool answer threads correctly on the OpenAI wire (a
// RoleTool message must follow an assistant message carrying the matching tool_call
// id). The arguments are a best-effort reconstruction from the pause payload — only
// the id needs to match for the resume to be wire-valid.
func assistantAskUserTurn(pause *agent.AwaitingInput) llm.Message {
	args, _ := json.Marshal(map[string]any{
		"question": pause.Question,
		"kind":     askUserKind(pause.Kind),
	})
	call := llm.ToolCall{ID: pause.ToolCallID, Type: "function"}
	call.Function.Name = "ask_user"
	call.Function.Arguments = string(args)
	return llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}}
}

// askUserKind defaults a missing pause kind to "clarification" (the ask_user schema's
// free-text answer kind) so the reconstructed assistant call stays schema-valid.
func askUserKind(kind string) string {
	if strings.TrimSpace(kind) == "" {
		return "clarification"
	}
	return kind
}

// agentJobGoal extracts the goal text from the agent_job payload.
func parseAgentJobPayload(payload []byte) agentJobPayload {
	var p agentJobPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return agentJobPayload{}
	}
	p.Goal = strings.TrimSpace(p.Goal)
	return p
}

// appendLine appends content to the summary builder on its own line.
func appendLine(b *strings.Builder, content string) {
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(content)
}
