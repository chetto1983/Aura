package telegram

import (
	"strings"
	"time"

	"github.com/aura/aura/internal/llm"
	tele "gopkg.in/telebot.v4"
)

// sendAssistant delivers LLM-generated Markdown using Telegram message
// entities (telegramify-markdown-go renders markdown → entity offsets).
// If entity delivery fails (rare: usually transient network or rate-limit),
// we fall back to plain text so the user still sees something. The old
// regex-based HTML fallback was retired with telegramify-markdown-go.
func (b *Bot) sendAssistant(c tele.Context, text string) {
	if err := sendTelegramEntityMessages(c.Bot(), c.Recipient(), text); err != nil {
		b.logger.Warn("entity send failed, falling back to plain text", "error", err)
		if err := c.Send(text); err != nil {
			b.logger.Warn("plain-text send failed", "error", err)
		}
	}
}

func (b *Bot) sendAssistantToRecipient(bot tele.API, recipient tele.Recipient, text string) error {
	if err := sendTelegramEntityMessages(bot, recipient, text); err != nil {
		b.logger.Warn("entity send failed, falling back to plain text", "error", err)
		_, plainErr := bot.Send(recipient, text)
		return plainErr
	}
	return nil
}

func (b *Bot) editAssistantMessage(bot tele.API, msg tele.Editable, part renderedTelegramMessage, rawText string) (*tele.Message, error) {
	edited, err := editTelegramEntityMessage(bot, msg, part)
	if err == nil {
		return edited, nil
	}
	b.logger.Warn("entity edit failed, falling back to plain text", "error", err)
	edited, err = bot.Edit(msg, rawText)
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

// streamingReasoningMinThreshold is the lower bar that triggers the
// first edit when ONLY reasoning text has arrived (no Content yet).
// Reasoning is the model's chain-of-thought — surfacing it early hides
// the "thinking pause" that would otherwise make Aura feel slow on
// reasoning-capable models (DeepSeek V4 Flash, OpenAI o-series).
const streamingReasoningMinThreshold = 8

// streamingEditThrottle bounds how often we call Telegram's editMessage
// API. Telegram rate-limits edits to ~1/sec per chat; 600ms keeps us
// safely under the limit while feeling more responsive.
const streamingEditThrottle = 600 * time.Millisecond

// composeStreamingMessage builds the live-edited Telegram message body
// from the current reasoning + content buffers. The reasoning chain is
// rendered as italic prose with a 🧠 prefix above a blank-line separator
// so it sits visually distinct from the actual answer. When reasoning is
// empty the prefix is omitted entirely; when content is empty (still
// thinking) the user sees only the live CoT.
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

// consumeStream reads tokens from ch and progressively edits a Telegram
// message as text accumulates. Returns an llm.Response shaped like the
// one Send would have produced plus a flag indicating whether a
// Telegram message has already been delivered for this iteration. When
// delivered=true, the caller should suppress c.Send to avoid double-
// posting. Slice 11s populates Token.Usage and Token.ToolCalls only on
// the final Done token, so we can build a complete Response here.
func (b *Bot) consumeStream(c tele.Context, ch <-chan llm.Token, userID string, placeholder *tele.Message) (llm.Response, bool, error) {
	var sb strings.Builder       // final answer content
	var cotBuf strings.Builder   // chain-of-thought reasoning
	var msg *tele.Message
	var lastEdit time.Time
	var resp llm.Response

	// readyToFlush returns true once we have either enough content to commit
	// to a non-discardable answer OR enough reasoning text to show the user
	// that the model is actively thinking instead of stuck. The two
	// thresholds differ because content might still be a preface to a tool
	// call (so wait longer) while reasoning is always safe to surface.
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
		parts := renderForTelegramEntities(raw)
		if len(parts) == 0 {
			return
		}
		if msg == nil {
			if placeholder != nil {
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
			b.logger.Debug("streaming edit failed", "user_id", userID, "error", err)
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
			// Final edit: drop the CoT prefix and show ONLY the answer.
			// The reasoning was scaffolding to mask latency — once the
			// final content is ready the user wants a clean message, not
			// a transcript of the model's thinking. Reasoning is still
			// preserved on resp.Reasoning for the archive / dashboard.
			if msg != nil && !resp.HasToolCalls {
				raw := strings.TrimSpace(sb.String())
				if raw == "" {
					break
				}
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
