package runner

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
)

// runner_steer.go carries the Runner-side inbox threading (Deps.Steer ->
// Runner.steer -> buildAgent's LlmAgentConfig.Steer) and drain-time
// persistence (persistSteerTurn), mirroring runner_reasoning.go's
// file-per-concern precedent so runner.go itself gets only a struct field, a
// Deps passthrough, and one buildAgent literal line (600-LOC ceiling).

// steerDeliveryForms are the two mid-run drain delivery values drainSteer
// (internal/agent/llm_agent_steer.go) stamps on each entry of
// Actions.SteerDelta["steers"]. persistSteerTurn fires ONLY for these two:
// 52-05's leftover auto-delivery form drives a whole follow-on turn whose OWN
// AppendTurn persists the leftover text as that turn's user message, so if
// persistSteerTurn also fired for it, one leftover would produce two
// byte-identical RoleUser rows — a reload could then tell an auto-delivered
// turn apart from a typed one, which is exactly what STEER-04 says must never
// happen.
var steerDeliveryForms = map[string]bool{
	"tool_result_append":    true,
	"user_message_fallback": true,
}

// steerInboxOrNil converts the Runner's concrete *steer.Inbox into the
// narrower agent.SteerInbox interface buildAgent's LlmAgentConfig literal
// needs, without the classic Go nil-interface trap: assigning a nil
// *steer.Inbox directly into an interface-typed struct field produces a
// NON-nil interface (type=*steer.Inbox, value=nil), so
// llm_agent_steer.go's `if a.steer == nil` guard would never fire and the
// first drainSteer call on a steer-off deployment (AURA_AGUI_RUN_STEER=false)
// would nil-pointer-panic inside Inbox.Drain. This helper is the one place
// that conversion happens, so there is exactly one nil check to get right,
// not one per call site.
func steerInboxOrNil(inbox *steer.Inbox) agent.SteerInbox {
	if inbox == nil {
		return nil
	}
	return inbox
}

// persistSteerTurn appends every mid-run-drained steer in ev.Actions.SteerDelta
// as its own RoleUser turn, in FIFO order, at the moment the drain Event
// arrives — so persisted order equals model-visible order equals wire order
// (amendment #132 item 5). It persists the RAW operator text ONLY, never the
// wrapUserSteer-marked form: the marker carries a per-run nonce
// (trust.go's toolOutputNonce()), and a nonce surviving into durable history
// would let a later rehydration replay it as though it were still live — the
// exact forgery D-07 exists to prevent.
//
// Wire validity: by the time either drain point runs, any tool-call batch in
// progress has already been fully dispatched and persisted (persistToolTurnEvent
// runs synchronously, strictly before the round loop reaches either drain
// point), so the new RoleUser turn this appends always lands AFTER the last
// persisted turn of a completed exchange — never between an assistant
// tool_calls turn and the tool-result turns answering it.
func (r *Runner) persistSteerTurn(ctx context.Context, tr *turnTracker, ev *agent.Event) error {
	delta := ev.Actions.SteerDelta
	if delta == nil {
		return nil
	}
	steers, ok := delta["steers"].([]map[string]any)
	if !ok {
		return nil
	}
	for _, s := range steers {
		delivery, _ := s["delivery"].(string)
		if !steerDeliveryForms[delivery] {
			continue
		}
		text, _ := s["text"].(string)
		if text == "" {
			continue
		}
		if err := r.Conv.AppendTurn(ctx, conversations.AppendTurnParams{
			ConversationID: tr.convID,
			Role:           llm.RoleUser,
			Content:        text,
		}); err != nil {
			return fmt.Errorf("persist steer turn: %w", err)
		}
	}
	return nil
}
