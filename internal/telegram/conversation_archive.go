package telegram

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
)

type archiveTurnInput struct {
	ChatID       int64
	UserID       int64
	NextIndex    int64
	UserText     string
	LoopMessages []llm.Message
	Stats        agent.TurnStats
	ElapsedMS    int64
	TokensIn     int
}

func (b *Bot) archiveAppenderForTurn() conversation.TurnAppender {
	if b == nil || b.rt == nil {
		return nil
	}
	return b.rt.archiver
}

func archiveConversationTurns(ctx context.Context, logger *slog.Logger, archiver conversation.TurnAppender, input archiveTurnInput) {
	if archiver == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	nextIdx := input.NextIndex
	appendTurn := func(turn conversation.Turn) {
		if err := archiver.Append(ctx, turn); err != nil {
			logger.Error("archive: append failed",
				"chat_id", turn.ChatID,
				"turn_index", turn.TurnIndex,
				"role", turn.Role,
				"error", err)
		}
	}

	appendTurn(conversation.Turn{
		ChatID:    input.ChatID,
		UserID:    input.UserID,
		TurnIndex: nextIdx,
		Role:      "user",
		Content:   input.UserText,
	})
	nextIdx++

	for i, msg := range input.LoopMessages {
		turn := conversation.Turn{
			ChatID:     input.ChatID,
			UserID:     input.UserID,
			TurnIndex:  nextIdx,
			Role:       msg.Role,
			Content:    msg.Content,
			ToolCallID: msg.ToolCallID,
		}
		if len(msg.ToolCalls) > 0 {
			if raw, err := json.Marshal(msg.ToolCalls); err == nil {
				turn.ToolCalls = string(raw)
			} else {
				logger.Warn("archive: tool_calls marshal failed",
					"chat_id", input.ChatID, "turn_index", nextIdx, "error", err)
			}
		}
		if msg.Role == "assistant" && i == len(input.LoopMessages)-1 {
			turn.LLMCalls = input.Stats.LLMCalls
			turn.ToolCallsCount = input.Stats.ToolCalls
			turn.ElapsedMS = input.ElapsedMS
			turn.TokensIn = input.TokensIn
		}
		appendTurn(turn)
		nextIdx++
	}
}
