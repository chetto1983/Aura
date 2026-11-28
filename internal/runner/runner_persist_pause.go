package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/approvaltext"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

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
func (r *Runner) persistPause(tr *turnTracker, ai *agent.AwaitingInput) error {
	if tr.parked {
		// The round's ParkedTurn committed this pause before its siblings ran.
		return nil
	}
	tr.paused = true
	tr.pauses = append(tr.pauses, ai)
	ins, err := r.pauseInsert(tr.convID, ai)
	if err != nil {
		return err
	}
	tr.pauseInserts = append(tr.pauseInserts, ins)
	return nil
}

// pauseInsert is the paused_states row of one pause, with a freshly minted token. Minted here
// so the runner stays the token source (RESEARCH gotcha #7); CommitPause writes the row.
func (r *Runner) pauseInsert(convID string, ai *agent.AwaitingInput) (askuser.InsertParams, error) {
	token, err := uuid.NewV7()
	if err != nil {
		return askuser.InsertParams{}, fmt.Errorf("mint pause token: %w", err)
	}
	options, err := pauseOptionsJSON(ai.Options)
	if err != nil {
		return askuser.InsertParams{}, err
	}
	resumeContext, err := r.pauseResumeContext(convID, ai)
	if err != nil {
		return askuser.InsertParams{}, err
	}
	// D-05 relay ids: a non-empty child id is forwarded as a non-nil *string (→
	// proxied_from_child_id); an empty one stays nil (→ SQL NULL for direct calls).
	var proxiedChild *string
	if ai.ProxiedFromChildID != "" {
		proxiedChild = &ai.ProxiedFromChildID
	}
	return askuser.InsertParams{
		Token:              token.String(),
		ConversationID:     convID,
		Kind:               ai.Kind,
		Question:           ai.Question,
		Options:            options,
		Priority:           ai.Priority,
		ToolCallID:         ai.ToolCallID,
		ResumeContext:      resumeContext,
		ProxiedFromChildID: proxiedChild,
		ProxiedToolCallID:  ai.ProxiedToolCallID,
	}, nil
}

// pauseResumeContext is the context a pause row stores. A call the host parked gets the
// tool_approval context, written from its Approval and never from anything streamed (D10). Any
// other context came from the model or a tool: it loses that reserved type, then gets the
// server's decision policy, challenge stamp and derived card.
func (r *Runner) pauseResumeContext(convID string, ai *agent.AwaitingInput) (json.RawMessage, error) {
	if ai.Approval != nil {
		parked, err := approvaltext.ToolApprovalContext(ai.Approval.ToolName, ai.Approval.Presentation)
		if err != nil {
			return nil, fmt.Errorf("persist pause approval: %w", err)
		}
		return ResumeContextWithDecisionPolicy(parked, allResumeDecisions())
	}
	resumeContext, err := withoutToolApprovalType(ai.ResumeContext)
	if err != nil {
		return nil, err
	}
	resumeContext, err = ResumeContextWithDecisionPolicy(resumeContext, allResumeDecisions())
	if err != nil {
		return nil, fmt.Errorf("persist pause decision policy: %w", err)
	}
	resumeContext, err = stampGatewayChallenge(resumeContext, convID, r.approvalChallenges)
	if err != nil {
		return nil, fmt.Errorf("persist pause challenge: %w", err)
	}
	resumeContext, err = approvaltext.Enrich(ai.Question, resumeContext)
	if err != nil {
		return nil, fmt.Errorf("persist pause presentation: %w", err)
	}
	return resumeContext, nil
}

// stampGatewayChallenge writes the gateway challenge's target and withheld tool_call_id into
// a relayed gateway_approval resume context, replacing whatever the model copied there (spec
// 2026-10-12). The model relays the coordinates; the server supplies what the operator reads
// and which call a decline refuses. The lookup is keyed on the pause's own conversation, the
// key the resume hook approves under, so a mismatch is a miss. A miss removes both fields: a
// target the model wrote never reaches the cockpit or Telegram. The model's copies go under
// any spelling, so no decoder that folds case can read one back. Other contexts pass through.
func stampGatewayChallenge(raw json.RawMessage, conversationID string, lookup ApprovalChallengeLookup) (json.RawMessage, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("approval resume_context must be a JSON object: %w", err)
	}
	if approvaltext.RootString(fields, "type") != "gateway_approval" {
		return raw, nil
	}
	for name := range fields {
		if strings.EqualFold(name, "target") || strings.EqualFold(name, "tool_call_id") {
			delete(fields, name)
		}
	}
	var target, toolCallID string
	found := false
	if lookup != nil {
		target, toolCallID, found = lookup(conversationID,
			approvaltext.RootString(fields, "tool"), approvaltext.RootString(fields, "args_sha256"))
	}
	if found {
		stamped := map[string]string{"tool_call_id": toolCallID}
		if target != "" {
			stamped["target"] = target
		}
		for name, value := range stamped {
			encoded, err := json.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("stamp %s: %w", name, err)
			}
			fields[name] = encoded
		}
	}
	return json.Marshal(fields)
}

// flushPause commits the round's pause exposure atomically (D-05/F-030): the SINGLE
// assistant ask_user tool_call turn (carrying ALL the round's calls — D-A1-07
// wire-correctness: the resume request must carry every original ask_user call so each
// injected RoleTool answer matches a real tool_call_id) + all N paused_states rows in ONE
// cross-store tx via the committer. The assistant turn goes in FIRST, so a pause is
// consumable (ListPendingAll/PendingFor) only AFTER its wire-valid history is durable; a
// tx failure leaves NEITHER, never an orphan tool answer on resume (the infinite-pause
// loop this closes). It runs once at round end, after la.Run drains, so the combined turn
// mirrors the agent's single-message rewrite. A park round committed its turn and pauses
// with its ParkedTurn, so a parked tracker has nothing left to flush.
func (r *Runner) flushPause(ctx context.Context, tr *turnTracker) error {
	if tr.parked || len(tr.pauses) == 0 {
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
	if err := r.resumeCommitter.CommitPause(ctx, assistantTurn, tr.pauseInserts, nil); err != nil {
		return fmt.Errorf("persist pause assistant turn: %w", err)
	}
	r.recordTurnDecision(ctx, tr)
	return nil
}

// assistantAskUserToolCalls reconstructs the ask_user assistant tool_calls from the
// round's pause payloads so the persisted assistant turn is wire-valid on resume:
// ONE assistant message carrying every ask_user call (CR-02). The arguments JSON
// mirrors the ask_user tool schema (question/kind/options) per call.
func assistantAskUserToolCalls(pauses []*agent.AwaitingInput) ([]byte, error) {
	out := make([]llm.ToolCall, 0, len(pauses))
	for _, ai := range pauses {
		if ai.OriginalToolName != "" {
			tc := llm.ToolCall{ID: ai.ToolCallID, Type: "function"}
			tc.Function.Name = ai.OriginalToolName
			tc.Function.Arguments = ai.OriginalArguments
			out = append(out, tc)
			continue
		}
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
