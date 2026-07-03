package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// agentJobMaxDuration is the fallback wall-clock budget when AgentDeps.MaxDuration is
// unset — mirrors the swarm child timeout default (AURA_SWARM_CHILD_TIMEOUT_SEC=120).
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

// AgentJobHandler runs a scheduled agent_job: a fresh, tool-bound, budget-bounded
// LlmAgent constructed DIRECTLY (mirroring swarm.runChild, NEVER runner.Turn —
// amendment #23), with its step budget INHERITED from the agent_job_runs row (D-24)
// and ask_user auto-rejected via inject-and-continue (D-25). It holds the shared
// runtime deps (client/LLM/registry); the per-run goal + budget arrive in the Job.
type AgentJobHandler struct {
	Deps AgentDeps
}

// agentJobPayload is the agent_job task payload shape: {"goal": "..."}.
type agentJobPayload struct {
	Goal string `json:"goal"`
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
	goal := agentJobGoal(job.Payload)
	if goal == "" {
		return "", fmt.Errorf("agent_job: payload has no goal")
	}

	budget, err := newJobBudget(job.StepBudget)
	if err != nil {
		return "", fmt.Errorf("agent_job: budget: %w", err)
	}

	runCtx, cancel := context.WithTimeout(ctx, h.Meta().MaxDuration)
	defer cancel()

	prior := []llm.Message{{Role: llm.RoleUser, Content: goal}}
	var summary strings.Builder

	for attempt := 0; attempt <= maxAutoRejects; attempt++ {
		worker := newAgentWorker(h.Deps, job.RunID, job.OriginConversationID, prior)
		content, pause, runErr := drain(runCtx, worker, budget)
		if runErr != nil {
			return summary.String(), fmt.Errorf("agent_job run: %w", runErr)
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

// drain runs one LlmAgent invocation to completion, returning the final assistant
// content, the FIRST ask_user pause (nil when the run finished without one), and a
// terminal error. A pause stops the drain so the caller can inject-and-continue; the
// agent's own loop terminates the run on a pause (llm_agent.go emitPauses returns),
// so there is nothing left to drain after it.
func drain(ctx context.Context, worker *agent.LlmAgent, budget *agent.Budget) (string, *agent.AwaitingInput, error) {
	ic := agent.InvocationContext{
		Ctx:       ctx,
		RequestID: uuid.Must(uuid.NewV7()),
		Budget:    budget,
	}
	var content string
	for ev, err := range worker.Run(ic) {
		if err != nil {
			return content, nil, err
		}
		if ev == nil {
			continue
		}
		if ai := ev.Actions.AwaitingInput; ai != nil {
			return content, ai, nil
		}
		if ev.LLMResponse != nil && ev.LLMResponse.Content != "" {
			content = ev.LLMResponse.Content
		}
	}
	return content, nil, nil
}

// newJobBudget builds the agent_job budget with MaxSteps INHERITED from the row
// (D-24): a positive Job.StepBudget overrides the AURA_LOOP_MAX_STEPS default; a
// zero/absent budget falls through to the env/builtin default (the task never set
// one). The wallclock + dedup stay at their env/default values.
func newJobBudget(stepBudget int) (*agent.Budget, error) {
	opts := agent.BudgetOptions{}
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
func agentJobGoal(payload []byte) string {
	var p agentJobPayload
	if err := json.Unmarshal(payload, &p); err != nil {
		return ""
	}
	return strings.TrimSpace(p.Goal)
}

// appendLine appends content to the summary builder on its own line.
func appendLine(b *strings.Builder, content string) {
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(content)
}
