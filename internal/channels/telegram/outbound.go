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

// Outbound implements chat.OutboundAdapter for the Telegram channel.
// Progressive message streaming happens inside telegramHubChatClient.Chat
// (internal/telegram), so Deliver only needs to log operational events.
type Outbound struct {
	logger *slog.Logger
}

var _ chat.OutboundAdapter = (*Outbound)(nil)

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

// Deliver logs operational events. Streaming is handled inside
// telegramHubChatClient.Chat (internal/telegram), so this adapter only
// needs to surface errors and usage stats for operators.
func (o *Outbound) Deliver(_ context.Context, ev chat.OutboundEvent) error {
	switch ev.Type {
	case chat.EventError:
		errText, _ := ev.Payload["error"].(string)
		o.logger.Warn("telegram run error",
			"run_id", ev.RunID,
			"thread_id", ev.ThreadID,
			"error", errText,
		)
	case chat.EventUsage:
		args := []any{"run_id", ev.RunID}
		for _, k := range []string{"llm_calls", "tool_calls", "tokens_total", "cost_usd", "terminal_tool"} {
			if v, ok := ev.Payload[k]; ok {
				args = append(args, k, v)
			}
		}
		o.logger.Info("telegram run usage", args...)
	}
	return nil
}

// ConsumeStream reads tokens from ch and progressively edits a Telegram message
// as text accumulates. Used by the US-301 fixture for record-and-replay tests;
// the logic mirrors telegramHubChatClient.streamTokens so fixture snapshots
// remain valid.
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
// rawText is kept for the entity-rendering side (it's the markdown source that
// produced part), but the plain-text fallback uses part.Text — the already-split,
// length-bounded variant — so it never trips Telegram's 4096 UTF-16 cap.
// Live debug 2026-05-16 hit "MESSAGE_TOO_LONG (400)" when the fallback re-sent
// rawText for an oversize buffer; using part.Text closes that path.
func (o *Outbound) editMessage(
	bot tele.API,
	msg tele.Editable,
	part tgtelegram.RenderedMessage,
	rawText string,
) (*tele.Message, error) {
	if bot == nil {
		return nil, errors.New("telegramadapter: bot is nil")
	}
	_ = rawText // kept for API symmetry; future debug logging may surface the unsplit source
	edited, err := bot.Edit(msg, part.Text, part.Entities)
	if err == nil {
		return edited, nil
	}
	o.logger.Warn("entity edit failed, falling back to plain text",
		"error", err,
		"part_text_len", len(part.Text),
	)
	// Fallback uses part.Text (length-bounded by tgmd.WithMaxMessageLen at split
	// time), NOT rawText (which may exceed Telegram's 4096 UTF-16 message cap
	// and re-trigger MESSAGE_TOO_LONG).
	edited, err = bot.Edit(msg, part.Text)
	if err != nil {
		return nil, err
	}
	return edited, nil
}

// sendRemainder sends parts[start:] as separate messages (overflow pages).
// Each part is already length-bounded by tgtelegram.RenderForEntities (4096
// UTF-16 cap). When entity send fails (entity parse error, not size), retry
// the same length-bounded part as plain text — mirrors the fallback logic in
// editMessage so a single bad entity range doesn't drop user-visible content.
func (o *Outbound) sendRemainder(bot tele.API, recipient tele.Recipient, parts []tgtelegram.RenderedMessage, start int) {
	for i, part := range parts[start:] {
		_, err := bot.Send(recipient, part.Text, part.Entities)
		if err == nil {
			continue
		}
		o.logger.Warn("streaming remainder send failed, falling back to plain text",
			"error", err,
			"part_index", start+i,
			"part_text_len", len(part.Text),
		)
		if _, fbErr := bot.Send(recipient, part.Text); fbErr != nil {
			o.logger.Warn("streaming remainder plain-text fallback failed",
				"error", fbErr,
				"part_index", start+i,
			)
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
