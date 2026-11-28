package runner

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/approvaltext"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/toolapprovals"
)

// recordBriefRunes is the CHECK bound of aura.tool_approvals.brief, in characters. Brief stays
// far below it; the cut keeps a card from rolling the whole park round back.
const recordBriefRunes = 2048

// commitParkedTurn commits a park round before any of its siblings runs (D19, T3-I2): the
// assistant turn with every call the model kept, every pause row of the round (drafts and host
// review included) and the record of each call the host parked, in one CommitPause
// transaction. The siblings' results land after it, through AppendTurn, which can spill.
func (r *Runner) commitParkedTurn(ctx context.Context, tr *turnTracker, turn *agent.ParkedTurn) error {
	inserts := make([]askuser.InsertParams, 0, len(turn.Pauses))
	var records []toolapprovals.Input
	parked := make(map[string]bool, len(turn.Pauses))
	for _, ai := range turn.Pauses {
		ins, err := r.pauseInsert(tr.convID, ai)
		if err != nil {
			return err
		}
		inserts = append(inserts, ins)
		parked[ai.ToolCallID] = true
		if ai.Approval != nil {
			records = append(records, parkedCallRecord(identityctx.IdentityID(ctx), ins, ai.Approval))
		}
	}
	// What the model wrote: a BeforeTool hook that rewrites a sibling changes the call that is
	// dispatched, not this turn.
	calls, err := json.Marshal(turn.Calls)
	if err != nil {
		return fmt.Errorf("marshal parked tool_calls: %w", err)
	}
	assistantTurn := conversations.AppendTurnParams{ConversationID: tr.convID, Role: llm.RoleAssistant, ToolCalls: calls}
	if err := r.resumeCommitter.CommitPause(ctx, assistantTurn, inserts, records); err != nil {
		return fmt.Errorf("persist parked turn: %w", err)
	}
	tr.paused, tr.parked = true, true
	tr.persistedCalls = make(map[string]bool, len(turn.Calls))
	for _, call := range turn.Calls {
		if !parked[call.ID] {
			tr.persistedCalls[call.ID] = false
		}
	}
	r.recordTurnDecision(ctx, tr)
	return nil
}

// parkedCallRecord is the record of a parked call, keyed on its pause. CommitPause stamps the
// assistant turn's seq.
func parkedCallRecord(identityID string, pause askuser.InsertParams, a *agent.ParkedApproval) toolapprovals.Input {
	return toolapprovals.Input{
		IdentityID: identityID, ConversationID: pause.ConversationID, PauseToken: pause.Token,
		ToolCallID: pause.ToolCallID, CallIndex: a.CallIndex, ToolName: a.ToolName,
		Source: toolapprovals.Source(a.Source), ExecuteArgs: a.ExecuteArgs, Fingerprint: a.Fingerprint,
		Brief:     headRunes(approvaltext.Brief(a.Presentation), recordBriefRunes),
		GrantTool: a.SubjectTool, GrantAction: a.SubjectAction,
	}
}

// withoutToolApprovalType drops the reserved type from a context the model wrote (D10): a
// surface trusts a tool_approval card as stored, so only the runner may write that type, from
// the call it parked.
func withoutToolApprovalType(raw json.RawMessage) (json.RawMessage, error) {
	if !approvaltext.IsToolApproval(raw) {
		return raw, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return nil, fmt.Errorf("strip tool_approval type: %w", err)
	}
	delete(fields, "type")
	return json.Marshal(fields)
}

// recordsLedger reports whether a tool event gets a ledger row. A park round's call that never
// started (a dedup trip's or a held call's synthetic result) closes no start, so its end is
// skipped, as the plain dedup path writes none.
func (t *turnTracker) recordsLedger(ti *agent.ToolInvocation) bool {
	started, persisted := t.persistedCalls[ti.ToolCallID]
	return ti.Event != agent.ToolInvocationEnd || !persisted || started
}
