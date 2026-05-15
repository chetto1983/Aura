package telegram

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"

	tele "gopkg.in/telebot.v4"
)

// Streaming constants mirror channels/telegram/outbound.go so both paths
// produce identical flush decisions.
const (
	streamingMinThreshold          = 30
	streamingReasoningMinThreshold = 8
	streamingEditThrottle          = 600 * time.Millisecond
)

// composeStreamingMessage builds the live-edited Telegram message body.
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

// sendAssistant delivers LLM-generated text to the user using Telegram entities.
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

// LogPlaceholderDeleteFailure is exported for channels/telegram.InvocationBuilder.
func LogPlaceholderDeleteFailure(logger *slog.Logger, userID string, placeholder *tele.Message, err error) {
	if err == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}
	args := []any{"user_id", userID, "error", err}
	if placeholder != nil {
		args = append(args, "message_id", placeholder.ID)
	}
	logger.Debug("telegram cleanup: placeholder delete failed", args...)
}

// telegramHubChatClient implements agent.ChatClient for the Hub path.
// It calls llm.Stream and drives progressive Telegram edits in real time.
type telegramHubChatClient struct {
	b           *Bot
	teleCtx     tele.Context
	userID      string
	placeholder *tele.Message
}

func (c *telegramHubChatClient) Chat(ctx context.Context, messages []llm.Message, toolDefs []llm.ToolDefinition) (agent.ChatResponse, error) {
	req := llm.Request{
		Messages:        messages,
		Model:           c.b.cfg.LLMModel,
		Tools:           toolDefs,
		ReasoningEffort: c.b.cfg.ReasoningEffort,
	}
	ch, err := c.b.rt.llm.Stream(ctx, req)
	if err != nil {
		c.b.logger.Error("LLM stream failed", "user_id", c.userID, "error", err)
		return agent.ChatResponse{Response: llm.Response{Content: "Sorry, I couldn't process your message. Please try again."}}, err
	}
	resp, delivered, err := c.streamTokens(ch)
	if err != nil {
		c.b.logger.Error("LLM stream read failed", "user_id", c.userID, "error", err)
		return agent.ChatResponse{Response: llm.Response{Content: "Sorry, I couldn't process your message. Please try again."}}, err
	}
	return agent.ChatResponse{Response: resp, Delivered: delivered}, nil
}

// streamTokens reads tokens from ch and progressively edits a Telegram message.
// Mirrors the legacy bot streaming path with identical flush thresholds.
func (c *telegramHubChatClient) streamTokens(ch <-chan llm.Token) (llm.Response, bool, error) {
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
		parts := renderForTelegramEntities(raw)
		if len(parts) == 0 {
			return
		}
		if msg == nil {
			if c.placeholder != nil {
				if edited, err := c.b.editAssistantMessage(c.teleCtx.Bot(), c.placeholder, parts[0], raw); err != nil {
					c.b.logger.Debug("placeholder edit failed, falling back to new message", "user_id", c.userID, "error", err)
					sent, sendErr := c.teleCtx.Bot().Send(c.teleCtx.Recipient(), parts[0].Text, parts[0].Entities)
					if sendErr != nil {
						return
					}
					msg = sent
				} else {
					msg = edited
				}
			} else {
				sent, err := c.teleCtx.Bot().Send(c.teleCtx.Recipient(), parts[0].Text, parts[0].Entities)
				if err != nil {
					c.b.logger.Warn("streaming initial send failed", "user_id", c.userID, "error", err)
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
		if _, err := c.b.editAssistantMessage(c.teleCtx.Bot(), msg, parts[0], raw); err != nil {
			c.b.logger.Debug("streaming edit failed", "user_id", c.userID, "error", err)
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
			if msg != nil && !resp.HasToolCalls {
				raw := strings.TrimSpace(sb.String())
				if raw == "" {
					break
				}
				parts := renderForTelegramEntities(raw)
				if len(parts) == 0 {
					break
				}
				if _, err := c.b.editAssistantMessage(c.teleCtx.Bot(), msg, parts[0], raw); err != nil {
					c.b.logger.Warn("streaming final edit failed", "user_id", c.userID, "error", err)
				}
				c.b.sendAssistantRemainder(c.teleCtx.Bot(), c.teleCtx.Recipient(), parts, 1)
			}
			break
		}
	}
	return resp, msg != nil && !resp.HasToolCalls, nil
}

// NewHubChatClient creates a telegramHubChatClient (agent.ChatClient) for use by
// internal/channels/telegram.InvocationBuilder, which cannot access the private type directly.
func NewHubChatClient(b *Bot, teleCtx tele.Context, userID string, placeholder *tele.Message) agent.ChatClient {
	return &telegramHubChatClient{b: b, teleCtx: teleCtx, userID: userID, placeholder: placeholder}
}

// SendAssistantText is the exported surface of sendAssistant.
func (b *Bot) SendAssistantText(c tele.Context, text string) { b.sendAssistant(c, text) }

// EditAssistantMsg is the exported surface of editAssistantMessage.
func (b *Bot) EditAssistantMsg(bot tele.API, msg tele.Editable, part RenderedMessage, rawText string) (*tele.Message, error) {
	return b.editAssistantMessage(bot, msg, part, rawText)
}

// SendAssistantMsgRemainder is the exported surface of sendAssistantRemainder.
func (b *Bot) SendAssistantMsgRemainder(bot tele.API, recipient tele.Recipient, parts []RenderedMessage, start int) {
	b.sendAssistantRemainder(bot, recipient, parts, start)
}

// toolActivityMessage returns the generic activity placeholder shown while a
// tool turn is in flight. The tool name is intentionally not included in the
// output (privacy: argument values must never appear in user-visible strings).
func toolActivityMessage(_ string) string {
	return "Sto lavorando alla richiesta..."
}
