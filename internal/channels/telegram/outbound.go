// Package telegramadapter wraps the chat.InboundAdapter / chat.OutboundAdapter
// pair for the Telegram channel.  Inbound normalises tele.Update payloads into
// chat.InboundMessage; Outbound drives progressive Telegram edits from a live
// token stream.
package telegramadapter

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/llm"
	tgtelegram "github.com/aura/aura/internal/telegram"
	tele "gopkg.in/telebot.v4"
)

// streaming constants – mirror the values in internal/telegram/streaming.go
// so the ported logic behaves identically.
const (
	// streamingMinThreshold is the content-buffer size at which we commit to
	// showing an in-progress message.  Below this, content might still be a
	// tool-call preface, so we wait.
	streamingMinThreshold = 30
	// streamingReasoningMinThreshold is the lower bar for showing live CoT
	// when no content has arrived yet – makes the model feel responsive.
	streamingReasoningMinThreshold = 8
	// streamingEditThrottle bounds edit-API calls to ~1/sec per chat, keeping
	// us safely under Telegram's rate limit while feeling live.
	streamingEditThrottle = 600 * time.Millisecond
)

// Outbound implements chat.OutboundAdapter for the Telegram streaming channel.
// It drives progressive Telegram message edits via ConsumeStream (token-channel
// path used by the legacy bot and the US-301 fixture).  The event-based
// Deliver path is wired in US-704.
type Outbound struct {
	logger *slog.Logger
}

// NewOutbound constructs an Outbound adapter.  A nil logger falls back to
// slog.Default().
func NewOutbound(logger *slog.Logger) *Outbound {
	if logger == nil {
		logger = slog.Default()
	}
	return &Outbound{logger: logger}
}

// Channel + Mode satisfy chat.OutboundAdapter.
func (*Outbound) Channel() chat.Channel   { return chat.ChannelTelegram }
func (*Outbound) Mode() chat.DeliveryMode { return chat.DeliveryModeStreaming }

// Deliver is the event-based outbound path.  The Telegram streaming path uses
// ConsumeStream (token channel), so Deliver is a no-op until US-704 wires the
// tele.Context lookup per RunID.
func (*Outbound) Deliver(_ context.Context, _ chat.OutboundEvent) error { return nil }

// ConsumeStream reads tokens from ch and progressively edits a Telegram message
// as text accumulates.  It is a faithful port of
// internal/telegram.Bot.consumeStream; the logic is byte-identical so that
// US-301 fixture snapshots remain valid.
//
// Returns an llm.Response shaped like the one llm.Client.Send would have
// produced, plus a delivered flag (true when a Telegram message was sent and
// the final content was delivered without a trailing tool call).  Callers
// suppress their own c.Send when delivered == true.
func (o *Outbound) ConsumeStream(
	c tele.Context,
	ch <-chan llm.Token,
	userID string,
	placeholder *tele.Message,
) (llm.Response, bool, error) {
	var sb strings.Builder
	var cotBuf strings.Builder
	var msg *tele.Message
	var lastEdit time.Time
	var resp llm.Response

	readyToFlush := func() bool {
		if sb.Len() >= streamingMinThreshold {
			return true
		}
		if sb.Len() == 0 && cotBuf.Len() >= streamingReasoningMinThreshold {
			return true
		}
		return false
	}

	flush := func() {
		if !readyToFlush() {
			return
		}
		raw := composeStreamingMessage(cotBuf.String(), sb.String())
		parts := tgtelegram.RenderForEntities(raw)
		if len(parts) == 0 {
			return
		}
		if msg == nil {
			if placeholder != nil {
				if edited, err := o.editMessage(c.Bot(), placeholder, parts[0], raw); err != nil {
					o.logger.Debug("placeholder edit failed, falling back to new message",
						"user_id", userID, "error", err)
					sent, sendErr := c.Bot().Send(c.Recipient(), parts[0].Text, parts[0].Entities)
					if sendErr != nil {
						return
					}
					msg = sent
				} else {
					msg = edited
				}
			} else {
				sent, err := c.Bot().Send(c.Recipient(), parts[0].Text, parts[0].Entities)
				if err != nil {
					o.logger.Warn("streaming initial send failed", "user_id", userID, "error", err)
					return
				}
				msg = sent
			}
			lastEdit = time.Now()
			return
		}
		if time.Since(lastEdit) < streamingEditThrottle {
			return
		}
		if _, err := o.editMessage(c.Bot(), msg, parts[0], raw); err != nil {
			o.logger.Debug("streaming edit failed", "user_id", userID, "error", err)
			return
		}
		lastEdit = time.Now()
	}

	for tok := range ch {
		if tok.Err != nil {
			return llm.Response{}, msg != nil, tok.Err
		}
		if tok.Reasoning != "" {
			cotBuf.WriteString(tok.Reasoning)
			flush()
		}
		if tok.Content != "" {
			sb.WriteString(tok.Content)
			flush()
		}
		if tok.Done {
			resp = llm.Response{
				Content:      sb.String(),
				HasToolCalls: len(tok.ToolCalls) > 0,
				ToolCalls:    tok.ToolCalls,
				Usage:        tok.Usage,
				Reasoning:    strings.TrimSpace(cotBuf.String()),
			}
			// Final edit: drop the CoT prefix so the user sees a clean answer.
			if msg != nil && !resp.HasToolCalls {
				raw := strings.TrimSpace(sb.String())
				if raw == "" {
					break
				}
				parts := tgtelegram.RenderForEntities(raw)
				if len(parts) == 0 {
					break
				}
				if _, err := o.editMessage(c.Bot(), msg, parts[0], raw); err != nil {
					o.logger.Warn("streaming final edit failed", "user_id", userID, "error", err)
				}
				o.sendRemainder(c.Bot(), c.Recipient(), parts, 1)
			}
			break
		}
	}
	return resp, msg != nil && !resp.HasToolCalls, nil
}

// editMessage calls bot.Edit with entities, falling back to plain text on error.
func (o *Outbound) editMessage(
	bot tele.API,
	msg tele.Editable,
	part tgtelegram.RenderedMessage,
	rawText string,
) (*tele.Message, error) {
	if bot == nil {
		return nil, errors.New("telegramadapter: bot is nil")
	}
	edited, err := bot.Edit(msg, part.Text, part.Entities)
	if err == nil {
		return edited, nil
	}
	o.logger.Warn("entity edit failed, falling back to plain text", "error", err)
	edited, err = bot.Edit(msg, rawText)
	if err != nil {
		return nil, err
	}
	return edited, nil
}

// sendRemainder sends parts[start:] as separate messages (overflow pages).
func (o *Outbound) sendRemainder(bot tele.API, recipient tele.Recipient, parts []tgtelegram.RenderedMessage, start int) {
	for _, part := range parts[start:] {
		if _, err := bot.Send(recipient, part.Text, part.Entities); err != nil {
			o.logger.Warn("streaming remainder send failed", "error", err)
			return
		}
	}
}

// composeStreamingMessage builds the live Telegram message body: CoT is
// rendered as italic prose with a 🧠 prefix; once content arrives the two
// sections are separated by a blank line.  When only content is present the
// prefix is omitted entirely.
func composeStreamingMessage(cot, content string) string {
	cot = strings.TrimSpace(cot)
	content = strings.TrimSpace(content)
	switch {
	case cot != "" && content != "":
		return "🧠 _" + cot + "_\n\n" + content
	case cot != "":
		return "🧠 _" + cot + "_"
	default:
		return content
	}
}
