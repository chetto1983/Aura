package api

import (
	"context"
	"net/http"
	"strconv"
	"time"
)

// ConversationsStatsResponse is GET /conversations/stats — used by the
// dashboard to show row count + retention stats next to the cleanup
// controls.
type ConversationsStatsResponse struct {
	TotalRows     int64  `json:"total_rows"`
	OldestAt      string `json:"oldest_at,omitempty"` // RFC3339
	NewestAt      string `json:"newest_at,omitempty"`
	DistinctChats int64  `json:"distinct_chats"`
}

// CleanupResponse is the response from any /conversations cleanup endpoint.
type CleanupResponse struct {
	OK      bool  `json:"ok"`
	Deleted int64 `json:"deleted"`
}

func handleConversationStats(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Archive == nil {
			writeJSON(w, deps.Logger, http.StatusOK, ConversationsStatsResponse{})
			return
		}
		stats, err := deps.Archive.Stats(r.Context())
		if err != nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}
		out := ConversationsStatsResponse{
			TotalRows:     stats.TotalRows,
			DistinctChats: stats.DistinctChats,
		}
		if !stats.OldestAt.IsZero() {
			out.OldestAt = stats.OldestAt.UTC().Format(time.RFC3339)
		}
		if !stats.NewestAt.IsZero() {
			out.NewestAt = stats.NewestAt.UTC().Format(time.RFC3339)
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

// handleConversationCleanup handles POST /conversations/cleanup with
// optional query params:
//   - chat_id: only delete that chat's history
//   - older_than_days: only delete turns older than N days
//   - all=true: nuke everything (must be explicit)
//
// Exactly one selector must be provided. Confirmation is the caller's
// job (the dashboard prompts before posting).
func handleConversationCleanup(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Archive == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "conversation archive not configured")
			return
		}
		q := r.URL.Query()
		chatIDStr := q.Get("chat_id")
		daysStr := q.Get("older_than_days")
		nukeAll := q.Get("all") == "true"

		populated := 0
		if chatIDStr != "" {
			populated++
		}
		if daysStr != "" {
			populated++
		}
		if nukeAll {
			populated++
		}
		if populated != 1 {
			writeError(w, deps.Logger, http.StatusBadRequest,
				"set exactly one of chat_id, older_than_days, or all=true")
			return
		}

		ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
		defer cancel()

		var deleted int64
		var err error
		switch {
		case chatIDStr != "":
			chatID, perr := strconv.ParseInt(chatIDStr, 10, 64)
			if perr != nil {
				writeError(w, deps.Logger, http.StatusBadRequest, "chat_id must be an integer")
				return
			}
			deleted, err = deps.Archive.DeleteByChat(ctx, chatID)
		case daysStr != "":
			days, perr := strconv.Atoi(daysStr)
			if perr != nil || days < 1 {
				writeError(w, deps.Logger, http.StatusBadRequest, "older_than_days must be a positive integer")
				return
			}
			cutoff := time.Now().UTC().AddDate(0, 0, -days)
			deleted, err = deps.Archive.DeleteOlderThan(ctx, cutoff)
		default: // all=true
			deleted, err = deps.Archive.DeleteAll(ctx)
		}
		if err != nil {
			deps.Logger.Warn("api: conversation cleanup", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "cleanup failed: "+err.Error())
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, CleanupResponse{OK: true, Deleted: deleted})
	}
}
