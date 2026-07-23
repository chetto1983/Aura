package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/google/uuid"
)

// turnTracker accumulates per-round persistence state: whether the round paused (so
// the auto-title worker is skipped), the running conversation id, and the round's
// ask_user pauses. When a round pauses on >=2 ask_user calls the agent rewrites its
// in-memory history to ONE assistant message carrying ALL the ask_user tool_calls
// (llm_agent.go pauseToolCalls), but the Runner observes one pause Event per call
// (emitPauses). To keep the persisted history wire-valid (CR-02), the pauses are
// accumulated here and the SINGLE combined assistant tool_call turn is written once
// at round end (flushPause), not one assistant turn per Event.
type turnTracker struct {
	convID string
	// userMsg is the round's user request, threaded here so the off-hot-path tool-
	// selection capture (persistToolTurn on ToolInvocationEnd) can pair it with the
	// executed tool name and feed the active-learning detector (Open-Q #3). Empty on a
	// resume round (userMsg=nil at turn start) — then no (request, used-tool) signal is
	// captured, which is correct (the request already produced its tool turns).
	userMsg string
	paused  bool
	pauses  []*agent.AwaitingInput // the round's ask_user pauses, flushed as ONE assistant turn
	// pauseInserts are the round's paused_states rows (each with its minted token),
	// accumulated by persistPause; flushPause writes them + the single assistant ask_user
	// tool_call turn in ONE cross-store tx so a pause is exposed only after durable history (D-05).
	pauseInserts []askuser.InsertParams

	// pendingToolCalls holds the current runnable tool batch until the first result
	// arrives, when the runner can persist one assistant turn carrying the whole
	// batch before appending the matching RoleTool turns.
	pendingToolCalls []llm.ToolCall
	openToolCalls    map[string]struct{}
	toolSeq          int

	// Display-only CoT accumulation (amendment #91 / fix-plan 1.12): the turn's
	// reasoning deltas joined in arrival order (interleaved spans across rounds),
	// rune-bounded by Runner.reasoningPersistMaxRunes, persisted onto the final
	// assistant answer turn. The observe/reset/read helpers live in
	// runner_reasoning_persist.go.
	reasoning          strings.Builder
	reasoningRunes     int
	reasoningTruncated bool
	reasoningFirst     time.Time // timestamp of the turn's first reasoning delta
	reasoningLast      time.Time // timestamp of the turn's last reasoning delta
}

func (t *turnTracker) nextToolInvocationSeq() int {
	t.toolSeq++
	return t.toolSeq
}

// persistEvent is the Event-sourced persistence seam (ADK AppendEvent-per-Event,
// D-A1-01). It persists exactly the turns a round produces and, on a pause Event,
// writes the paused_states row as the SOLE writer (T-04-19):
//
//   - a final Event (FinishReason set) → the assistant answer turn + its usage;
//   - a pause Event (Actions.AwaitingInput) → the paused_states row (SOLE writer) +
//     an accumulated pause payload; the SINGLE assistant ask_user tool_call turn
//     carrying ALL the round's calls is flushed once at round end (flushPause, CR-02),
//     so resume history carries one wire-valid assistant-with-N-tool_calls message.
//
// Streamed chunks and model tool_call announcements remain transport-only for
// conversation_turns. Executed-tool start/end facts also reconstruct the assistant
// tool_calls + RoleTool turns, while the append-only ledger stays best-effort
// observability.
func (r *Runner) persistEvent(ctx context.Context, tr *turnTracker, ev *agent.Event) error {
	if ev == nil {
		return nil
	}
	// B-12 repudiation: a mid-stream retry repudiates everything already streamed —
	// the accumulated CoT included — so the persisted reasoning mirrors what the
	// consumer actually rendered (the retry over a blank slate), never failed-attempt
	// deltas joined with their replay.
	if ev.Actions.DiscardStreamed {
		tr.resetReasoning()
	}
	if ev.Actions.AwaitingInput != nil {
		return r.persistPause(ctx, tr, ev.Actions.AwaitingInput)
	}
	if ev.Actions.ToolInvocation != nil {
		if err := r.persistToolTurn(ctx, tr, ev.Actions.ToolInvocation); err != nil {
			return err
		}
		// The ledger is operational observability (migration 0011 header), NOT a
		// permission system: a transient insert failure (pg hiccup, pool exhaustion,
		// mid-round cancel) must NOT abort the user-facing turn. Log and continue.
		// Contrast persistPause/persistAssistantAnswer, which gate durable history and
		// stay turn-fatal below.
		if err := r.persistToolInvocationLedger(ctx, tr, ev); err != nil {
			ti := ev.Actions.ToolInvocation
			slog.Warn("tool invocation ledger insert failed (continuing)",
				"tool", ti.ToolName, "tool_call_id", ti.ToolCallID, "event", ti.Event, "err", err)
		}
		return nil
	}
	if ev.LLMResponse != nil && ev.LLMResponse.Reasoning != "" {
		r.observeReasoning(tr, ev)
	}
	if ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
		return r.persistAssistantAnswer(ctx, tr, ev)
	}
	return nil
}

