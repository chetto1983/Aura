package conversation

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/aura/aura/internal/llm"
)

// ArchiveTurnInput carries all per-turn data needed to persist a conversation
// turn to the archive. It is channel-neutral: no Telegram types referenced.
type ArchiveTurnInput struct {
	ChatID       int64
	UserID       int64
	NextIndex    int64
	UserText     string
	LoopMessages []llm.Message
	LLMCalls     int
	ToolCalls    int
	ElapsedMS    int64
	TokensIn     int
}

// ArchiveConversationTurns persists one conversation turn (user message +
// subsequent loop messages) to archiver. It is a no-op when archiver is nil.
func ArchiveConversationTurns(ctx context.Context, logger *slog.Logger, archiver TurnAppender, input ArchiveTurnInput) {
	if archiver == nil {
		return
	}
	if logger == nil {
		logger = slog.Default()
	}

	nextIdx := input.NextIndex
	appendTurn := func(turn Turn) {
		if err := archiver.Append(ctx, turn); err != nil {
			logger.Error("archive: append failed",
				"chat_id", turn.ChatID,
				"turn_index", turn.TurnIndex,
				"role", turn.Role,
				"error", err)
		}
	}

	appendTurn(Turn{
		ChatID:    input.ChatID,
		UserID:    input.UserID,
		TurnIndex: nextIdx,
		Role:      "user",
		Content:   input.UserText,
	})
	nextIdx++

	for i, msg := range input.LoopMessages {
		turn := Turn{
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
			turn.LLMCalls = input.LLMCalls
			turn.ToolCallsCount = input.ToolCalls
			turn.ElapsedMS = input.ElapsedMS
			turn.TokensIn = input.TokensIn
		}
		appendTurn(turn)
		nextIdx++
	}
}
