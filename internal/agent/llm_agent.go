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
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/display"
	"github.com/chetto1983/aura/internal/agent/prompt"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/reasoningtrace"
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
	// activated is the set of deferred tool names promoted into the callable manifest
	// by tool_search (Claude Code parity: a deferred tool is not callable until its
	// schema is loaded). It is CONVERSATION-scoped: a fresh LlmAgent per turn seeds it
	// from the rehydrated history (deriveActivated), because a tool_search result keeps
	// the loaded schema visible in the transcript across turns. Written only by
	// promoteFromMeta in the serial dispatch result loop, and read by buildRequest and
	// the dispatch gate.
	activated map[string]struct{}
	// everLoaded is every deferred tool whose schema has been READ in this conversation,
	// with no expiry — a superset of activated, and deliberately NOT part of the manifest.
	// It exists because the dispatch gate was asking the wrong question. "Is this tool in
	// the manifest right now?" bounces a call the model made in good faith: the transcript
	// still shows the schema it read three turns ago, so its arguments are grounded, but
	// the grant had lapsed under the use-promotes rule. That cost a whole wasted LLM
	// round trip every time the model looked up two tools and used one. The question worth
	// asking is "are these arguments grounded in a schema the model actually saw?", and
	// this set answers exactly that while activated keeps doing its own job of holding the
	// manifest down.
	everLoaded map[string]struct{}
	previewCap int
	runDir     string
	sessionID  string
	workspace  string
	builder    *prompt.PromptBuilder
	hooks      *HookManager

	// gateway is the optional Phase-35 policy PEP (GATE-01): execTool calls its Decide
	// before tool.Execute. nil is an Allow no-op (dev-parity for tests/standalone).
	gateway *gateway.Gateway
	// ledgerConvID is the ORIGINATING conversation UUID the gateway ReservationKey is
	// keyed on. It defaults to sessionID (the main runner path, where session_id ==
	// conversation_id UUID); headless swarm/cron roots set it to the originating
	// conversation UUID so a flat session never keys the ledger (Open Q1).
	ledgerConvID string

	history []llm.Message

	// recoveryAttempts is the per-run recovery gate (D-08): a COUNTER, never a
	// one-shot boolean latch (CrewAI #1656 anti-pattern). The FIRST early-
	// termination trip (budget or dedup) increments it 0→1, injects a single
	// user-role nudge, and runs one more turn; a SECOND trip of any kind routes
	// straight to finalize(). It lives on LlmAgent (NOT Budget — shared by pointer
	// across the swarm tree, D-10 — and NOT InvocationContext) so recovery state is
	// per-run and never leaks between branches.
	recoveryAttempts int

	// sideEffected is set true once a Mutating tool (fs_write/shell_exec/etc.) has
	// been dispatched this run; the completion gate (D-43) only runs its critic on
	// a side-effecting turn — a pure read/chat turn skips it at zero extra cost.
	// completionAttempts is the gate's dedicated veto counter (max 1), orthogonal
	// to recoveryAttempts so a budget recovery does not consume the gate's one
	// veto. Both are per-run (a fresh LlmAgent per turn resets them).
	sideEffected       bool
	completionAttempts int

	// ledger is the verification evidence the verify-on-stop gate reads
	// (llm_agent_verification.go); nil disables that gate entirely, so a deployment
	// without the ledger degrades to no nudge rather than to a panic.
	// verificationAttempts is its own veto counter (max verificationMaxAttempts),
	// orthogonal to completionAttempts so one gate never spends the other's budget.
	// editedPaths accumulates the paths this run's write tools touched — written only
	// from the SERIAL result loop in dispatch, so it needs no lock. All three are
	// per-run (a fresh LlmAgent per turn resets them).
	ledger               VerificationLedger
	verificationAttempts int
	editedPaths          []string

	streamRetryUsed bool

	// truncatedToolTurns counts consecutive turns whose tool call was cut mid-JSON by
	// the output budget (finish_reason="length"). Per-run (a fresh LlmAgent per turn
	// resets it); maxTruncatedToolTurns bounds the nudge-then-finalize sequence so a
	// truncated tool call can never thrash (the 203-turn disaster, 2026-06-14).
	truncatedToolTurns int

	breaker *llm.Breaker

	// classifier is the local embedding-based reasoning-tier router (nil when no
	// embedder is wired). When present, adaptiveReasoningTier uses it instead of
	// the per-turn LLM router round-trip; the LLM router remains the fallback.
	classifier *prompt.ReasoningClassifier

	// reasoningOverride is the FIXED per-turn effort selected in the web Composer (37E),
	// threaded from runner.WithReasoningOverride via LlmAgentConfig. When non-empty the
	// Run loop SKIPS adaptiveReasoningTier and forces req.Reasoning through
	// BuildWithReasoningOverride on a reasoning target (OpenRouter OR llama.cpp, D-08);
	// empty is "auto" — today's adaptive path runs byte-identical (D-04, zero regression).
	reasoningOverride llm.ReasoningEffort

	// sources is the per-run URL-keyed source registry (D-05): web_search/web_fetch
	// results accumulate into it across the turn so the model sees ONE numbered
	// [n] Title — url list, threaded into prompt.Budget.Sources (the tail-inject copy
	// path, NEVER messages[0]). It also feeds the typed-display Payload emitted on each
	// web tool-result Event (DISP-01). A fresh LlmAgent per turn resets it, so the live
	// and replay paths each build an independent registry — identical by construction.
	sources *display.Registry
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
	// Classifier is the SHARED long-lived reasoning-tier classifier (anchors built
	// once, reused across turns). Production injects this via the Runner so the
	// static curated-anchor build is amortized rather than paid per turn.
	Classifier *prompt.ReasoningClassifier
	// Embedder is a convenience for tests/standalone construction:
	// when Classifier is nil and Embedder is set, NewLlmAgent builds a per-agent
	// classifier. Production leaves these unset and passes Classifier instead.
	Embedder prompt.Embedder
	// Breaker is the SHARED process-lifetime circuit breaker (B-05). The Runner owns
	// ONE breaker and injects it into every per-turn agent so a provider outage trips
	// cross-turn protection (a fresh per-agent breaker reset each turn and never
	// opened). nil => NewLlmAgent mints a fresh per-agent breaker (tests/standalone).
	Breaker *llm.Breaker
	// HookManager is the optional Phase-21 extension surface. nil is a no-op.
	HookManager *HookManager
	// Gateway is the optional Phase-35 policy PEP (GATE-01). nil is an Allow no-op
	// (dev-parity). The composition roots inject the one process-wide *gateway.Gateway.
	Gateway *gateway.Gateway
	// Ledger is the optional verification evidence ledger the verify-on-stop gate
	// reads. nil disables that gate (tests/standalone, and any deployment with no
	// Postgres pool behind NewEvidenceStore).
	Ledger VerificationLedger
	// LedgerConversationID is the ORIGINATING conversation UUID the gateway keys its
	// decision-fact ledger on. Empty defaults to SessionID in NewLlmAgent — correct for
	// the main runner path (session_id == conversation_id UUID); the headless swarm/cron
	// roots pass the originating conversation UUID so a flat session never keys the ledger.
	LedgerConversationID string
	// ReasoningOverride is the FIXED per-turn reasoning effort selected in the web
	// Composer (37E), threaded here by runner.buildAgent from runner.WithReasoningOverride
	// on ctx. When non-empty the agent BYPASSES the adaptive classifier and forces
	// req.Reasoning on a reasoning target (OpenRouter OR llama.cpp, D-08); empty is the
	// "auto" default, leaving today's adaptive path byte-identical (D-04, zero regression).
	ReasoningOverride llm.ReasoningEffort
}

