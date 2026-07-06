package telegram

import (
	"context"
	"log/slog"

	tele "gopkg.in/telebot.v4"
)

const (
	// activationRequiredMsg is the reject-unlinked reply (D-24): an unknown chat-id
	// runs NO agent and is pointed to the WEB-initiated linking flow — the operator
	// opens Settings -> Telegram in the web cockpit (POST /api/settings/telegram/link),
	// which mints a one-time code, and sends it here with /start to bind this chat to
	// their identity. No long-lived token is ever pasted into a URL (MUSR-06): the code
	// rides the <=1h ?start= setup-bootstrap deep-link.
	activationRequiredMsg   = "Questa istanza di Aura non e collegata al tuo account Telegram. Per l'attivazione apri Impostazioni Telegram nel pannello web, genera un codice monouso e invialo qui con /start."
	activationRequiredToast = "Account Telegram non collegato"
)

type telegramSeenMarker interface {
	TouchLastSeen(ctx context.Context, telegramUserID int64) error
}

func (t *Telegram) profileForDispatch() *profileOnboarding {
	if t.profile != nil {
		return t.profile
	}
	p := newProfileOnboarding(t.deps.Profile, t.accountsForDispatch())
	p.extractor = t.deps.AnswerExtractor
	return p
}

func (t *Telegram) accountsForDispatch() profileAccountResolver {
	if t.deps.profileAccounts != nil {
		return t.deps.profileAccounts
	}
	if t.deps.Store != nil {
		return t.deps.Store
	}
	return nil
}

func telegramUserIDFromMessage(msg *tele.Message) int64 {
	if msg == nil {
		return 0
	}
	if msg.Sender != nil {
		return msg.Sender.ID
	}
	if msg.Chat != nil {
		return msg.Chat.ID
	}
	return 0
}

func telegramUserIDFromCallback(cb *tele.Callback) int64 {
	if cb == nil {
		return 0
	}
	if cb.Sender != nil {
		return cb.Sender.ID
	}
	if cb.Message != nil {
		return telegramUserIDFromMessage(cb.Message)
	}
	return 0
}

// requireLinkedMessage is the fail-closed inbound-message gate. It enforces the
// personal-DM appliance invariant FIRST — a non-private chat (group/supergroup/channel) is
// rejected up front (HI-03): with private-only, msg.Chat.ID == msg.Sender.ID, so the gate's
// sender-id key and startTurn's chat-id scope key are provably the same id. That closes the
// group-chat divergence where a linked sender passed the gate but the chat-id scope missed
// and the turn ran as the seeded local admin. It then requires a linked account.
func (t *Telegram) requireLinkedMessage(ctx context.Context, c tele.Context, msg *tele.Message) bool {
	if msg == nil || msg.Chat == nil || msg.Chat.Type != tele.ChatPrivate {
		t.reply(c, activationRequiredMsg)
		return false
	}
	if t.telegramUserIsLinked(ctx, telegramUserIDFromMessage(msg)) {
		return true
	}
	t.reply(c, activationRequiredMsg)
	return false
}

// requireLinkedCallback mirrors requireLinkedMessage for inline-button callbacks: a
// non-private chat is rejected with the activation toast BEFORE the linked-account check,
// so a callback originating outside a private DM never resolves or resumes a pause (HI-03).
func (t *Telegram) requireLinkedCallback(ctx context.Context, c tele.Context, cb *tele.Callback) bool {
	if cb == nil || cb.Message == nil || cb.Message.Chat == nil || cb.Message.Chat.Type != tele.ChatPrivate {
		_ = c.Respond(&tele.CallbackResponse{Text: activationRequiredToast})
		return false
	}
	if t.telegramUserIsLinked(ctx, telegramUserIDFromCallback(cb)) {
		return true
	}
	_ = c.Respond(&tele.CallbackResponse{Text: activationRequiredToast})
	return false
}

// telegramUserIsLinked is the D-24 fail-closed per-user gate: it returns true ONLY for
// a chat-id with a provisioned telegram_accounts row (GetAccountByTelegramID resolves →
// account.IdentityID). A missing resolver, an absent sender, or an unlinked chat-id all
// fail CLOSED (no agent run) — every dispatch entry point (onText/onVoice/onPhoto/
// onDocument/onReply/onCallback) calls this before spawning a turn, so an unknown or
// unprovisioned user never falls through to another identity's context or the local
// admin. A linked user's turn is then scoped to THEIR identity in startTurn
// (scopeTurnToIdentity, D-23).
func (t *Telegram) telegramUserIsLinked(ctx context.Context, telegramUserID int64) bool {
	accounts := t.accountsForDispatch()
	if accounts == nil {
		slog.Warn("telegram auth: no account resolver wired; rejecting inbound update", "telegram_user_id", telegramUserID)
		return false
	}
	if telegramUserID == 0 {
		slog.Warn("telegram auth: inbound update missing sender", "telegram_user_id", telegramUserID)
		return false
	}
	if _, err := accounts.GetAccountByTelegramID(ctx, telegramUserID); err != nil {
		slog.Info("telegram auth: rejected unlinked sender", "telegram_user_id", telegramUserID, "err", err)
		return false
	}
	if marker, ok := accounts.(telegramSeenMarker); ok {
		if err := marker.TouchLastSeen(ctx, telegramUserID); err != nil {
			slog.Warn("telegram auth: touch last_seen failed", "telegram_user_id", telegramUserID, "err", err)
		}
	}
	return true
}
