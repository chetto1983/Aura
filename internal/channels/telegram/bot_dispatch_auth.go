package telegram

import (
	"context"
	"log/slog"

	tele "gopkg.in/telebot.v4"
)

const (
	activationRequiredMsg   = "Questa istanza di Aura non e collegata al tuo account Telegram. Apri il link di attivazione dal pannello di setup e poi riprova."
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

func (t *Telegram) requireLinkedMessage(ctx context.Context, c tele.Context, msg *tele.Message) bool {
	if t.telegramUserIsLinked(ctx, telegramUserIDFromMessage(msg)) {
		return true
	}
	t.reply(c, activationRequiredMsg)
	return false
}

func (t *Telegram) requireLinkedCallback(ctx context.Context, c tele.Context, cb *tele.Callback) bool {
	if t.telegramUserIsLinked(ctx, telegramUserIDFromCallback(cb)) {
		return true
	}
	_ = c.Respond(&tele.CallbackResponse{Text: activationRequiredToast})
	return false
}

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
