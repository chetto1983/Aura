package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// turnTracker accumulates per-round persistence state: whether the round paused (so
// the auto-title worker is skipped) and the running conversation id.
type turnTracker struct {
	convID string
	paused bool
}

// persistEvent is the Event-sourced persistence seam (ADK AppendEvent-per-Event,
// D-A1-01). It persists exactly the turns a round produces and, on a pause Event,
// writes the paused_states row as the SOLE writer (T-04-19):
//
//   - a final Event (FinishReason set) → the assistant answer turn + its usage;
//   - a pause Event (Actions.AwaitingInput) → the assistant ask_user tool_call turn
//     (so resume history carries the question) + the paused_states row.
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
	return r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: convID,
		Seq:            seq,
		Role:           llm.RoleAssistant,
		Content:        ev.LLMResponse.Content,
		InputTokens:    u.PromptTokens,
		OutputTokens:   u.CompletionTokens,
		CachedTokens:   u.CachedTokens,
		CostUSD:        cost,
	})
}

// persistPause writes the assistant ask_user tool_call turn (so resume history
// carries the original question→answer pair, SC-4) AND the paused_states row. The
// Runner is the SOLE writer of paused_states (T-04-19): only this path calls Insert.
func (r *Runner) persistPause(ctx context.Context, tr *turnTracker, ai *agent.AwaitingInput) error {
	tr.paused = true

	// 1. Persist the assistant ask_user-only tool_call turn (D-A1-07 wire-correctness:
	//    the resume request must carry the original ask_user call so the injected
	//    RoleTool answer matches a real tool_call_id, never a duplicate).
	toolCalls, err := assistantAskUserToolCalls(ai)
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

	// 2. Write the paused_states row (SOLE writer). A fresh token keys the pending.
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

// assistantAskUserToolCalls reconstructs the ask_user assistant tool_call from the
// pause payload so the persisted assistant turn is wire-valid on resume. The
// arguments JSON mirrors the ask_user tool schema (question/kind/options).
func assistantAskUserToolCalls(ai *agent.AwaitingInput) ([]byte, error) {
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
	calls, err := json.Marshal([]llm.ToolCall{tc})
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
