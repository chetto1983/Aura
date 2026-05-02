package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v4"
)

// onStart handles the /start command. Three branches:
//
//  1. user already allowlisted → mint a fresh dashboard token (welcome back).
//  2. user unknown AND system has zero allowlisted users → first-run TOFU
//     bootstrap; this user claims the install. This is the only path that
//     auto-grants access, and it's bounded to "before any owner exists" —
//     otherwise the dashboard would have nobody to log in and approve future
//     requests.
//  3. user unknown AND someone is already allowlisted → enqueue the request
//     into pending_users, notify every allowlisted user via Telegram, and
//     reply that approval is required. The dashboard owner approves or
//     denies via /api/pending-users/{id}/{approve,deny}.
func (b *Bot) onStart(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	username := c.Sender().Username

	if b.isAllowlisted(userID) {
		return b.sendLoginToken(c, userID, "Welcome back to Aura.")
	}

	claimed, err := b.tryBootstrapUser(userID)
	if err != nil {
		b.logger.Error("bootstrap allowlist failed", "user_id", userID, "error", err)
		return c.Send("Aura could not complete first-time setup. Check the app logs and try /start again.")
	}
	if claimed {
		b.logger.Info("first-run telegram user bootstrapped",
			"user_id", userID,
			"username", username,
		)
		return b.sendLoginToken(c, userID, "Welcome to Aura. You claimed this first-run install.")
	}

	if b.authDB == nil {
		b.logger.Warn("start from non-allowlisted user (no auth store)",
			"user_id", userID,
			"username", username,
		)
		return c.Send("Aura is private. Ask the owner to add your Telegram user ID to TELEGRAM_ALLOWLIST: " + userID)
	}
	fresh, err := b.authDB.RequestAccess(context.Background(), userID, username)
	if err != nil {
		b.logger.Error("pending request enqueue failed", "user_id", userID, "error", err)
		return c.Send("Aura could not record your access request. Try /start again in a moment.")
	}
	if fresh {
		b.logger.Info("pending access request recorded",
			"user_id", userID,
			"username", username,
		)
		b.notifyOwnersOfPendingRequest(userID, username)
	} else {
		b.logger.Info("repeat pending access request",
			"user_id", userID,
			"username", username,
		)
	}
	return c.Send("Your access request is pending approval. The owner has been notified — you'll get a message here once you're approved.")
}

// notifyOwnersOfPendingRequest fans out a Telegram message to every
// allowlisted user (env allowlist + persisted bootstrap/approved set)
// telling them a stranger just hit /start. Best-effort — a delivery
// failure to one owner shouldn't block the rest. Only triggered for
// fresh requests so a user spamming /start can't pingstorm the owner.
func (b *Bot) notifyOwnersOfPendingRequest(userID, username string) {
	owners := b.collectOwnerIDs()
	if len(owners) == 0 {
		return
	}
	display := username
	if display == "" {
		display = "(no username)"
	}
	body := fmt.Sprintf(
		"New Aura access request:\n\n• User: @%s\n• Telegram ID: %s\n\nReview in the dashboard under Pending requests.",
		display, userID,
	)
	for _, owner := range owners {
		if owner == userID {
			continue
		}
		if err := b.SendToUser(owner, body); err != nil {
			b.logger.Warn("pending request notification failed",
				"owner_id", owner, "requester_id", userID, "error", err)
		}
	}
}

// collectOwnerIDs returns the union of env-configured allowlist IDs and
// persisted bootstrap/approved IDs, deduplicated so a user that lives in
// both places is not notified twice.
func (b *Bot) collectOwnerIDs() []string {
	seen := make(map[string]struct{})
	var out []string
	if b.cfg != nil {
		for _, id := range b.cfg.Allowlist {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	if b.authDB != nil {
		ids, err := b.authDB.AllowedUserIDs(context.Background())
		if err != nil {
			b.logger.Warn("collect owner ids: db lookup failed", "error", err)
		}
		for _, id := range ids {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

// ApproveAccess satisfies api.PendingApprover. It approves the pending
// row, mints a fresh dashboard token, and ships it to the requester over
// Telegram so the plaintext never round-trips through the dashboard.
// Returns auth.ErrInvalid when no open pending request exists for userID.
func (b *Bot) ApproveAccess(ctx context.Context, userID string) error {
	if b.authDB == nil {
		return fmt.Errorf("auth store unavailable")
	}
	if err := b.authDB.Approve(ctx, userID); err != nil {
		return err
	}
	token, err := b.authDB.Issue(ctx, userID)
	if err != nil {
		return fmt.Errorf("issue token: %w", err)
	}
	body := "Aura access approved.\n\nDashboard token (paste into the Aura web login):\n\n" +
		token + "\n\nKeep this private. You can ask /login anytime for a fresh token."
	if err := b.SendToUser(userID, body); err != nil {
		b.logger.Warn("approval notification failed", "user_id", userID, "error", err)
	}
	return nil
}

// DenyAccess satisfies api.PendingApprover. It rejects the pending row
// and sends the requester a courtesy Telegram note. Failure to deliver
// the note is logged but doesn't fail the deny.
func (b *Bot) DenyAccess(ctx context.Context, userID string) error {
	if b.authDB == nil {
		return fmt.Errorf("auth store unavailable")
	}
	if err := b.authDB.Deny(ctx, userID); err != nil {
		return err
	}
	if err := b.SendToUser(userID, "Your Aura access request was declined."); err != nil {
		b.logger.Warn("deny notification failed", "user_id", userID, "error", err)
	}
	return nil
}

func (b *Bot) onLogin(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	if !b.isAllowlisted(userID) {
		b.logger.Warn("login token requested by non-allowlisted user",
			"user_id", userID,
			"username", c.Sender().Username,
		)
		return c.Send("Aura is private. Ask the owner to add your Telegram user ID to TELEGRAM_ALLOWLIST: " + userID)
	}
	return b.sendLoginToken(c, userID, "Here is a fresh dashboard token.")
}

func (b *Bot) tryBootstrapUser(userID string) (bool, error) {
	if b.cfg.AllowlistConfigured || b.authDB == nil {
		return false, nil
	}
	return b.authDB.BootstrapUser(context.Background(), userID)
}

func (b *Bot) isAllowlisted(userID string) bool {
	if b.cfg != nil && b.cfg.IsAllowlisted(userID) {
		return true
	}
	if b.cfg == nil || b.cfg.AllowlistConfigured || b.authDB == nil {
		return false
	}
	ok, err := b.authDB.IsUserAllowed(context.Background(), userID)
	if err != nil {
		b.logger.Warn("bootstrap allowlist lookup failed", "user_id", userID, "error", err)
		return false
	}
	return ok
}

func (b *Bot) sendLoginToken(c tele.Context, userID, prefix string) error {
	if b.authDB == nil {
		return c.Send(prefix + "\n\nDashboard auth is not available in this run.")
	}
	token, err := b.authDB.Issue(context.Background(), userID)
	if err != nil {
		b.logger.Error("dashboard token issue failed", "user_id", userID, "error", err)
		return c.Send("I could not create a dashboard token. Check the app logs and try /login again.")
	}
	body := prefix + "\n\nDashboard token (paste into the Aura web login):\n\n" +
		token + "\n\nKeep this private. You can ask /login anytime for a fresh token."
	return c.Send(body)
}
