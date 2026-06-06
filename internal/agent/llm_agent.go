// LlmAgent is Aura's first real Agent implementation (D-01 commit 6): the
// budget-gated tool-dispatch run-loop that drives the openai_compat client,
// threads ToolResult into in-memory history, streams chunk/tool-call/tool-result/
// final Events, and terminates via text_response with a content-stop fallback. It
// resolves the Phase-2 deferred SpanID minting (full-tree crypto/rand, tracing.go)
// and emits one llm.request span per LLM call.
//
// The loop shape replicates agenttest.CountingAgent.Run (the canonical budget-gated
// iter.Seq2 emitter): budget gate BEFORE each call -> on a trip yield a terminal
// Event (never the error slot, D-04); the yield-after-false guard is mandatory.
package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/canonicaljson"
	"github.com/chetto1983/aura/internal/llm"
)

// truncationNotice is appended to a finish_reason="length" final answer (D-21).
// Load-bearing literal: a test asserts it byte-for-byte.
const truncationNotice = "\n\n[risposta troncata: max_tokens]"

// terminalTool is the sole loop-terminating tool channel (D-13).
const terminalTool = "text_response"

// LlmAgent drives the autonomous tool-dispatch loop. history is in-memory only
// this phase (D-26); messages[0] is the byte-stable system prompt (Req#14) and is
// never mutated. sessionID = Event.ThreadID (D-26) — the sidecar/spillover key.
type LlmAgent struct {
	name        string
	description string
	client      llm.Client
	cfg         llm.Config
	registry    *tools.Registry
	previewCap  int
	runDir      string
	sessionID   string
	workspace   string
	builder     *prompt.PromptBuilder

	history []llm.Message

	// recoveryAttempts is the per-run recovery gate (D-08): a COUNTER, never a
	// one-shot boolean latch (CrewAI #1656 anti-pattern). The FIRST early-
	// termination trip (budget or dedup) increments it 0→1, injects a single
	// user-role nudge, and runs one more turn; a SECOND trip of any kind routes
	// straight to finalize(). It lives on LlmAgent (NOT Budget — shared by pointer
	// across the swarm tree, D-10 — and NOT InvocationContext) so recovery state is
	// per-run and never leaks between branches.
	recoveryAttempts int
}

var _ Agent = (*LlmAgent)(nil)

// LlmAgentConfig carries the LlmAgent constructor inputs.
type LlmAgentConfig struct {
	Name        string
	Description string
	Client      llm.Client
	LLM         llm.Config
	Registry    *tools.Registry
	PreviewCap  int    // AURA_CONTEXT_PREVIEW_CAP_BYTES (config.ToolPreviewCap)
	RunDir      string // config.RunDir — sidecar root
	SessionID   string // Event.ThreadID; sidecar dir key (D-26)
	Workspace   string // the shell workspace path, rendered into the per-turn tail hint (#52/D-41); "" omits
	UserTurns   []llm.Message
}

// NewLlmAgent builds an LlmAgent. messages[0] is ALWAYS the byte-stable system
// prompt (D-08/D-09) followed by the supplied user turns; the agent owns history
// from here. Name/Description default when empty.
func NewLlmAgent(cfg LlmAgentConfig) *LlmAgent {
	hist := make([]llm.Message, 0, len(cfg.UserTurns)+1)
	hist = append(hist, llm.Message{Role: llm.RoleSystem, Content: systemMessage()})
	hist = append(hist, cfg.UserTurns...)

	name := cfg.Name
	if name == "" {
		name = "aura"
	}
	desc := cfg.Description
	if desc == "" {
		desc = "Aura's autonomous tool-dispatch agent"
	}
	return &LlmAgent{
		name:        name,
		description: desc,
		client:      cfg.Client,
		cfg:         cfg.LLM,
		registry:    cfg.Registry,
		previewCap:  cfg.PreviewCap,
		runDir:      cfg.RunDir,
		sessionID:   cfg.SessionID,
		workspace:   cfg.Workspace,
		builder:     prompt.NewPromptBuilder(),
		history:     hist,
	}
}

// Name is the Event Author / FindAgent key.
func (a *LlmAgent) Name() string { return a.name }

