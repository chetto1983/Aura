// Package telegram — this file is the content renderer consumer (msg #2, streamed
// content). Full render (mdv2 escape + plain-text fallback on a 400 can't-parse-
// entities, tables→PNG sendPhoto, per-chat rate limit, 4096 cap) lands in Task 2;
// this is the constructor + drain skeleton the per-turn fanout wiring binds.
package telegram

import (
	"context"
	"time"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events"
	tele "gopkg.in/telebot.v4"
)

// renderer streams msg #2 for one turn: TEXT_MESSAGE_* deltas accumulate into the
// content message, sent MarkdownV2-escaped with a plain-text fallback, throttled.
type renderer struct {
	bot      botSender
	to       tele.Recipient
	throttle time.Duration
	chatRate time.Duration
}

// newRenderer builds a content renderer bound to a chat with the content throttle
// and the per-chat rate limit.
func newRenderer(bot botSender, to tele.Recipient, throttle, chatRate time.Duration) *renderer {
	return &renderer{bot: bot, to: to, throttle: throttle, chatRate: chatRate}
}

// consume drains the content subscriber channel to completion. The full render
// behavior is implemented in Task 2; this skeleton drains so the fanout producer
// closes cleanly (no leak) until then.
func (r *renderer) consume(ctx context.Context, ch <-chan events.Event) {
	for range ch {
		if ctx.Err() != nil {
			return
		}
	}
}
