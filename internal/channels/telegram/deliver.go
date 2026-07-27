package telegram

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	tele "gopkg.in/telebot.v4"

	"github.com/chetto1983/aura/internal/channels"
)

// compile-time proof that *Telegram satisfies the optional channels.Deliverer
// capability — the Registry runtime-asserts it to route identity-keyed pushes
// (Phase 20 R3) without any registry change.
var _ channels.Deliverer = (*Telegram)(nil)

// compile-time proof that *Telegram ALSO satisfies the optional channels.ApprovalDeliverer
// capability — the scheduler approval-reminder sweep fans out through it to render a real
// on-channel HITL approval prompt (Amendment #92 revised).
var _ channels.ApprovalDeliverer = (*Telegram)(nil)

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
	// The Bot API rejects a message over 4096 runes outright, so an unsplit push of a
	// long agent_job summary was lost in full — the interactive path has always split
	// (renderer.go), the push path never did. Same splitter, same package: a second one
	// would drift. splitTelegramText returns nil only for the empty string, where the
	// single-send behaviour (and its API error) is preserved deliberately.
	chunks := splitTelegramText(text, telegramTextCap)
	if len(chunks) == 0 {
		chunks = []string{text}
	}
	for i, chunk := range chunks {
		if i > 0 {
			if err := t.paceDeliver(ctx, t.chatRateLimit()); err != nil {
				return false, fmt.Errorf("telegram deliver: pacing chunk %d/%d to %d: %w", i+1, len(chunks), acct.TelegramUserID, err)
			}
		}
		if _, err := sender.Send(tele.ChatID(acct.TelegramUserID), chunk); err != nil {
			// Stop at the first failure rather than pushing the remainder: the chat has
			// already received chunks 1..i, and continuing past a rate-limit rejection
			// only deepens the hole. The caller's retry re-sends the WHOLE text, so a
			// mid-sequence failure costs a duplicated prefix — accepted, because the
			// alternative (per-chunk delivery state) is a durable cursor this push path
			// has no room for.
			return false, fmt.Errorf("telegram deliver: send chunk %d/%d to %d: %w", i+1, len(chunks), acct.TelegramUserID, err)
		}
	}
	return true, nil
}

// paceDeliver waits between chunks so a long split message does not trip Telegram's
// per-chat flood limit (the renderer paces for the same reason). It is ctx-aware: a
// cancelled delivery stops mid-sequence instead of sleeping out the remaining chunks.
// deliverSleep is the unexported test seam — set, it replaces the wait entirely, so a
// multi-chunk test does not pay the real 1s-per-chunk rate limit.
func (t *Telegram) paceDeliver(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	if t.deliverSleep != nil {
		t.deliverSleep(d)
		return nil
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// DeliverApproval renders an ACTIONABLE scheduled-task approval prompt to the 1:1 Telegram chat
// owned by identityID, satisfying channels.ApprovalDeliverer (Amendment #92 revised): a bounded
// question (task kind + short id, never the payload) + an inline Sì/No keyboard bound to the
// pause token. It renders through the SAME approvalMarkup/callback endpoint as the in-turn relay
// (Phase A consolidation), so pressing a button resolves the REAL ask_user pause via the single
// onCallback → SubmitAnswer path — HITL parity with the model relay, not a bespoke approve. It
// honors the SAME tri-state contract as Deliver: (false,nil) not my user; (true,nil) delivered;
// (false,err) owns-but-failed.
func (t *Telegram) DeliverApproval(ctx context.Context, identityID, token, taskID, kind string) (bool, error) {
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
			return false, nil // not my user → try next channel (WebUI pulls the pause)
		}
		return false, fmt.Errorf("telegram deliver approval: resolve identity %s: %w", identityID, err)
	}
	text := approvalPromptText(taskID, kind)
	if _, err := sender.Send(tele.ChatID(acct.TelegramUserID), text, &tele.SendOptions{ReplyMarkup: approvalPushMarkup(token)}); err != nil {
		return false, fmt.Errorf("telegram deliver approval: send to %d: %w", acct.TelegramUserID, err)
	}
	return true, nil
}

// approvalPromptText is the bounded sweep-push copy — task kind + short id only, never the
// payload (no goal/secret leakage beyond what the origin already saw); the full request is one
// Dettagli tap away, read from the pause itself. Plain text on purpose: the send sets no
// ParseMode, so Markdown syntax would render literally. The in-turn relay renders the pause's own
// Question instead — this leg has only (taskID, kind) to work with.
func approvalPromptText(taskID, kind string) string {
	return "🔔 Approvazione richiesta\n" +
		"Task pianificato: " + kind + "\n" +
		"ID: " + shortTelegramTaskID(taskID) + "\n" +
		"Tocca Dettagli per la richiesta completa, poi Approva o Rifiuta."
}

// shortTelegramTaskID renders the first 8 chars of a task id for the bounded prompt: a raw
// 36-char UUID in operator-facing copy is noise, and the full id stays in resume_context for the
// machine leg (parity with the tool-side mask in internal/agent/tools/task.go).
func shortTelegramTaskID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
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
