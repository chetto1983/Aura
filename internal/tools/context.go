package tools

import "context"

// userIDKey is the unexported context key used to thread the calling
// Telegram user's ID into tool invocations. Tools that need to know who
// asked for the action (e.g. schedule_task setting the reminder
// recipient) read it via UserIDFromContext; tools that don't simply
// ignore it.
type userIDKey struct{}

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
