package tools

import (
	"context"
	"errors"
	"fmt"

	"github.com/aura/aura/internal/auth"
)

// TokenSender delivers a freshly-minted dashboard token to the user out
// of band. The bot satisfies it by sending a Telegram message to the
// caller's chat. Returning the token through the LLM tool result would
// land it in conversation history and logs — splitting the channel keeps
// the plaintext off that path.
type TokenSender interface {
	SendToUser(userID, message string) error
}

// AllowlistFunc is satisfied by config.IsAllowlisted; declared here so
// the tool package doesn't import internal/config.
type AllowlistFunc func(userID string) bool

// RequestDashboardTokenTool mints a new bearer token for the calling
// Telegram user and delivers it via Telegram. The LLM never sees the
// token text; the tool's return value is a bookkeeping confirmation.
type RequestDashboardTokenTool struct {
	store     *auth.Store
	sender    TokenSender
	allowlist AllowlistFunc
}

// NewRequestDashboardTokenTool builds the tool. All three deps are
// required — the constructor returns nil if any is missing so the bot
// can skip registration when auth isn't configured.
func NewRequestDashboardTokenTool(store *auth.Store, sender TokenSender, allowlist AllowlistFunc) *RequestDashboardTokenTool {
	if store == nil || sender == nil || allowlist == nil {
		return nil
	}
	return &RequestDashboardTokenTool{store: store, sender: sender, allowlist: allowlist}
}

func (t *RequestDashboardTokenTool) Name() string { return "request_dashboard_token" }

func (t *RequestDashboardTokenTool) Description() string {
	return "Mint a fresh bearer token for this user's dashboard session and send it to them via Telegram. Use when the user asks for dashboard access, login link, or token. The token is delivered out-of-band — never echo it in your reply."
}

func (t *RequestDashboardTokenTool) Parameters() map[string]any {
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
}

func (t *RequestDashboardTokenTool) Execute(ctx context.Context, args map[string]any) (string, error) {
	userID := UserIDFromContext(ctx)
	if userID == "" {
		return "", errors.New("request_dashboard_token: no user context")
	}
	if !t.allowlist(userID) {
		// Unreachable in practice — only allowlisted users reach the tool
		// loop — but we double-check so a future plumbing change can't
		// silently issue tokens to non-allowlisted users.
		return "", errors.New("request_dashboard_token: user not allowlisted")
	}
	token, err := t.store.Issue(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("request_dashboard_token: issue: %w", err)
	}
	// Format chosen so the user can long-press / copy on mobile without
	// grabbing surrounding prose. Newlines around the token isolate it.
	body := "Dashboard token (paste into the Aura web login):\n\n" + token + "\n\nKeep this private. Use the dashboard's logout button to revoke it."
	if err := t.sender.SendToUser(userID, body); err != nil {
		// We've already minted the token; couldn't deliver. The token is
		// still valid and discoverable in the DB, but for safety we
		// revoke it so the partial state doesn't leave a usable bearer
		// floating around.
		_ = t.store.Revoke(ctx, token)
		return "", fmt.Errorf("request_dashboard_token: deliver: %w", err)
	}
	return "Sent a fresh dashboard token to your Telegram chat. Paste it into the web login.", nil
}