// Description is the human/LLM-facing one-liner.
func (a *LlmAgent) Description() string { return a.description }

// SubAgents returns nil — LlmAgent is a leaf.
func (a *LlmAgent) SubAgents() []Agent { return nil }

// FindAgent returns self when name matches, else nil.
func (a *LlmAgent) FindAgent(name string) Agent {
	if a.name == name {
		return a
	}
	return nil
}

// Run drives the budget-gated tool-dispatch loop (Req#9/#10). Termination paths:
// text_response (D-13), content-stop fallback on finish_reason=stop with no tool
// call (D-13/D-16), a budget trip (terminal Event with reason, D-04), or a real
// infra failure (the iter.Seq2 error slot, D-15).
func (a *LlmAgent) Run(ic InvocationContext) iter.Seq2[*Event, error] {
	spanID, parentSpanID := rootSpanIDs()
	requestID := ic.RequestID.String()

	return func(yield func(*Event, error) bool) {
		// skipBudgetGate carries the one recovery turn PAST the budget gate without
		// spending a step (Req#4, D-08): when a trip increments recoveryAttempts the
		// budget is already exhausted, so re-entering the loop normally would just
		// re-trip. The flag is set when the recovery nudge is injected and cleared
		// after that single turn, so the recovery call rides OUTSIDE ConsumeStep —
		// the ceiling becomes max_steps + 2 (one recovery + one finalize), enforced
		// by TestFinalizeOutsideBudget. It is loop-local (never on Budget) so a
		// parallel branch cannot observe a sibling's bypass.
		skipBudgetGate := false
		for {
			// 1. Budget gate BEFORE each LLM call — a trip forces recovery-or-
			// finalization (Req#2/#3). The recovery turn bypasses the gate via
			// skipBudgetGate so it never consumes a step (Req#4). On a real trip the
			// FIRST one (recoveryAttempts==0) injects a generic nudge and continues
			// one more turn; a SECOND trip routes to finalize (a non-empty terminal
			// Event, never the error slot — D-04). max_steps and wallclock share the
			// same ConsumeStep, only the reason differs.
			if skipBudgetGate {
				skipBudgetGate = false
			} else if ok, reason := ic.Budget.ConsumeStep(); !ok {
				if a.maybeRecover("") {
					skipBudgetGate = true
					continue
				}
				a.finalize(ic, spanID, parentSpanID, requestID, reason, yield)
				return
			}

			// 2. Per-call span + bounded call ctx (D-19 total timeout on the ctx).
			callCtx, cancel := context.WithTimeout(ic.Ctx,
				time.Duration(a.cfg.TotalTimeoutSec)*time.Second)
			spanCtx, span := startLLMSpan(callCtx)

			// The builder is the single assembly chokepoint (D-01): it reproduces the
			// byte-stable messages[0] and routes the provider-aware cache_control seam.
			// a.history stays read-only — the client never mutates it (Req#13). The
			// live <budget> block is tail-injected to a COPY (messages[0] untouched,
			// D-04): remaining is the shared balance, used = the steps this branch has
			// spent (Remaining never exceeds the start, so used = start-remaining is
			// the per-branch consumption — no MaxSteps() getter, landmine #11).
			budget := prompt.Budget{Used: ic.Budget.BranchConsumed(), Remaining: ic.Budget.Remaining(), Workspace: a.workspace}
			req := a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget)
			req.SessionID = a.sessionID
			ch, err := a.client.Stream(spanCtx, req)
			if err != nil {
				span.End()
				cancel()
				yield(nil, err) // REAL infra failure → error slot (D-15)
				return
			}

			text, calls, finish, usage, stopped := a.consume(ch, ic, spanID, parentSpanID, requestID, yield)
			setSpanAttrs(span, a.cfg.Model, a.cfg.Provider, requestID, usage)
			span.End()
			cancel()

			// The consumer broke mid-stream: consume already returned false from a
			// yield, so the iter.Seq2 contract forbids any further yield — re-yielding
			// the final/tool Event below would panic ("range function continued
			// iteration after... returned false"). Return now.
			if stopped {
				return
			}

			// 3. No tool calls → terminal (content-stop fallback, D-13/D-16).
			if len(calls) == 0 {
				// A completion with NO calls and NO content is a provider hiccup, not
				// an answer (observed live 2026-06-06: DeepSeek empty turn mid-task
				// ended the xlsx North-Star run silently). One nudged retry keeps the
				// task alive; a second empty routes to finalize — no path ever emits
				// an empty terminal (Req#2).
				if strings.TrimSpace(text) == "" {
					if a.maybeRecoverEmptyResponse() {
						continue
					}
					a.finalize(ic, spanID, parentSpanID, requestID, "empty_response", yield)
					return
				}
				answer := text
				if finish == "length" {
					answer += truncationNotice // D-21 — no auto-continue
				}
				a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: answer})
				yield(a.finalEvent(ic, spanID, parentSpanID, requestID, answer, finish, usage), nil)
				return
			}

			// 4. Intra-turn exclusivity (D-A1-07): if any call is an ask_user pause,
			// the assistant message is rewritten to ask_user-only tool_calls and the
			// siblings are dropped (re-emitted next round). Otherwise record the full
			// assistant tool-call message and dispatch sequentially (D-14).
			if pauses := a.pauseCalls(ic.Ctx, calls); len(pauses) > 0 {
				a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, ToolCalls: pauseToolCalls(pauses)})
				a.emitPauses(ic, spanID, parentSpanID, pauses, yield)
				return
			}
			a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, ToolCalls: calls})
			done, infraErr := a.dispatch(ic, spanID, parentSpanID, requestID, calls, usage, yield)
			if infraErr != nil {
				yield(nil, infraErr)
				return
			}
			if done {
				return
			}
			// loop: the next LLM call sees the appended RoleTool results.
		}
	}
}

