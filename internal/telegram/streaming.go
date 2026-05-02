package telegram

import (
	"strings"
	"time"

	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

// sendAssistant delivers an LLM-generated message to the user with
// Markdown rendered to Telegram's HTML subset. Plain operator strings
// (auth errors, bootstrap messages) keep using c.Send directly so we
// don't double-escape them. If HTML send fails (e.g. malformed render),
// fall back to a plain c.Send so the user still sees the response.
func (b *Bot) sendAssistant(c tele.Context, text string) {
	rendered := renderForTelegram(text)
	if err := c.Send(rendered, tele.ModeHTML); err != nil {
		b.logger.Warn("HTML send failed, falling back to plain text", "error", err)
		_ = c.Send(text)
	}
}

// streamingMinThreshold is the buffered-content size at which we stop
// hiding and send the placeholder Telegram message. Below this the
// model may still be deciding whether to call tools (in which case
// any text would be a discardable preface), so we wait until we have
// enough text that progressive display is clearly worth it.
const streamingMinThreshold = 30

// streamingEditThrottle bounds how often we call Telegram's editMessage
// API. Telegram rate-limits edits to ~1/sec per chat; 800ms keeps us
// safely under the limit while still feeling responsive.
const streamingEditThrottle = 800 * time.Millisecond

// consumeStream reads tokens from ch and progressively edits a Telegram
// message as text accumulates. Returns an llm.Response shaped like the
// one Send would have produced plus a flag indicating whether a
// Telegram message has already been delivered for this iteration. When
// delivered=true, the caller should suppress c.Send to avoid double-
// posting. Slice 11s populates Token.Usage and Token.ToolCalls only on
// the final Done token, so we can build a complete Response here.
func (b *Bot) consumeStream(c tele.Context, ch <-chan llm.Token, userID string) (llm.Response, bool, error) {
	var sb strings.Builder
	var msg *tele.Message
	var lastEdit time.Time
	var resp llm.Response

	flush := func() {
		text := renderForTelegram(sb.String())
		if msg == nil {
			if sb.Len() < streamingMinThreshold {
				return
			}
			sent, err := c.Bot().Send(c.Recipient(), text, tele.ModeHTML)
			if err != nil {
				b.logger.Warn("streaming initial send failed", "user_id", userID, "error", err)
				return
			}
			msg = sent
			lastEdit = time.Now()
			return
		}
		if time.Since(lastEdit) < streamingEditThrottle {
			return
		}
		if _, err := c.Bot().Edit(msg, text, tele.ModeHTML); err != nil {
			// Rate limit or transient: skip this edit, the next one will retry.
			b.logger.Debug("streaming edit failed", "user_id", userID, "error", err)
			return
		}
		lastEdit = time.Now()
	}

	for tok := range ch {
		if tok.Err != nil {
			return llm.Response{}, msg != nil, tok.Err
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
			}
			// Final edit so the message reflects the complete text even
			// if the throttle skipped the last delta.
			if msg != nil && !resp.HasToolCalls {
				rendered := renderForTelegram(sb.String())
				if _, err := c.Bot().Edit(msg, rendered, tele.ModeHTML); err != nil {
					b.logger.Warn("streaming final edit failed", "user_id", userID, "error", err)
				}
			}
			break
		}
	}
	return resp, msg != nil && !resp.HasToolCalls, nil
}
