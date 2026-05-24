package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/aura/aura/internal/conversation"
)

func handleConversationList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var (
			chatID    int64
			hasChatID bool
		)
		if chatIDStr := r.URL.Query().Get("chat_id"); chatIDStr != "" {
			parsed, err := strconv.ParseInt(chatIDStr, 10, 64)
			if err != nil {
				writeError(w, deps.Logger, http.StatusBadRequest, "chat_id must be an integer")
				return
			}
			chatID = parsed
			hasChatID = true
		}

		limit := 50
		if lStr := r.URL.Query().Get("limit"); lStr != "" {
			if l, err := strconv.Atoi(lStr); err == nil && l > 0 {
				limit = l
			}
		}

		if deps.Archive == nil {
			writeJSON(w, deps.Logger, http.StatusOK, []ConversationTurn{})
			return
		}

		var (
			turns []conversation.Turn
			err   error
		)
		if hasChatID {
			turns, err = deps.Archive.ListByChat(r.Context(), chatID, limit)
		} else {
			turns, err = deps.Archive.ListAll(r.Context(), limit)
		}
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list conversations")
			return
		}

		out := make([]ConversationTurn, len(turns))
		for i, t := range turns {
			out[i] = turnToDTO(t)
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func handleConversationDetail(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathParamInt64(w, deps.Logger, r, "id")
		if !ok {
			return
		}

		if deps.Archive == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "conversation not found")
			return
		}

		t, err := deps.Archive.Get(r.Context(), id)
		if err != nil {
			if code, msg := errorStatus(err); code != 0 {
				writeError(w, deps.Logger, code, msg)
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to get conversation")
			return
		}

		writeJSON(w, deps.Logger, http.StatusOK, turnToDetailDTO(t))
	}
}

func handleConversationCompactions(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := pathParamInt64(w, deps.Logger, r, "id")
		if !ok {
			return
		}
		if deps.Archive == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "conversation not found")
			return
		}
		turn, err := deps.Archive.Get(r.Context(), id)
		if err != nil {
			if code, msg := errorStatus(err); code != 0 {
				writeError(w, deps.Logger, code, msg)
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to get conversation")
			return
		}
		if deps.Compactions == nil {
			writeJSON(w, deps.Logger, http.StatusOK, []ConversationCompaction{})
			return
		}
		events, err := deps.Compactions.ListCompactionsForTurn(r.Context(), turn.ChatID, turn.TurnIndex)
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list compactions")
			return
		}
		out := make([]ConversationCompaction, len(events))
		for i, event := range events {
			out[i] = compactionToDTO(id, event)
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func turnToDTO(t conversation.Turn) ConversationTurn {
	return ConversationTurn{
		ID:             t.ID,
		ChatID:         t.ChatID,
		UserID:         t.UserID,
		TurnIndex:      t.TurnIndex,
		Role:           t.Role,
		Content:        t.Content,
		ToolCalls:      t.ToolCalls,
		ToolCallID:     t.ToolCallID,
		LLMCalls:       t.LLMCalls,
		ToolCallsCount: t.ToolCallsCount,
		ElapsedMS:      t.ElapsedMS,
		TokensIn:       t.TokensIn,
		TokensOut:      t.TokensOut,
		CreatedAt:      t.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func compactionToDTO(conversationID int64, event conversation.CompactionEvent) ConversationCompaction {
	return ConversationCompaction{
		ID:               event.ID,
		ConversationID:   conversationID,
		RunID:            event.RunID,
		ChatID:           event.ChatID,
		TurnIndex:        event.TurnIndex,
		Iteration:        event.Iteration,
		MessagesBefore:   event.MessagesBefore,
		MessagesAfter:    event.MessagesAfter,
		TokensBefore:     event.TokensBefore,
		TokensAfter:      event.TokensAfter,
		ThresholdTokens:  event.ThresholdTokens,
		CumulativeTokens: event.CumulativeTokens,
		FocusPreview:     event.FocusPreview,
		ElapsedMS:        event.ElapsedMS,
		CreatedAt:        event.CreatedAt.UTC().Format(time.RFC3339),
	}
}

func turnToDetailDTO(t conversation.Turn) ConversationDetail {
	return ConversationDetail(turnToDTO(t))
}
