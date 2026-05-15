package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/aura/aura/internal/identity"
)

// userIDKey is the context key the middleware uses to stash the resolved
// Telegram user ID. Handlers that need to know who's calling read it via
// UserIDFromContext. The key is unexported and the value type is private
// so no other package can spoof it.
type userIDKey struct{}
type actorIDKey struct{}

// UserIDFromContext returns the user ID resolved by RequireBearer, or "" if
// the request didn't go through the middleware.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(userIDKey{}).(string); ok {
		return v
	}
	return ""
}

// ActorIDFromContext returns the identity actor resolved by RequireBearer.
func ActorIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(actorIDKey{}).(string); ok {
		return v
	}
	return ""
}

// AllowlistFunc is the predicate the middleware uses to confirm a user
// is still allowed to use the dashboard. Tied to the same source of
// truth as the Telegram bot (config.IsAllowlisted) so revoking from one
// channel revokes from both.
type AllowlistFunc func(userID string) bool

// RequireBearer returns a middleware that enforces a valid `Authorization:
// Bearer <token>` header on every wrapped request. On success the
// resolved user ID is stashed in the request context. On failure the
// handler emits a JSON 401. Expired tokens get a distinct body so the
// dashboard can explain re-login; missing, wrong, revoked, and
// de-allowlisted tokens remain generic so an attacker can't enumerate
// token state.
func RequireBearer(store TokenReader, allowlist AllowlistFunc, logger *slog.Logger, next http.Handler) http.Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := extractBearer(r.Header.Get("Authorization"))
		if token == "" {
			writeUnauthorized(w)
			return
		}
		userID, err := store.Lookup(r.Context(), token)
		if err != nil {
			var auditErr *AuditUpdateError
			if errors.As(err, &auditErr) {
				logger.Warn("auth: token audit update failed", "user_id", auditErr.UserID, "error", auditErr.Err)
				err = nil
			}
		}
		if err != nil {
			// Don't log the token (or its prefix) — a leak in logs
			// defeats the whole point. Just record that auth failed.
			if errors.Is(err, ErrExpired) {
				logger.Debug("auth: token expired", "remote_addr", r.RemoteAddr)
				writeTokenExpired(w)
				return
			}
			logger.Debug("auth: lookup failed", "remote_addr", r.RemoteAddr)
			writeUnauthorized(w)
			return
		}
		if allowlist != nil && !allowlist(userID) {
			logger.Warn("auth: user no longer allowlisted", "user_id", userID)
			writeUnauthorized(w)
			return
		}
		ctx := context.WithValue(r.Context(), userIDKey{}, userID)
		actorID := identity.TelegramSessionActorID(userID)
		ctx = context.WithValue(ctx, actorIDKey{}, actorID)
		ctx = identity.WithActorID(ctx, actorID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractBearer parses "Bearer <token>" from an Authorization header.
// Case-insensitive on the scheme; tolerates a single space.
func extractBearer(header string) string {
	if header == "" {
		return ""
	}
	const prefix = "Bearer "
	if len(header) <= len(prefix) {
		return ""
	}
	if !strings.EqualFold(header[:len(prefix)], prefix) {
		return ""
	}
	return strings.TrimSpace(header[len(prefix):])
}

// writeUnauthorized emits the canonical 401 JSON body so the frontend
// can branch on status alone.
func writeUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"unauthorized"}`))
}

func writeTokenExpired(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"token_expired"}`))
}

// TokenFromRequest reads the bearer token off the request header, when
// present. Used by the logout handler so it can revoke the exact token
// the client used.
func TokenFromRequest(r *http.Request) string {
	return extractBearer(r.Header.Get("Authorization"))
}