// Run drives the budget-gated tool-dispatch loop (Req#9/#10). Termination paths:
// text_response (D-13), content-stop fallback on finish_reason=stop with no tool
// call (D-13/D-16), a budget trip (terminal Event with reason, D-04), or a real
// infra failure (the iter.Seq2 error slot, D-15).
func (a *LlmAgent) Run(ic InvocationContext) iter.Seq2[*Event, error] {
	spanID, parentSpanID := rootSpanIDs()
	requestID := ic.RequestID.String()
	slog.Info("agent turn start", "request_id", requestID, "thread_id", a.sessionID, "agent", a.name)

	// agent.turn wraps the whole loop (O-08): the per-call llm.request and per-tool
	// tool.execute spans nest under it via the threaded ctx, so a trace shows
	// per-turn and per-tool latency, not just the flat per-LLM-call coverage. The
	// span ends on EVERY Run return path through the deferred endTurnSpan over this
	// closure; turnReason carries the terminal outcome stamped at End.
	turnCtx, turnSpan := startTurnSpan(ic.Ctx, requestID, a.sessionID)
	// Thread the per-turn request_id onto the ctx so execTool can build the gateway
	// ReservationKey (conversation + request + tool_call) without a signature change on
	// the dispatch chain (Open Q2 — the smaller signature touch).
	turnCtx = tools.WithRequestID(turnCtx, requestID)
	ic = ic.WithContext(turnCtx)

	return func(yield func(*Event, error) bool) {
		turnReason := "incomplete"
		var activeLLMEnd *obs.BoundaryEnd
		defer func() {
			if err := a.hooks.OnTurnEnd(ic.Ctx, a.hookTurn(ic, turnReason)); err != nil {
				reasoningtrace.Record("agent_hook_turn_end_error", map[string]any{
					"request_id": requestID,
					"thread_id":  a.sessionID,
					"error":      err.Error(),
				})
			}
			recordTurnOutcome(turnReason)
			slog.Info("agent turn end", "request_id", requestID, "thread_id", a.sessionID, "outcome", turnReason)
			endTurnSpan(turnSpan, turnReason)
		}()
		defer func() {
			if r := recover(); r != nil {
				if activeLLMEnd != nil {
					activeLLMEnd.EndPanic()
					activeLLMEnd = nil
				}
				recordRecoveredPanic("llm_agent_run")
				turnReason = "panic"
				yield(nil, fmt.Errorf("agent panic: %v", r))
			}
		}()
		if err := a.hooks.OnTurnStart(ic.Ctx, a.hookTurn(ic, "")); err != nil {
			turnReason = "hook_turn_start_error"
			yield(nil, err)
			return
		}
		var adaptiveTier prompt.ReasoningTier
		var adaptiveTierSet bool
		var adaptiveTierOK bool
		var modelRoundOrdinal modelRoundOrdinal
		var retryRequest *llm.Request
		var retryRound modelRound
		// skipBudgetGate carries the one recovery turn PAST the budget gate without
		// spending a step (Req#4, D-08): when a trip increments recoveryAttempts the
		// budget is already exhausted, so re-entering the loop normally would just
		// re-trip. The flag is set when the recovery nudge is injected and cleared
		// after that single turn, so the recovery call rides OUTSIDE ConsumeStep —
		// the ceiling becomes max_steps + 2 (one recovery + one finalize), enforced
		// by TestFinalizeOutsideBudget. It is loop-local (never on Budget) so a
		// parallel branch cannot observe a sibling's bypass.
		skipBudgetGate := false
		// Turn-scoped, NOT loop-scoped: every LLM call below folds into it, so the
		// terminal Event reports what the whole turn cost instead of its last call.
		var turnU turnUsage
		for {
			// 1. Budget gate BEFORE each LLM call — a trip forces recovery-or-
			// finalization (Req#2/#3). The recovery turn bypasses the gate via
			// skipBudgetGate so it never consumes a step (Req#4). On a real trip the
			// FIRST one (recoveryAttempts==0) injects a generic nudge and continues
			// one more turn; a SECOND trip routes to finalize (a non-empty terminal
			// Event, never the error slot — D-04). max_steps and wallclock share the
			// same ConsumeStep, only the reason differs.
			//
			// recoveryTurn snapshots skipBudgetGate BEFORE it is reset below (fix-plan
			// 1.1): production (internal/runner/runner.go) drives ic.Ctx from
			// budget.WithDeadline, an ABSOLUTE deadline equal to the SAME wallclock
			// cutoff ConsumeStep just checked. On a wallclock trip ic.Ctx is therefore
			// ALREADY Done the instant this recovery turn rides the gate bypass — the
			// call at step 2 below would be dead-on-arrival unless its deadline is
			// severed for this ONE turn. Ordinary turns (recoveryTurn==false) are
			// UNCHANGED: they keep deriving straight from ic.Ctx, so a real deadline
			// still bounds them (no unbounding of the normal loop).
			transportRetry := retryRequest != nil
			recoveryTurn := skipBudgetGate
			if transportRetry {
				// Exact transport retry: the already-built primary request does not
				// consume another reasoning step.
			} else if skipBudgetGate {
				skipBudgetGate = false
			} else if ok, reason := ic.Budget.ConsumeStep(); !ok {
				if a.maybeRecover("") {
					skipBudgetGate = true
					continue
				}
				turnReason = reason
				a.finalize(ic, spanID, parentSpanID, requestID, reason, &turnU, yield)
				return
			}

			// 2. Per-call span + bounded call ctx (D-19 total timeout on the ctx).
			// The recovery turn severs an expired ic.Ctx deadline first (fix-plan 1.1,
			// see the recoveryTurn comment above); every other turn is byte-identical
			// to before.
			callParent := ic.Ctx
			if recoveryTurn {
				callParent = context.WithoutCancel(ic.Ctx)
			}
			callCtx, cancel := context.WithTimeout(callParent,
				time.Duration(a.cfg.TotalTimeoutSec)*time.Second)
			spanCtx, span := startLLMSpan(callCtx)

			// The builder is the single assembly chokepoint (D-01): it reproduces the
			// byte-stable messages[0] and routes the provider-aware cache_control seam.
			// a.history stays read-only — the client never mutates it (Req#13). The
			// live volatile hints are tail-injected to a COPY (messages[0] untouched,
			// D-04): remaining is the shared balance, used = the steps this branch has
			// spent (Remaining never exceeds the start, so used = start-remaining is
			// the per-branch consumption — no MaxSteps() getter, landmine #11). Current
			// time rides here too, not in the system prompt, so date-sensitive turns are
			// deterministic without poisoning the cached prefix.
			now := ic.Budget.Now()
			budget := prompt.Budget{
				Used:        ic.Budget.BranchConsumed(),
				Remaining:   ic.Budget.Remaining(),
				Workspace:   a.workspace,
				CurrentTime: now.Format(time.RFC3339),
				Today:       now.Format("2006-01-02"),
				// D-05: the volatile numbered source list rides the tail-inject copy
				// (RenderSourceList is "" until a web tool consulted a source this turn,
				// so a non-web turn keeps the byte-identical default). messages[0] stays
				// untouched — the static citation convention lives in the system prompt.
				Sources: a.sources.RenderSourceList(),
				// The roster of what is still unloaded, minus what this run already
				// promoted — so it shrinks as the turn goes and never tells the model to
				// load something it is already holding. It travels here rather than in
				// tool_search's Description for the same reason as Sources: the cached
				// prefix is the wrong distance from the decision.
				DeferredTools: a.registry.DeferredRoster(a.activated),
			}
			// A FIXED per-turn override (37E) bypasses the adaptive classifier entirely
			// (D-04/D-08): skip the reasoning-router round-trip so buildRequest can force
			// the selected effort. Auto (empty override) runs the classifier UNCHANGED.
			var req llm.Request
			var modelRound modelRound
			if transportRetry {
				req = *retryRequest
				modelRound = retryRound
				retryRequest = nil
			} else if a.reasoningOverride == "" && !adaptiveTierSet {
				adaptiveTier, adaptiveTierOK = a.adaptiveReasoningTier(ic.Ctx)
				adaptiveTierSet = true
			}
			if !transportRetry {
				modelRound = modelRoundOrdinal.next(ic.RequestID)
			}
			spanCtx = withModelRound(spanCtx, modelRound)
			var hookResult *ModelHookResult
			if !transportRetry {
				prepared, err := a.prepareReasoningRequest(
					spanCtx, budget, modelRound, adaptiveTier, adaptiveTierOK,
				)
				if err != nil {
					span.End()
					cancel()
					turnReason = "request_prepare_error"
					yield(nil, err)
					return
				}
				req = prepared.Request
				hookResult = prepared.HookResult
			}
			if !transportRetry {
				reasoningtrace.Record("agent_request_built", map[string]any{
					"request_id":          requestID,
					"model_round_ordinal": modelRound.ordinal,
					"thread_id":           a.sessionID,
					"provider":            a.cfg.Provider,
					"model":               req.Model,
					"max_tokens":          req.MaxTokens,
					"tool_choice":         req.ToolChoice,
					"tools_count":         len(req.Tools),
					"reasoning":           req.Reasoning,
					"history":             a.history,
				})
				if hookResult != nil {
					span.End()
					cancel()
					answer := normalizeContentStopAnswer(hookResult.Content)
					if hookResult.FinishReason == "" {
						hookResult.FinishReason = "hook"
					}
					if veto, feedback := a.gateCompletion(ic, answer); veto {
						a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: feedback})
						continue
					}
					a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: answer})
					turnReason = "hook_model_response"
					turnU.add(hookResult.Usage)
					yield(a.finalEvent(
						ic, spanID, parentSpanID, requestID, answer,
						hookResult.FinishReason, turnU.total(),
					), nil)
					return
				}
			}
			llmCtx, llmEnd := llmCallBoundary.Start(spanCtx)
			activeLLMEnd = llmEnd
			endLLM := func(err error) {
				llmEnd.End(err)
				activeLLMEnd = nil
			}
			ch, err := a.streamWithOpenRetry(llmCtx, req, requestID)
			if err != nil {
				endLLM(err)
				recordLLMError(llmErrorKind("stream_open", err))
				slog.Error("agent llm call error", "request_id", requestID, "thread_id", a.sessionID, "kind", llmErrorKind("stream_open", err), "err", err)
				span.End()
				cancel()
				if errors.Is(err, llm.ErrBreakerOpen) {
					// Breaker-open is graceful degradation, not an infra failure (B-06):
					// finalize from whatever was already gathered instead of surfacing the
					// iter.Seq2 error slot. finalize's own synthesis short-circuits the same
					// open breaker and falls back to the deterministic stub digest, so the
					// terminal Event is always non-empty.
					turnReason = "breaker_open"
					a.finalize(ic, spanID, parentSpanID, requestID, "breaker_open", &turnU, yield)
					return
				}
				turnReason = "stream_open_error"
				yield(nil, err) // REAL infra failure → error slot (D-15)
				return
			}

			text, calls, finish, usage, stopped, streamErr := a.consume(ch, ic, spanID, parentSpanID, requestID, yield)
			// Fold THIS call in before any exit path below reads the total. The span keeps
			// reporting the single call, which is what a span is: one request.
			turnU.add(usage)
			setSpanAttrs(span, a.cfg.Model, a.cfg.Provider, requestID, usage)
			span.End()
			cancel()

			// The consumer broke mid-stream: consume already returned false from a
			// yield, so the iter.Seq2 contract forbids any further yield — re-yielding
			// the final/tool Event below would panic ("range function continued
			// iteration after... returned false"). Return now.
			if stopped {
				endLLM(context.Canceled)
				turnReason = "consumer_stopped"
				return
			}
			if streamErr != nil {
				endLLM(streamErr)
				recordLLMError(llmErrorKind("stream", streamErr))
				slog.Error("agent llm call error", "request_id", requestID, "thread_id", a.sessionID, "kind", llmErrorKind("stream", streamErr), "err", streamErr)
				if !a.streamRetryUsed && retryableStreamOpenError(streamErr) {
					a.streamRetryUsed = true
					if a.breaker != nil {
						a.breaker.Failure(streamErr)
					}
					reasoningtrace.Record("agent_stream_mid_retry", map[string]any{
						"request_id": requestID,
						"thread_id":  a.sessionID,
						"error":      streamErr.Error(),
					})
					// Repudiate the partial chunks the failed attempt already streamed
					// (B-12) before the retry streams fresh — otherwise the consumer shows
					// partial+answer. A consumer stop here ends the run (iter.Seq2 contract:
					// no further yield after a false return).
					if !yield(a.discardStreamedEvent(ic, spanID, parentSpanID), nil) {
						turnReason = "consumer_stopped"
						return
					}
					retryRequest = &req
					retryRound = modelRound
					continue
				}
				turnReason = "stream_error"
				yield(nil, streamErr)
				return
			}
			endLLM(nil)
			slog.Info("agent llm call end", "request_id", requestID, "thread_id", a.sessionID, "finish_reason", finish, "tool_calls", len(calls))

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
					turnReason = "empty_response"
					a.finalize(ic, spanID, parentSpanID, requestID, "empty_response", &turnU, yield)
					return
				}
				answer := normalizeContentStopAnswer(text)
				if finish == "length" {
					answer += truncationNotice // D-21 — no auto-continue
				}
				// Verification gate: an edit with no fresh passing evidence gets one
				// deterministic follow-up, injected exactly like a completion veto. It
				// runs FIRST because it costs no model call and the completion critic
				// costs one — a turn the free gate sends back never pays for the paid one.
				if nudge, ok := a.gateVerification(); ok {
					a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: nudge})
					continue
				}
				// Completion gate (D-43): content-stop is also a voluntary termination.
				// No tool_call to attach feedback to here, so a veto appends only the
				// user-role nudge. The vetoed answer must not become durable history:
				// finalize copies history forward on budget trips.
				if veto, feedback := a.gateCompletion(ic, answer); veto {
					a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: feedback})
					continue
				}
				a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: answer})
				turnReason = "content_stop"
				yield(a.finalEvent(ic, spanID, parentSpanID, requestID, answer, finish, turnU.total()), nil)
				return
			}

			// A finish_reason="length" tool-call turn has truncated arguments (D-21
			// extended to tool calls): nudge once, finalize on a repeat — never dispatch
			// a truncated batch. Rationale + counter in llm_agent_truncation.go.
			switch a.classifyToolTruncation(finish) {
			case truncationFinalize:
				turnReason = "tool_args_truncated"
				a.finalize(ic, spanID, parentSpanID, requestID, "tool_args_truncated", &turnU, yield)
				return
			case truncationContinue:
				continue
			}

			// 4. Intra-turn exclusivity (D-A1-07): if any call is an ask_user pause,
			// the assistant message is rewritten to ask_user-only tool_calls and the
			// siblings are dropped (re-emitted next round). Otherwise record the full
			// assistant tool-call message and dispatch sequentially (D-14).
			if pauses := a.pauseCalls(ic.Ctx, calls); len(pauses) > 0 {
				a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, ToolCalls: pauseToolCalls(pauses)})
				turnReason = "ask_user_pause"
				a.emitPauses(ic, spanID, parentSpanID, pauses, yield)
				return
			}
			a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, ToolCalls: calls})
			done, infraErr := a.dispatch(ic, spanID, parentSpanID, requestID, calls, &turnU, yield)
			if infraErr != nil {
				turnReason = "dispatch_infra_error"
				yield(nil, infraErr)
				return
			}
			if done {
				turnReason = "tool_terminal"
				return
			}
			// loop: the next LLM call sees the appended RoleTool results.
		}
	}
}