// dispatch runs the assistant's tool calls in order (D-14). It returns done=true
// when the turn terminated (text_response or a dedup trip emitted a terminal
// Event); otherwise the loop continues. The error return is reserved for a real
// infra failure that must surface in the iter.Seq2 error slot — tool-arg /
// unknown-tool errors are NOT infra failures (D-15) and stay inline as RoleTool
// error messages.
func (a *LlmAgent) dispatch(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte, requestID string,
	calls []llm.ToolCall, usage llm.Usage, yield func(*Event, error) bool,
) (done bool, infraErr error) {
	for i := range calls {
		call := calls[i]

		if call.Function.Name == terminalTool {
			answer, perr := parseTextResponse(call.Function.Arguments)
			if perr != nil {
				// Malformed terminal args are NOT terminal: feed the error back so
				// the model self-corrects (D-15), bounded by the budget.
				a.appendToolError(call.ID, perr)
				if !yield(a.toolResultEvent(ic, spanID, parentSpanID, call.ID, "parse error"), nil) {
					return true, nil
				}
				continue
			}
			a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: answer})
			yield(a.finalEvent(ic, spanID, parentSpanID, requestID, answer, "stop", usage), nil)
			return true, nil
		}

		// Dedup gate (D-14): a repeated identical tool call trips with reason
		// "dedup". Like the budget trip, it forces recovery-or-finalization (Req#2/#3)
		// instead of a prose-less terminalBudgetEvent. The FIRST trip injects a
		// tool-specific nudge (call.Function.Name) and returns to the loop for one
		// more turn; the recovery turn re-issuing the same call re-trips dedup, and
		// the SECOND trip (recoveryAttempts>=1) routes to finalize — the intended
		// two-trips-one-nudge path (RESEARCH Open Question #2).
		canon := canonicalArgs(call.Function.Arguments)
		if dedup, reason := ic.Budget.BeforeToolCall(call.Function.Name, canon); dedup {
			if a.maybeRecover(call.Function.Name) {
				return false, nil
			}
			a.finalize(ic, spanID, parentSpanID, requestID, reason, yield)
			return true, nil
		}

		preview := a.runTool(ic.Ctx, ic.Budget, call)
		a.history = append(a.history, llm.Message{
			Role: llm.RoleTool, ToolCallID: call.ID, Content: preview,
		})
		ic.Budget.AfterToolResult(call.Function.Name, canon, []byte(preview))
		if !yield(a.toolResultEvent(ic, spanID, parentSpanID, call.ID, preview), nil) {
			return true, nil
		}
	}
	return false, nil
}

