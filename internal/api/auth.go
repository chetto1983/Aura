package api

import (
	"errors"
	"net/http"

	"github.com/aura/aura/internal/auth"
)

// handleAuthLogout revokes the bearer token used to make this request.
// The middleware has already validated the token before this runs, so
// we re-extract from the header and revoke. Idempotent — a second logout
// returns 200 (the user was already logged out, no need to error).
func handleAuthLogout(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Auth == nil {
			writeError(w, deps.Logger, http.StatusInternalServerError, "auth store unavailable")
			return
		}
		token := auth.TokenFromRequest(r)
		if token == "" {
			// Should never happen — middleware would have 401'd. Defensive.
			writeError(w, deps.Logger, http.StatusBadRequest, "no bearer token on request")
			return
		}
		if err := deps.Auth.Revoke(r.Context(), token); err != nil && !errors.Is(err, auth.ErrInvalid) {
			deps.Logger.Warn("api: logout revoke", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "revoke failed")
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"ok": true})
	}
}

// handleAuthWhoami returns the user ID resolved from the bearer token.
// Lets the dashboard show "logged in as <user>" without keeping a copy
// alongside the token in localStorage. Cheap — no DB hit beyond what
// RequireBearer already did.
func handleAuthWhoami(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, deps.Logger, http.StatusOK, map[string]any{
			"user_id": auth.UserIDFromContext(r.Context()),
		})
	}
}
