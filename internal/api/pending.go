package api

import (
	"context"
	"errors"
	"net/http"
	"regexp"

	"github.com/aura/aura/internal/auth"
)

// PendingApprover is the boundary between the API and the Telegram bot.
// Approve is what closes the loop on a pending request: flip the row to
// approved, mint a token, and ship it to the requester over Telegram so
// the plaintext never round-trips through the dashboard. Deny mirrors
// the shape so the frontend can call both with the same request style.
//
// The bot wires the real implementation; tests inject a fake.
type PendingApprover interface {
	ApproveAccess(ctx context.Context, userID string) error
	DenyAccess(ctx context.Context, userID string) error
}

// telegramIDRe constrains the userID path segment so a malicious value
// can't sneak through into a database query or log line.
var telegramIDRe = regexp.MustCompile(`^[0-9]{1,32}$`)

// PendingUserSummary is one row of GET /pending-users.
type PendingUserSummary struct {
	UserID      string `json:"user_id"`
	Username    string `json:"username,omitempty"`
	RequestedAt string `json:"requested_at"`
}

func handlePendingList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Auth == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "auth store unavailable")
			return
		}
		rows, err := deps.Auth.ListPending(r.Context())
		if err != nil {
			deps.Logger.Warn("api: pending list", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list pending users")
			return
		}
		out := make([]PendingUserSummary, 0, len(rows))
		for _, p := range rows {
			out = append(out, PendingUserSummary{
				UserID:      p.UserID,
				Username:    p.Username,
				RequestedAt: p.RequestedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			})
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func handlePendingApprove(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.PendingApprover == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "approval pipeline unavailable")
			return
		}
		id := r.PathValue("id")
		if !telegramIDRe.MatchString(id) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid telegram id")
			return
		}
		if err := deps.PendingApprover.ApproveAccess(r.Context(), id); err != nil {
			if errors.Is(err, auth.ErrInvalid) {
				writeError(w, deps.Logger, http.StatusNotFound, "no pending request for that user")
				return
			}
			deps.Logger.Warn("api: pending approve", "user_id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "approve failed")
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"ok": true, "user_id": id})
	}
}

func handlePendingDeny(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.PendingApprover == nil {
			writeError(w, deps.Logger, http.StatusServiceUnavailable, "approval pipeline unavailable")
			return
		}
		id := r.PathValue("id")
		if !telegramIDRe.MatchString(id) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid telegram id")
			return
		}
		if err := deps.PendingApprover.DenyAccess(r.Context(), id); err != nil {
			if errors.Is(err, auth.ErrInvalid) {
				writeError(w, deps.Logger, http.StatusNotFound, "no pending request for that user")
				return
			}
			deps.Logger.Warn("api: pending deny", "user_id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "deny failed")
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"ok": true, "user_id": id})
	}
}