func (r *Runner) persistToolTurn(ctx context.Context, tr *turnTracker, ti *agent.ToolInvocation) error {
	if ti == nil || ti.ToolCallID == "" {
		return nil
	}
	switch ti.Event {
	case agent.ToolInvocationStart:
		tr.addPendingToolCall(ti)
		if ti.BatchSize > 0 && ti.BatchIndex == ti.BatchSize-1 {
			if err := r.flushToolCalls(ctx, tr); err != nil {
				return err
			}
		}
		return nil
	case agent.ToolInvocationEnd:
		if _, ok := tr.openToolCalls[ti.ToolCallID]; !ok {
			// Defensive repair for an end event observed without its start: synthesize
			// the assistant call from the end payload so we never persist an orphan
			// RoleTool turn.
			tr.addPendingToolCall(ti)
		}
		if err := r.flushToolCalls(ctx, tr); err != nil {
			return err
		}
		if err := r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{
			ConversationID: tr.convID,
			Role:           llm.RoleTool,
			Content:        ti.ResultPreview,
			ToolCallID:     ti.ToolCallID,
		}); err != nil {
			return fmt.Errorf("persist tool result turn %s/%s: %w", ti.ToolName, ti.ToolCallID, err)
		}
		// Open-Q #3 — the LIVE tool-selection capture site. This is the post-tool-
		// execution, ToolName-bearing, already-best-effort branch (the ledger insert next
		// to it is non-fatal too): the correct observation point for the (request,
		// used-tool) detection signal. Observe is a NON-BLOCKING handoff (CR-01): it only
		// enqueues the raw signal onto a bounded, drop-on-full channel and returns — the
		// mis-route detection (which embeds) AND the exemplar embed run on the learner's
		// intake worker, off this lock-held turn goroutine, so a hung embed sidecar can
		// never wedge the user turn. nil-safe and skipped on a resume round (userMsg empty).
		if r.toolSelectLearner != nil && tr.userMsg != "" {
			r.toolSelectLearner.Observe(tr.userMsg, ti.ToolName)
		}
		delete(tr.openToolCalls, ti.ToolCallID)
		if len(tr.openToolCalls) == 0 {
			tr.openToolCalls = nil
		}
		return nil
	default:
		return nil
	}
}

func (t *turnTracker) addPendingToolCall(ti *agent.ToolInvocation) {
	if t.openToolCalls == nil {
		t.openToolCalls = make(map[string]struct{})
	}
	if _, ok := t.openToolCalls[ti.ToolCallID]; ok {
		return
	}
	tc := llm.ToolCall{ID: ti.ToolCallID, Type: "function"}
	tc.Function.Name = ti.ToolName
	tc.Function.Arguments = ti.Arguments
	t.pendingToolCalls = append(t.pendingToolCalls, tc)
	t.openToolCalls[ti.ToolCallID] = struct{}{}
}

func (r *Runner) flushToolCalls(ctx context.Context, tr *turnTracker) error {
	if len(tr.pendingToolCalls) == 0 {
		return nil
	}
	raw, err := json.Marshal(tr.pendingToolCalls)
	if err != nil {
		return fmt.Errorf("marshal tool_calls: %w", err)
	}
	if err := r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: tr.convID,
		Role:           llm.RoleAssistant,
		ToolCalls:      raw,
	}); err != nil {
		return fmt.Errorf("persist assistant tool_calls turn: %w", err)
	}
	tr.pendingToolCalls = nil
	return nil
}

