package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
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
	paused bool
	pauses []*agent.AwaitingInput // the round's ask_user pauses, flushed as ONE assistant turn
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
// Streamed chunk / tool_call / tool_result Events are transport-only here (the
// LlmAgent threads tool results into its own in-memory history for the round; the
// durable record is the assistant answer/pause). This keeps LoadHistory a function
// of completed turns, not mid-stream deltas.
func (r *Runner) persistEvent(ctx context.Context, tr *turnTracker, ev *agent.Event) error {
	if ev == nil {
		return nil
	}
	if ev.Actions.AwaitingInput != nil {
		return r.persistPause(ctx, tr, ev.Actions.AwaitingInput)
	}
	if ev.LLMResponse != nil && ev.LLMResponse.FinishReason != "" {
		return r.persistAssistantAnswer(ctx, tr.convID, ev)
	}
	return nil
}

// persistAssistantAnswer persists the terminal assistant answer turn with its
// per-turn usage (read off the final Event's StateDelta, mirroring chat_render.go).
//
// Consistency contract (WR-03): the assistant turn and its cache_metrics row are two
// separate writes (the conversations and cachemetrics Stores share no tx seam). The turn
// is the load-bearing record; the metric is an append-only observation. If the metric
// write fails the turn fails too (no silent drop — no-skip discipline), but the metric
// INSERT is idempotent on (conversation_id, seq) via ON CONFLICT DO NOTHING, so a retry
// re-records the same seq's metric as a no-op, never a duplicate. (Assistant-turn
// duplication on a fresh-seq retry is a property of the CountTurns/AppendTurn seq model
// shared by every turn write here, not specific to the metric; closing it fully needs a
// shared transaction across the two Stores — deferred as out of phase scope.)
func (r *Runner) persistAssistantAnswer(ctx context.Context, convID string, ev *agent.Event) error {
	seq, err := r.nextSeq(ctx, convID)
	if err != nil {
		return err
	}
	u := usageFromStateDelta(ev.Actions.StateDelta)
	cost := 0.0
	if u.Cost != nil {
		cost = *u.Cost
	}
	if err := r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: convID,
		Seq:            seq,
		Role:           llm.RoleAssistant,
		Content:        ev.LLMResponse.Content,
		InputTokens:    u.PromptTokens,
		OutputTokens:   u.CompletionTokens,
		CachedTokens:   u.CachedTokens,
		CostUSD:        cost,
	}); err != nil {
		return err
	}
	return r.persistCacheMetric(ctx, convID, seq, u, cost)
}

// persistCacheMetric writes the per-turn append-only cache_metrics row from the
// already-computed llm.Usage + cost (D-02a: no wire-path touch — the same numbers that
// fed the conversation turn). A nil cacheMetrics is a wiring bug, not a silent skip:
// the prod composition root MUST inject a Store (no-skip discipline), so the metric is
// never dropped without notice.
func (r *Runner) persistCacheMetric(ctx context.Context, convID string, seq int, u llm.Usage, cost float64) error {
	if r.cacheMetrics == nil {
		return fmt.Errorf("persist cache metric: cacheMetrics store is nil (composition root must inject one)")
	}
	p, err := cachemetrics.NewInsertParams(cachemetrics.MetricParams{
		ConversationID: convID,
		Seq:            seq,
		PromptTokens:   u.PromptTokens,
		CachedTokens:   u.CachedTokens,
		CostUSD:        cost,
	})
	if err != nil {
		return fmt.Errorf("persist cache metric: %w", err)
	}
	if err := r.cacheMetrics.Insert(ctx, p); err != nil {
		return fmt.Errorf("persist cache metric: %w", err)
	}
	return nil
}

// persistPause writes the paused_states row for one ask_user pause (SOLE writer,
// T-04-19) and accumulates the pause payload in the tracker. The assistant
// ask_user tool_call turn is NOT written here: when a round emits >=2 ask_user
// calls the agent collapses them into ONE assistant message (pauseToolCalls), so
// writing a separate assistant turn per Event would yield a wire-invalid
// interleaving on resume (CR-02). The combined assistant turn is flushed once at
// round end by flushPause.
func (r *Runner) persistPause(ctx context.Context, tr *turnTracker, ai *agent.AwaitingInput) error {
	tr.paused = true
	tr.pauses = append(tr.pauses, ai)

	// Write the paused_states row (SOLE writer). A fresh token keys the pending.
	token, err := uuid.NewV7()
	if err != nil {
		return fmt.Errorf("mint pause token: %w", err)
	}
	options, err := pauseOptionsJSON(ai.Options)
	if err != nil {
		return err
	}
	if err := r.pause.Insert(ctx, askuser.InsertParams{
		Token:          token.String(),
		ConversationID: tr.convID,
		Kind:           ai.Kind,
		Question:       ai.Question,
		Options:        options,
		Priority:       ai.Priority,
		ToolCallID:     ai.ToolCallID,
	}); err != nil {
		return fmt.Errorf("insert paused state: %w", err)
	}
	return nil
}

// flushPause writes the SINGLE assistant ask_user tool_call turn carrying ALL the
// round's ask_user calls (D-A1-07 wire-correctness: the resume request must carry
// every original ask_user call so each injected RoleTool answer matches a real
// tool_call_id, never a duplicate, and the assistant-with-N-tool_calls message is
// immediately followed by its N tool answers). It runs once at round end, after
// la.Run drains, so the combined turn mirrors the agent's single-message rewrite.
func (r *Runner) flushPause(ctx context.Context, tr *turnTracker) error {
	if len(tr.pauses) == 0 {
		return nil
	}
	toolCalls, err := assistantAskUserToolCalls(tr.pauses)
	if err != nil {
		return err
	}
	seq, err := r.nextSeq(ctx, tr.convID)
	if err != nil {
		return err
	}
	if err := r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: tr.convID,
		Seq:            seq,
		Role:           llm.RoleAssistant,
		ToolCalls:      toolCalls,
	}); err != nil {
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
	default:
		return 0
	}
}

func anyFloat(v any) (float64, bool) {
	switch f := v.(type) {
	case float64:
		return f, true
	case int:
		return float64(f), true
	default:
		return 0, false
	}
}
