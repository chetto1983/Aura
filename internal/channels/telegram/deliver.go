package telegram

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/channels"
)

// compile-time proof that *Telegram satisfies the optional channels.Deliverer
// capability — the Registry runtime-asserts it to route identity-keyed pushes
// (Phase 20 R3) without any registry change.
var _ channels.Deliverer = (*Telegram)(nil)

// accountResolver is the narrow identity→account seam Deliver reads. *Store
// satisfies it (GetAccountByIdentity); declaring it consumer-side keeps Deliver
// unit-testable with a stub and free of a DB. A non-UUID/missing identity surfaces
// as a wrapped pgx.ErrNoRows so Deliver can treat both as "not my user".
type accountResolver interface {
	GetAccountByIdentity(ctx context.Context, identityID string) (Account, error)
}

// Deliver pushes text to the 1:1 Telegram chat owned by identityID, satisfying
// channels.Deliverer (Phase 20 R3). It honors the tri-state contract:
//
//	(false, nil) = not my user (no account / 'local' / no bot) → caller tries next
//	(true,  nil) = delivered
//	(false, err) = owns-but-failed (resolve or send error) → caller stops, no siblings
//
// The live bot is read under t.mu (it may be nil after Stop — Pitfall 4: never
// deref a racing nil bot); a nil bot or nil Store means the channel cannot push,
// so it returns (false, nil) and lets the route fall-back handle delivery.
func (t *Telegram) Deliver(ctx context.Context, identityID, text string) (bool, error) {
	t.mu.Lock()
	sender := t.deliverSender()
	t.mu.Unlock()
	if sender == nil {
		return false, nil
	}

	resolver := t.accountResolver()
	if resolver == nil {
		return false, nil
	}
	acct, err := resolver.GetAccountByIdentity(ctx, identityID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil // not my user → try next channel
		}
		return false, fmt.Errorf("telegram deliver: resolve identity %s: %w", identityID, err)
	}
	if _, err := sender.Send(tele.ChatID(acct.TelegramUserID), text); err != nil {
		return false, fmt.Errorf("telegram deliver: send to %d: %w", acct.TelegramUserID, err)
	}
	return true, nil
}

// deliverSender returns the botSender Deliver pushes through. It MUST be called
// under t.mu. The test override (deliverBot) wins so a recording double is injected
// without a live API; otherwise the live *tele.Bot (nil before Start / after Stop)
// is used. A nil result means the channel cannot push.
func (t *Telegram) deliverSender() botSender {
	if t.deliverBot != nil {
		return t.deliverBot
	}
	if t.bot == nil {
		return nil
	}
	return t.bot
}

// accountResolver resolves the identity→account seam: the test override wins, else
// the live *Store (nil when onboarding is unwired).
func (t *Telegram) accountResolver() accountResolver {
	if t.deliverResolver != nil {
		return t.deliverResolver
	}
	if t.deps.Store == nil {
		return nil
	}
	return t.deps.Store
}
