// Package telegram — this file is the status pane consumer (msg #1, edited in
// place). Full status-pane-B render (tool list 🟡→✅/❌, 💭 reasoning, running-cost
// footer, coalescing throttle) lands in Task 2; this is the constructor + drain
// skeleton the per-turn fanout wiring (agui_subscriber.go) binds.
package telegram

import (
	"context"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"
	"time"
)

// statusPane manages msg #1 for one turn: it is opened on RUN_STARTED and edited
// in place as tool/reasoning/cost events arrive, coalesced to the status throttle.
type statusPane struct {
	bot      botSender
	to       tele.Recipient
	throttle time.Duration
}

// newStatusPane builds a status pane bound to a chat with the status throttle.
func newStatusPane(bot botSender, to tele.Recipient, throttle time.Duration) *statusPane {
	return &statusPane{bot: bot, to: to, throttle: throttle}
}

// consume drains the status subscriber channel to completion. The full render
// behavior is implemented in Task 2; this skeleton drains so the fanout producer
// closes cleanly (no leak) until then.
func (p *statusPane) consume(ctx context.Context, ch <-chan events.Event) {
	for range ch {
		if ctx.Err() != nil {
			return
		}
	}
}