func (r *Runner) persistToolInvocationLedger(ctx context.Context, tr *turnTracker, ev *agent.Event) error {
	if r.toolInvocations == nil {
		// A nil ledger is a logged no-op, not a turn-killer: the ledger is observability,
		// not a required dependency for a turn that dispatches a tool. The prod
		// composition root injects one; a context without it (a test, a degraded boot)
		// simply does not record the forensic fact.
		slog.Warn("tool invocation ledger not wired; skipping ledger insert",
			"tool", ev.Actions.ToolInvocation.ToolName)
		return nil
	}
	ti := ev.Actions.ToolInvocation
	e := toolinvocations.Event{
		ConversationID: tr.convID,
		RequestID:      ev.RequestID.String(),
		ToolCallID:     ti.ToolCallID,
		ToolName:       ti.ToolName,
		Event:          ti.Event,
		Seq:            tr.nextToolInvocationSeq(),
		Timestamp:      toolInvocationTimestamp(ti, ev.Timestamp),
		StartedAt:      timePtrValue(ti.StartedAt),
		EndedAt:        timePtrValue(ti.EndedAt),
		DurationMS:     ti.DurationMS,
		// Arguments/ResultPreview are forwarded as-observed; the store's toParams seam
		// caps them to a byte ceiling and runs RedactForLedger over them BEFORE the
		// durable column (WR-02), so a secret the model placed on a command line lands
		// as [REDACTED] in the un-deletable ledger. The redaction lives at the store
		// boundary (store.go toParams) so it covers every emitter, not just this one.
		Arguments:         ti.Arguments,
		ArgsBytes:         ti.ArgsBytes,
		Status:            ti.Status,
		Error:             ti.Error,
		ResultPreview:     ti.ResultPreview,
		PreviewBytes:      ti.PreviewBytes,
		ResultBytes:       ti.ResultBytes,
		ResultTruncated:   ti.ResultTruncated,
		ResultSidecarPath: ti.ResultSidecarPath,
		ExitCode:          ti.ExitCode,
		Meta:              ti.Meta,
	}
	if err := r.toolInvocations.Insert(ctx, e); err != nil {
		return fmt.Errorf("persist tool invocation %s/%s: %w", ti.ToolName, ti.ToolCallID, err)
	}
	return nil
}

func timePtrValue(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return t.UTC()
}

func toolInvocationTimestamp(ti *agent.ToolInvocation, fallback time.Time) time.Time {
	if ti.Event == agent.ToolInvocationStart && ti.StartedAt != nil {
		return ti.StartedAt.UTC()
	}
	if ti.Event == agent.ToolInvocationEnd && ti.EndedAt != nil {
		return ti.EndedAt.UTC()
	}
	return fallback.UTC()
}

// persistAssistantAnswer persists the terminal assistant answer turn with its
// per-turn usage (read off the final Event's StateDelta, mirroring chat_render.go)
// plus the turn's accumulated display-only reasoning (amendment #91): the bounded
// CoT + first→last delta wall time land on the same row, "" / 0 mapping to NULL.
//
// Consistency contract: the assistant turn, conversation aggregate update, and
// cache_metrics row are persisted through the conversation store's atomic
// assistant-write seam. A metric failure rolls the turn back, so callers never see a
// failed turn while the conversation history already contains the answer.
func (r *Runner) persistAssistantAnswer(ctx context.Context, tr *turnTracker, ev *agent.Event) error {
	u := usageFromStateDelta(ev.Actions.StateDelta)
	// Prefer the provider's wire-reported cost (D-18); fall back to the seeded price
	// table (D-23) when the provider omits it, so a priced turn never persists $0 while
	// the displayed footer (same llm.CostUSD precedence) shows the table price. A
	// genuinely unknown model (no table entry) stays 0.0 — the honest numeric "n/a".
	cost := 0.0
	if v, ok := llm.CostUSDValue(r.cfg.Prices, r.cfg.Model, u.PromptTokens, u.CompletionTokens, u.Cost); ok {
		cost = v
	}
	reasoning, reasoningDurationMS := tr.persistedReasoning()
	turn := conversations.AppendTurnParams{
		ConversationID:      tr.convID,
		Role:                llm.RoleAssistant,
		Content:             ev.LLMResponse.Content,
		InputTokens:         u.PromptTokens,
		OutputTokens:        u.CompletionTokens,
		CachedTokens:        u.CachedTokens,
		CostUSD:             cost,
		Reasoning:           reasoning,
		ReasoningDurationMS: reasoningDurationMS,
	}
	metric, err := r.cacheMetricParams(tr.convID, 0, u, cost)
	if err != nil {
		return err
	}
	if err := r.Conv.AppendAssistantTurnWithCacheMetric(ctx, turn, metric); err != nil {
		return fmt.Errorf("persist assistant answer: %w", err)
	}
	return nil
}

