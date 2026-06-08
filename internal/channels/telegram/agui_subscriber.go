// Package telegram — this file is the per-turn AG-UI fanout consumer (the
// Phase-12 seam). It is the single biggest anti-re-implementation point of the
// phase (research §"Don't Hand-Roll"): it MUST consume internal/agui/fanout.go,
// never rebuild event distribution.
package telegram

import (
	"context"
	"strconv"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/google/uuid"
)

// eventConsumer drains one AG-UI subscriber channel to completion (the channel is
// closed by the Fanout producer on source-end/ctx-cancel). The status pane and the
// renderer each implement it. Declaring the seam lets handleTurn wire two
// consumers generically and lets the Task-1 fanout test inject recorders that
// prove the per-turn Subscribe×2-before-Run distribution without the full render
// stack.
type eventConsumer interface {
	consume(ctx context.Context, ch <-chan events.Event)
}

// consumerFactory builds the two per-turn consumers (status pane → msg #1, renderer
// → msg #2) bound to a chat. It is a field on Telegram so production wires the real
// status_pane/renderer (Task 2) and tests wire recorders. The default factory
// (set in New) builds the real consumers.
type consumerFactory func(bot botSender, to tele.Recipient) (status, content eventConsumer)

// handleTurn drives ONE turn through the per-turn fanout wiring (research §2 code
// example, verbatim):
//
//	Translate(convID, runID, idgen, runner.Turn(...))  → an AG-UI events.Event stream
//	NewFanout(translated)
//	statusCh := fo.Subscribe()   // status pane (msg #1)
//	contentCh := fo.Subscribe()  // content    (msg #2)   — BOTH before Run
//	fo.Run(ctx)                  // single producer goroutine, closes both chans on end/cancel
//	→ status pane consumes statusCh ; renderer consumes contentCh
//
// Subscribe MUST register BOTH consumers before Run (fanout.go:51 panics on
// Subscribe-after-Run); exactly one Fanout drives one turn (fanout.go:67 panics on
// double-Run). A fresh Fanout is built per turn — never one at channel start.
func (t *Telegram) handleTurn(ctx context.Context, bot botSender, chatID int64, userMsg string) {
	idgen := agui.NewIDGenerator()
	runID := uuid.NewString()

	translated := agui.Translate(convID(chatID), runID, idgen, t.deps.Turn(ctx, convID(chatID), &userMsg))
	fo := agui.NewFanout(translated)
	statusCh := fo.Subscribe()  // → status pane
	contentCh := fo.Subscribe() // → renderer  (BOTH before Run)
	fo.Run(ctx)

	to := tele.ChatID(chatID)
	pane, rend := t.consumers(bot, to)

	// The status pane consumes on its own goroutine (it edits msg #1 in place); the
	// renderer consumes inline on the handler goroutine (it streams msg #2 and
	// returns when the content channel closes). Both channels are closed by the sole
	// Fanout producer on source-end/ctx-cancel, so both consumers terminate without
	// a leak.
	done := make(chan struct{})
	go func() {
		defer close(done)
		pane.consume(ctx, statusCh)
	}()
	rend.consume(ctx, contentCh)
	<-done
}

// consumers builds the per-turn status + content consumers via the configured
// factory, falling back to the production status_pane/renderer when no factory was
// injected (the common path; tests inject recorders).
func (t *Telegram) consumers(bot botSender, to tele.Recipient) (status, content eventConsumer) {
	if t.deps.consumerFactory != nil {
		return t.deps.consumerFactory(bot, to)
	}
	pane := newStatusPane(bot, to, t.statusThrottle())
	rend := newRenderer(bot, to, t.contentThrottle(), t.chatRateLimit())
	return pane, rend
}

// convID derives the conversation id for a Telegram chat. One conversation per
// chat keeps the per-chat thread stable across turns (the runner persists it). The
// namespace is deterministic so the same chat always resolves the same thread.
//
// NOTE: this is the channel-side mapping; the composition root (plan 13-07) ensures
// the conversation row exists. Kept here so handleTurn is self-contained for the
// unit tier.
func convID(chatID int64) string {
	return uuid.NewSHA1(telegramChatNamespace, []byte(strconv.FormatInt(chatID, 10))).String()
}

// telegramChatNamespace is a fixed namespace so chat→conversation ids are stable
// and collision-free across restarts (a random-but-fixed UUID, not a well-known
// one, so it never collides with another subsystem's UUIDv5 namespace).
var telegramChatNamespace = uuid.MustParse("7e1e9a3a-0e3a-4b6c-9d2f-3a1b2c4d5e6f")
