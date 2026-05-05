package telegram

import (
	"strings"
	"time"

	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

// sendAssistant delivers LLM-generated Markdown using Telegram message
// entities. Plain operator strings (auth errors, bootstrap messages) keep
// using c.Send directly so token-like payloads are never transformed. If
// entity delivery fails, we fall back through the older HTML renderer and
// then plain text so the user still sees the response.
func (b *Bot) sendAssistant(c tele.Context, text string) {
	if err := sendTelegramEntityMessages(c.Bot(), c.Recipient(), text); err != nil {
		b.logger.Warn("entity send failed, falling back to HTML", "error", err)
		rendered := renderForTelegram(text)
		if err := c.Send(rendered, tele.ModeHTML); err != nil {
			b.logger.Warn("HTML send failed, falling back to plain text", "error", err)
			_ = c.Send(text)
		}
	}
}

func (b *Bot) sendAssistantToRecipient(bot tele.API, recipient tele.Recipient, text string) error {
	if err := sendTelegramEntityMessages(bot, recipient, text); err != nil {
		b.logger.Warn("entity send failed, falling back to HTML", "error", err)
		rendered := renderForTelegram(text)
		if _, htmlErr := bot.Send(recipient, rendered, tele.ModeHTML); htmlErr != nil {
			b.logger.Warn("HTML send failed, falling back to plain text", "error", htmlErr)
			_, plainErr := bot.Send(recipient, text)
			return plainErr
		}
	}
	return nil
}

func (b *Bot) editAssistantMessage(bot tele.API, msg tele.Editable, part renderedTelegramMessage, rawText string) (*tele.Message, error) {
	edited, err := editTelegramEntityMessage(bot, msg, part)
	if err == nil {
		return edited, nil
	}
	b.logger.Warn("entity edit failed, falling back to HTML", "error", err)
	rendered := renderForTelegram(rawText)
	edited, err = bot.Edit(msg, rendered, tele.ModeHTML)
	if err != nil {
		return nil, err
	}
	return edited, nil
}

func (b *Bot) sendAssistantRemainder(bot tele.API, recipient tele.Recipient, parts []renderedTelegramMessage, start int) {
	for _, part := range parts[start:] {
		if _, err := bot.Send(recipient, part.Text, part.Entities); err != nil {
			b.logger.Warn("streaming remainder send failed", "error", err)
			return
		}
	}
}

// streamingMinThreshold is the buffered-content size at which we stop
// hiding and send the placeholder Telegram message. Below this the
// model may still be deciding whether to call tools (in which case
// any text would be a discardable preface), so we wait until we have
// enough text that progressive display is clearly worth it.
const streamingMinThreshold = 30

// streamingEditThrottle bounds how often we call Telegram's editMessage
// API. Telegram rate-limits edits to ~1/sec per chat; 600ms keeps us
// safely under the limit while feeling more responsive.
const streamingEditThrottle = 600 * time.Millisecond

// consumeStream reads tokens from ch and progressively edits a Telegram
// message as text accumulates. Returns an llm.Response shaped like the
// one Send would have produced plus a flag indicating whether a
// Telegram message has already been delivered for this iteration. When
// delivered=true, the caller should suppress c.Send to avoid double-
// posting. Slice 11s populates Token.Usage and Token.ToolCalls only on
// the final Done token, so we can build a complete Response here.
func (b *Bot) consumeStream(c tele.Context, ch <-chan llm.Token, userID string, placeholder *tele.Message) (llm.Response, bool, error) {
	var sb strings.Builder
	var msg *tele.Message
	var lastEdit time.Time
	var resp llm.Response

	flush := func() {
		if sb.Len() < streamingMinThreshold {
			return
		}
		raw := sb.String()
		parts := renderForTelegramEntities(raw)
		if len(parts) == 0 {
			return
		}
		if msg == nil {
			if placeholder != nil {
				// Edit the pre-existing placeholder instead of sending a new message.
				if edited, err := b.editAssistantMessage(c.Bot(), placeholder, parts[0], raw); err != nil {
					b.logger.Debug("placeholder edit failed, falling back to new message", "user_id", userID, "error", err)
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
					b.logger.Warn("streaming initial send failed", "user_id", userID, "error", err)
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
		if _, err := b.editAssistantMessage(c.Bot(), msg, parts[0], raw); err != nil {
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
				raw := sb.String()
				parts := renderForTelegramEntities(raw)
				if len(parts) == 0 {
					break
				}
				if _, err := b.editAssistantMessage(c.Bot(), msg, parts[0], raw); err != nil {
					b.logger.Warn("streaming final edit failed", "user_id", userID, "error", err)
				}
				b.sendAssistantRemainder(c.Bot(), c.Recipient(), parts, 1)
			}
			break
		}
	}
	return resp, msg != nil && !resp.HasToolCalls, nil
}
