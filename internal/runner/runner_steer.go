package runner

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/google/uuid"
)

// runner_steer.go carries the Runner-side inbox threading (Deps.Steer ->
// Runner.steer -> buildAgent's LlmAgentConfig.Steer), drain-time persistence
// (persistSteerTurn), and the leftover-steer auto-delivery wrap
// (deliverLeftoverSteer, 52-05), mirroring runner_reasoning.go's
// file-per-concern precedent so runner.go itself gets only a struct field, a
// Deps passthrough, and one wrapping call site in runTurn (600-LOC ceiling).

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

// steerAutoDeliverMaxChain bounds leftover-steer auto-delivery to ONE
// follow-on turn per outer Turn/runTurn call (T-52-21, D-09): a steer that
// arrives DURING the auto-delivered turn is handled by THAT turn's own two
// drain points (or, if it too ends with a leftover, becomes the NEXT outer
// call's problem) — never chased by a second auto-delivery inside this call.
// A named constant, not an inline "just do it once", so the loop below makes
// the bound structural: unbounded recursion is impossible by construction,
// not merely unlikely (T-52-21's own mitigation plan).
const steerAutoDeliverMaxChain = 1

// steerDeliveryAutoNextTurn is the delivery form 52-04 reserved and
// deliberately excluded from steerDeliveryForms above (see that var's doc
// comment): this plan's leftover auto-delivery drives its OWN follow-on turn
// through turnLocked's ordinary appendUserTurn/AppendTurn path, so the
// drain-time persistSteerTurn branch must never ALSO persist it — that would
// be the exact double-write T-52-26 exists to close.
const steerDeliveryAutoNextTurn = "auto_delivery_next_turn"

// steerAutoDeliveryNotice is the load-bearing, byte-stable line prefixed onto
// the auto-delivered follow-on turn's content (D-09; wording is Claude's
// Discretion, the property is not). Both the visible/persisted turn and the
// model see it — it is what makes STEER-04's truth literally true: "a reload
// cannot tell an auto-delivered turn from a typed one EXCEPT by the visible
// line", since both persist through the identical AppendTurn path and this
// prefix is the one difference. A structural consumer (the cockpit, a future
// Telegram render) does not need to parse this prose at all: it reads the
// aura.steer echo frame's per-steer "delivery" field
// (steerDeliveryAutoNextTurn) instead — this string only ever needs to be
// read by a human.
const steerAutoDeliveryNotice = "The previous turn ended before this message could be delivered, so it is being sent now as a new message:"

// deliverLeftoverSteer wraps the iterator turnLocked returns for ONE turn:
// every event of inner passes through unchanged (honoring the yield-after-
// false contract — once the consumer stops, nothing further is pulled from
// inner or yielded), and only once inner is fully exhausted does it check the
// conversation's steer inbox ONE final time. Non-empty ⇒ a steer was accepted
// (202) while the run was live but never reached a drain point before the
// round ended — exactly the loss window STEER-04 exists to close. It then
// emits ONE aura.steer-carrying Event naming the next-turn delivery form,
// reusing 52-02/52-04's SAME payload keys (never a parallel shape, never a
// second translator.go branch), and drives ONE more real turn — turnLocked,
// under the SAME already-held lock ctx passed in by runTurn, NEVER
// Turn/runTurn, which would attempt a fresh Lock() against a mutex this
// goroutine already holds and deadlock — whose user message is the leftover
// text(s) joined FIFO behind the visible notice line. A nil inbox or an empty
// drain is a total no-op: no extra Event, no extra turn, no extra
// persistence, no extra LLM call, byte-identical to the no-steering path.
func (r *Runner) deliverLeftoverSteer(ctx context.Context, convID string, inner iter.Seq2[*agent.Event, error]) iter.Seq2[*agent.Event, error] {
	return func(yield func(*agent.Event, error) bool) {
		for ev, err := range inner {
			if !yield(ev, err) {
				return
			}
		}
		if r.steer == nil {
			return
		}
		for range steerAutoDeliverMaxChain {
			msgs := r.steer.Drain(convID)
			if len(msgs) == 0 {
				return
			}
			noticeID, err := uuid.NewV7()
			if err != nil {
				yield(nil, fmt.Errorf("mint leftover steer notice id: %w", err))
				return
			}
			if !yield(leftoverSteerNoticeEvent(noticeID, convID, msgs), nil) {
				return
			}
			leftover := joinSteerLeftovers(msgs)
			followInput := turnInput{visibleUserMsg: &leftover, modelUserMsg: &leftover}
			for ev, err := range r.turnLocked(ctx, convID, followInput) {
				if !yield(ev, err) {
					return
				}
			}
			// A steer queued DURING the follow-on turn above is this turn's OWN
			// problem (its two drain points, or its own leftover on the NEXT outer
			// call) — the hop cap above stops a second auto-delivery from ever being
			// attempted within THIS call.
		}
	}
}

// joinSteerLeftovers joins N leftover steers' RAW text, FIFO, into the SINGLE
// next-turn message D-09 describes ("delivered automatically as the next
// user turn", singular) — never N separate follow-on turns — prefixed with
// the visible notice line so the transcript says out loud what happened.
func joinSteerLeftovers(msgs []steer.Message) string {
	texts := make([]string, 0, len(msgs))
	for _, m := range msgs {
		texts = append(texts, m.Text)
	}
	return steerAutoDeliveryNotice + "\n\n" + strings.Join(texts, "\n\n")
}

// leftoverSteerNoticeEvent builds the aura.steer echo Event for the leftover
// case, reusing 52-02/52-04's EXACT payload keys
// (conversation_id/round/steers[].{id,source,text,delivery}) — never a
// parallel shape — so translator.go's existing SteerDelta branch fans it out
// with no second CUSTOM-event branch. "round" is the zero value: this fires
// strictly BETWEEN rounds, after the agent's own Run() has already returned,
// so there is no live modelRound ordinal to carry. Mirrors
// runner_fastpath.go's fastReplyEvent construction convention for a
// runner-minted (not agent-minted) Event.
func leftoverSteerNoticeEvent(id uuid.UUID, convID string, msgs []steer.Message) *agent.Event {
	steers := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		steers = append(steers, map[string]any{
			"id":       m.ID,
			"source":   m.Source,
			"text":     m.Text,
			"delivery": steerDeliveryAutoNextTurn,
		})
	}
	return &agent.Event{
		RequestID: id,
		Author:    "aura",
		ThreadID:  convID,
		Actions: agent.Actions{
			SteerDelta: map[string]any{
				"conversation_id": convID,
				"round":           uint32(0),
				"steers":          steers,
			},
		},
		Timestamp: time.Now().UTC(),
	}
}