// buildRequest selects the single per-turn request builder: the adaptive
// reasoning-tier variant when a tier was resolved this run, the plain builder
// otherwise. Exactly one builder runs — the discarded Build() (a wasted
// RenderToolDefs() per turn, QUAL-02/T7) is gone — and the chosen request is
// byte-identical to the old branch's chosen request (D-01 parity).
func (a *LlmAgent) buildRequest(budget prompt.Budget, tier prompt.ReasoningTier, tierOK bool) llm.Request {
	// Fixed per-turn override (37E): force the selected effort and bypass the adaptive
	// tier/plain selector. ApplyFixedReasoning gates on the generalized reasoning target
	// (OpenRouter OR llama.cpp, D-08); off-target it no-ops, so a non-reasoning backend
	// simply gets a plain build with the override inert.
	if a.reasoningOverride != "" {
		return a.builder.BuildWithReasoningOverride(a.history, a.registry, a.cfg.Provider, a.cfg, budget, a.reasoningOverride, a.activated)
	}
	if tierOK {
		return a.builder.BuildWithReasoningTier(a.history, a.registry, a.cfg.Provider, a.cfg, budget, tier, a.activated)
	}
	return a.builder.Build(a.history, a.registry, a.cfg.Provider, a.cfg, budget, a.activated)
}
