// Package telegramadapter wraps the chat Hub InboundAdapter/OutboundAdapter
// pair for the Telegram channel. It keeps all tele.* coupling under the
// Telegram channel adapter so the chat core stays channel-neutral.
package telegramadapter

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/aura/aura/internal/chat"
	tele "gopkg.in/telebot.v4"
)

// ChannelDataKeyContext is the ChannelData key under which the inbound
// adapter stashes the tele.Context so the outbound adapter can build the
// reply (Bot, Recipient, Chat) without re-deriving them from the raw
// payload.
const ChannelDataKeyContext = "tele_context"

// Inbound implements chat.InboundAdapter for Telegram. The raw payload
// is expected to be a tele.Context (the value produced by telebot's update
// handler). Normalize extracts user / chat / text / locale and stashes the
// original tele.Context under ChannelData so the outbound side can drive
// progressive edits and final delivery.
type Inbound struct{}

var _ chat.InboundAdapter = Inbound{}

// New returns a freshly constructed Inbound adapter. No configuration is
// required today; the constructor exists so future wiring can pass in a
// logger or principal-resolver without breaking callers.
func New() *Inbound { return &Inbound{} }

// Channel reports the Telegram channel constant — used by Hub.RegisterInbound
// for routing.
func (Inbound) Channel() chat.Channel { return chat.ChannelTelegram }

// Normalize extracts a chat.InboundMessage from a tele.Context. Returns
// an error when the raw payload is not a tele.Context (defensive — the bot
// only ever hands real contexts to this adapter, but a future testing path
// might).
//
// ThreadID is derived from the Telegram chat ID. Telegram threads (forum
// topics) are not yet wired through; this can be extended to incorporate
// ThreadID from c.Message().ThreadID without breaking the contract.
//
// Attachments today are best-effort — Telegram document handling lives in
// the bot's documents.go pipeline and produces a source_id out of band.
// When the bot transitions to dispatching uploads through chat Hub the
// adapter will populate Attachments with the resolved AttachmentRef list.
func (a Inbound) Normalize(_ context.Context, raw any) (chat.InboundMessage, error) {
	c, ok := raw.(tele.Context)
	if !ok {
		return chat.InboundMessage{}, fmt.Errorf("telegramadapter: expected tele.Context, got %T", raw)
	}
	sender := c.Sender()
	tgChat := c.Chat()
	if sender == nil || tgChat == nil {
		return chat.InboundMessage{}, fmt.Errorf("telegramadapter: tele.Context missing sender or chat")
	}
	threadID := strconv.FormatInt(tgChat.ID, 10)
	msgID := ""
	if m := c.Message(); m != nil {
		msgID = strconv.Itoa(m.ID)
	}
	return chat.InboundMessage{
		ID:          msgID,
		Channel:     chat.ChannelTelegram,
		PrincipalID: strconv.FormatInt(sender.ID, 10),
		ThreadID:    threadID,
		Text:        c.Text(),
		Locale:      sender.LanguageCode,
		Mode:        chat.DeliveryModeStreaming,
		CreatedAt:   time.Now().UTC(),
		ChannelData: map[string]any{
			ChannelDataKeyContext: c,
		},
	}, nil
}
