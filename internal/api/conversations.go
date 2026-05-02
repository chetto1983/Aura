package api

import (
	"database/sql"
	"errors"
	"fmt"
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
		idStr := r.PathValue("id")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil {
			writeError(w, deps.Logger, http.StatusBadRequest, fmt.Sprintf("invalid id %q", idStr))
			return
		}

		if deps.Archive == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "conversation not found")
			return
		}

		t, err := deps.Archive.Get(r.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, deps.Logger, http.StatusNotFound, "conversation not found")
				return
			}
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to get conversation")
			return
		}

		writeJSON(w, deps.Logger, http.StatusOK, turnToDetailDTO(t))
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

func turnToDetailDTO(t conversation.Turn) ConversationDetail {
	return ConversationDetail{
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