// runTool dispatches one tool call and returns the RoleTool history content. A
// missing tool or an Execute error becomes an error preview the model sees (D-15),
// never a panic. The shared spillover ctx is injected so large outputs page to a
// sidecar (D-25); the swarm ctx is also injected so a swarm_spawn dispatch can read
// the parent budget/registry/client/config off the ctx (the cycle-free seam — only
// swarm_spawn reads the key, every other tool ignores it).
func (a *LlmAgent) runTool(ctx context.Context, budget *Budget, call llm.ToolCall) string {
	tool, ok := a.registry.Get(call.Function.Name)
	if !ok {
		return fmt.Sprintf("error: unknown tool %q", call.Function.Name)
	}
	toolCtx := tools.WithToolCallContext(ctx, a.sessionID, call.ID, a.runDir, a.previewCap)
	toolCtx = WithSwarmContext(toolCtx, budget, a.registry, a.client, a.cfg, a.sessionID)
	res, err := tool.Execute(toolCtx, json.RawMessage(call.Function.Arguments))
	if err != nil {
		return fmt.Sprintf("error: %s", err.Error())
	}
	return res.Preview
}

// appendToolError appends a RoleTool error message for a malformed terminal-tool
// call so the model self-corrects (D-15).
func (a *LlmAgent) appendToolError(callID string, err error) {
	a.history = append(a.history, llm.Message{
		Role: llm.RoleTool, ToolCallID: callID, Content: "error: " + err.Error(),
	})
}

// consume drains one stream: it re-emits each text delta as a chunk Event, gathers
// finalized tool calls, the finish_reason, and the trailing usage. Tool-call Events
// are emitted as they finalize so the Event order is chunk -> tool_call (Req#9).
func (a *LlmAgent) consume(ch <-chan llm.Chunk, ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte,
	requestID string, yield func(*Event, error) bool,
) (text string, calls []llm.ToolCall, finish string, usage llm.Usage, stopped bool) {
	var b strings.Builder
	for c := range ch {
		switch {
		case c.Usage != nil:
			usage = *c.Usage
		case c.ToolCall != nil:
			calls = append(calls, *c.ToolCall)
			if !yield(a.toolCallEvent(ic, spanID, parentSpanID, *c.ToolCall), nil) {
				// Consumer stopped: drain the rest to let the client close cleanly,
				// then report stopped so Run never yields again (iter.Seq2 contract).
				for range ch { //nolint:revive // drain-to-close keeps the stream goroutine from leaking
				}
				return b.String(), calls, finish, usage, true
			}
		case c.Text != "":
			b.WriteString(c.Text)
			if !yield(a.chunkEvent(ic, spanID, parentSpanID, c.Text), nil) {
				for range ch { //nolint:revive // drain-to-close
				}
				return b.String(), calls, finish, usage, true
			}
		case c.FinishReason != "":
			finish = c.FinishReason
		}
	}
	return b.String(), calls, finish, usage, false
}

// parseTextResponse validates the terminal-tool arguments (D-13). A malformed or
// empty payload is an error the loop feeds back to the model, never a panic.
func parseTextResponse(rawArgs string) (string, error) {
	var a struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal([]byte(rawArgs), &a); err != nil {
		return "", fmt.Errorf("parse error: %w", err)
	}
	if strings.TrimSpace(a.Text) == "" {
		return "", fmt.Errorf("validation error: text_response.text is empty")
	}
	return a.Text, nil
}

// canonicalArgs canonicalizes a tool call's JSON arguments for the dedup
// fingerprint (the budget's caller-canonicalizes contract, B2). A non-JSON or
// empty payload falls back to the raw bytes so a malformed-arg storm still dedups
// on identical raw input.
func canonicalArgs(rawArgs string) []byte {
	var v any
	if err := json.Unmarshal([]byte(rawArgs), &v); err != nil {
		return []byte(rawArgs)
	}
	canon, err := canonicaljson.Marshal(v)
	if err != nil {
		return []byte(rawArgs)
	}
	return canon
}
