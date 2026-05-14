package tools

import (
	"context"
	"strings"
)

// userIDKey is the unexported context key used to thread the calling
// Telegram user's ID into tool invocations. Tools that need to know who
// asked for the action (e.g. schedule_task setting the reminder
// recipient) read it via UserIDFromContext; tools that don't simply
// ignore it.
type userIDKey struct{}
type allowedToolNamesKey struct{}

// WithUserID returns a context that carries the caller's Telegram user
// ID for downstream tool calls. Returns the input ctx unchanged when
// userID is empty so callers don't need a special case.
func WithUserID(ctx context.Context, userID string) context.Context {
	if userID == "" {
		return ctx
	}
	return context.WithValue(ctx, userIDKey{}, userID)
}

// UserIDFromContext reads the user ID stored by WithUserID. Returns
// empty string when not set — tools that need an ID should reject the
// call rather than guessing.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(userIDKey{}).(string); ok {
		return v
	}
	return ""
}

// WithAllowedToolNames returns a context carrying the exact tool surface made
// visible to the model in the current turn. Programmatic tool orchestration
// uses this as the final guard against guessed hidden tool names.
func WithAllowedToolNames(ctx context.Context, names []string) context.Context {
	cleaned := cleanStrings(names)
	if len(cleaned) == 0 {
		return ctx
	}
	return context.WithValue(ctx, allowedToolNamesKey{}, cleaned)
}

// AllowedToolNamesFromContext reads the current-turn model-visible tool names.
func AllowedToolNamesFromContext(ctx context.Context) []string {
	if v, ok := ctx.Value(allowedToolNamesKey{}).([]string); ok {
		return append([]string(nil), v...)
	}
	return nil
}

func toolNameInList(name string, names []string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	for _, value := range names {
		if value == name {
			return true
		}
	}
	return false
}