// cacheMetricParams builds the per-turn cache_metrics row from the already-computed
// llm.Usage + cost. A nil cacheMetrics is a wiring bug, not a silent skip: the prod
// composition root MUST inject a Store, so the metric path is never unwired without
// notice.
func (r *Runner) cacheMetricParams(convID string, seq int, u llm.Usage, cost float64) (sqlc.InsertCacheMetricParams, error) {
	if r.cacheMetrics == nil {
		return sqlc.InsertCacheMetricParams{}, fmt.Errorf("persist cache metric: cacheMetrics store is nil (composition root must inject one)")
	}
	p, err := cachemetrics.NewInsertParams(cachemetrics.MetricParams{
		ConversationID: convID,
		Seq:            seq,
		PromptTokens:   u.PromptTokens,
		CachedTokens:   u.CachedTokens,
		CostUSD:        cost,
	})
	if err != nil {
		return sqlc.InsertCacheMetricParams{}, fmt.Errorf("persist cache metric: %w", err)
	}
	return p, nil
}

// persistPause mints the pause token and accumulates its paused_states InsertParams in
// the tracker; it NO LONGER writes the row (D-05/F-030). Deferring the insert to
// flushPause lets the assistant ask_user tool_call turn + all N pause rows commit in ONE
// cross-store tx, so a pause is exposed (ListPendingAll/PendingFor) only AFTER its
// wire-valid history is durable — never an orphan tool answer on resume. The token is
// minted here but never leaves the tracker before the flush insert, and the pause Event
// (agent.AwaitingInput) carries no token, so a consumer learns tokens only via post-flush
// store reads (A1); moving the insert therefore surfaces no token early. The combined
// assistant turn (from tr.pauses) is likewise flushed once at round end (CR-02): a round
// with >=2 ask_user calls collapses to ONE assistant message, so a per-Event assistant
// turn would be wire-invalid.
func (r *Runner) persistPause(ctx context.Context, tr *turnTracker, ai *agent.AwaitingInput) error {
	tr.paused = true
	tr.pauses = append(tr.pauses, ai)

	// A fresh token keys the pending. Minted here so persistPause stays the token source
	// (RESEARCH gotcha #7); the row itself is written by flushPause's CommitPause.
	token, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("mint pause token: %w", err)
	}
	options, err := pauseOptionsJSON(ai.Options)
	if err != nil {
		return err
	}
	// D-05 relay ids: a non-empty child id is forwarded as a non-nil *string (→
	// proxied_from_child_id); an empty one stays nil (→ SQL NULL for direct calls).
	var proxiedChild *string
	if ai.ProxiedFromChildID != "" {
		proxiedChild = &ai.ProxiedFromChildID
	}
	tr.pauseInserts = append(tr.pauseInserts, askuser.InsertParams{
		Token:              token.String(),
		ConversationID:     tr.convID,
		Kind:               ai.Kind,
		Question:           ai.Question,
		Options:            options,
		Priority:           ai.Priority,
		ToolCallID:         ai.ToolCallID,
		ResumeContext:      ai.ResumeContext,
		ProxiedFromChildID: proxiedChild,
		ProxiedToolCallID:  ai.ProxiedToolCallID,
	})
	return nil
}

// flushPause commits the round's pause exposure atomically (D-05/F-030): the SINGLE
// assistant ask_user tool_call turn (carrying ALL the round's calls — D-A1-07
// wire-correctness: the resume request must carry every original ask_user call so each
// injected RoleTool answer matches a real tool_call_id) + all N paused_states rows in ONE
// cross-store tx via the committer. The assistant turn goes in FIRST, so a pause is
// consumable (ListPendingAll/PendingFor) only AFTER its wire-valid history is durable; a
// tx failure leaves NEITHER, never an orphan tool answer on resume (the infinite-pause
// loop this closes). It runs once at round end, after la.Run drains, so the combined turn
// mirrors the agent's single-message rewrite.
func (r *Runner) flushPause(ctx context.Context, tr *turnTracker) error {
	if len(tr.pauses) == 0 {
		return nil
	}
	toolCalls, err := assistantAskUserToolCalls(tr.pauses)
	if err != nil {
		return err
	}
	assistantTurn := conversations.AppendTurnParams{
		ConversationID: tr.convID,
		Role:           llm.RoleAssistant,
		ToolCalls:      toolCalls,
	}
	if err := r.resumeCommitter.CommitPause(ctx, assistantTurn, tr.pauseInserts); err != nil {
		return fmt.Errorf("persist pause assistant turn: %w", err)
	}
	return nil
}

// assistantAskUserToolCalls reconstructs the ask_user assistant tool_calls from the
// round's pause payloads so the persisted assistant turn is wire-valid on resume:
// ONE assistant message carrying every ask_user call (CR-02). The arguments JSON
// mirrors the ask_user tool schema (question/kind/options) per call.
func assistantAskUserToolCalls(pauses []*agent.AwaitingInput) ([]byte, error) {
	out := make([]llm.ToolCall, 0, len(pauses))
	for _, ai := range pauses {
		args := map[string]any{"question": ai.Question, "kind": ai.Kind}
		if len(ai.Options) > 0 {
			opts := make([]map[string]string, len(ai.Options))
			for i, o := range ai.Options {
				opts[i] = map[string]string{"label": o.Label, "value": o.Value}
			}
			args["options"] = opts
		}
		if ai.Priority != 0 {
			args["priority"] = ai.Priority
		}
		if len(ai.ResumeContext) > 0 {
			args["resume_context"] = json.RawMessage(ai.ResumeContext)
		}
		argsJSON, err := json.Marshal(args)
		if err != nil {
			return nil, fmt.Errorf("marshal ask_user args: %w", err)
		}
		tc := llm.ToolCall{ID: ai.ToolCallID, Type: "function"}
		tc.Function.Name = "ask_user"
		tc.Function.Arguments = string(argsJSON)
		out = append(out, tc)
	}
	calls, err := json.Marshal(out)
	if err != nil {
		return nil, fmt.Errorf("marshal tool_calls: %w", err)
	}
	return calls, nil
}

// pauseOptionsJSON marshals the pause options to the jsonb the paused_states row
// stores (nil → SQL NULL via a nil slice).
func pauseOptionsJSON(opts []agent.PauseOption) (json.RawMessage, error) {
	if len(opts) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(opts)
	if err != nil {
		return nil, fmt.Errorf("marshal pause options: %w", err)
	}
	return b, nil
}

// usageFromStateDelta reconstructs the per-turn llm.Usage the LlmAgent stamped into
// the final Event's StateDelta (mirrors cmd/aura/chat_render.go usageFromStateDelta;
// duplicated here so the runner package does not import cmd/aura).
func usageFromStateDelta(d map[string]any) llm.Usage {
	if d == nil {
		return llm.Usage{}
	}
	u := llm.Usage{
		PromptTokens:     anyInt(d["prompt_tokens"]),
		CompletionTokens: anyInt(d["completion_tokens"]),
		CachedTokens:     anyInt(d["cache_hit_tokens"]),
	}
	if c, ok := anyFloat(d["cost_usd"]); ok {
		u.Cost = &c
	}
	return u
}

func anyInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		// A UseNumber decoder or a jsonb round-trip widens token counts to
		// json.Number (M-07); parse it instead of silently zeroing the usage row.
		if i, err := n.Int64(); err == nil {
			return int(i)
		}
		return 0
	default:
		return 0
	}
}

// anyFloat mirrors anyInt's type set and adds a json.Number case (IN-03): the
// StateDelta is decoded with UseNumber (event.go), so cost_usd can arrive as a
// json.Number rather than a float64. Without the json.Number/int64 cases a priced
// turn would silently fall back to the price table instead of using the wire cost.
func anyFloat(v any) (float64, bool) {
	switch f := v.(type) {
	case float64:
		return f, true
	case int:
		return float64(f), true
	case int64:
		return float64(f), true
	case json.Number:
		n, err := f.Float64()
		return n, err == nil
	default:
		return 0, false
	}
}
