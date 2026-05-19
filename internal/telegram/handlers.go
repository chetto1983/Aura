package telegram

import (
	"context"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/concurrency"
	"github.com/aura/aura/internal/identity"
	tele "gopkg.in/telebot.v4"
)

// AskUserCallbackUnique is the Telegram inline-button callback endpoint used
// by the channel adapter when rendering ask_user options.
const AskUserCallbackUnique = "aura_ask_user"

func (b *Bot) registerHandlers() {
	b.bot.Handle("/start", b.onStart)
	b.bot.Handle("/login", b.onLogin)
	b.bot.Handle("/token", b.onLogin)
	b.bot.Handle("/help", b.onHelp)
	b.bot.Handle("/clear", b.onClear)
	b.bot.Handle("/reset", b.onClear)
	b.bot.Handle("/tools", b.onTools)
	b.bot.Handle("\f"+AskUserCallbackUnique, b.onAskUserCallback)
	b.bot.Handle(tele.OnText, b.onMessage)
	b.bot.Handle("/status", b.onStatus)
	if b.docs != nil {
		b.bot.Handle(tele.OnDocument, b.docs.onDocument)
	}
	if b.voice != nil {
		b.bot.Handle(tele.OnVoice, b.voice.onVoiceMessage)
	}
}

func (b *Bot) onMessage(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)

	if !b.isAllowlisted(userID) {
		b.logger.Warn("message from non-allowlisted user",
			"user_id", userID,
			"username", c.Sender().Username,
		)
		return nil
	}

	b.routeTelegramContext(c, userID)
	return nil
}

func (b *Bot) onAskUserCallback(c tele.Context) error {
	sender := c.Sender()
	if sender == nil {
		return nil
	}
	userID := strconv.FormatInt(sender.ID, 10)
	if !b.isAllowlisted(userID) {
		b.logger.Warn("ask_user callback from non-allowlisted user",
			"user_id", userID,
			"username", sender.Username,
		)
		_ = c.Respond()
		return nil
	}

	if _, ok := askUserCallbackReplyText(c.Data()); !ok {
		_ = c.RespondAlert("Invalid choice")
		return nil
	}
	tgChat := c.Chat()
	if b.hub == nil || tgChat == nil {
		_ = c.RespondAlert("Question is no longer pending")
		return nil
	}
	threadID := strconv.FormatInt(tgChat.ID, 10)
	hasPending := false
	if _, durablePending, err := b.hub.PendingQuestion(b.telegramHubContext(context.Background(), userID), threadID, chat.ChannelTelegram); err != nil {
		b.logger.Warn("ask_user callback pending lookup failed", "user_id", userID, "error", err)
	} else if durablePending {
		hasPending = true
	}
	if status, ok := b.hub.ThreadRunStatus(threadID); ok && status == chat.RunStatusWaitingForUser {
		hasPending = true
	}
	if !hasPending {
		_ = c.RespondAlert("Question is no longer pending")
		return nil
	}
	if err := c.RespondText("Choice received"); err != nil {
		b.logger.Warn("ask_user callback acknowledgement failed", "user_id", userID, "error", err)
	}
	b.routeTelegramContext(c, userID)
	return nil
}

func askUserCallbackReplyText(data string) (string, bool) {
	data = strings.TrimSpace(data)
	n, err := strconv.Atoi(data)
	if err != nil || n < 1 {
		return "", false
	}
	return strconv.Itoa(n), true
}

func (b *Bot) routeTelegramContext(c tele.Context, userID string) {
	// Route through UserGate for per-user serialization (CONC-01, D-15).
	// Acquire is called from onMessage's goroutine (telebot handler), NOT from
	// the actor goroutine, preventing re-entrant deadlock (Pitfall 1).
	// The Process closure runs inside the per-user actor goroutine.
	gate := b.userGate()
	if gate == nil {
		// Fallback: UserGate not configured (tests, edge case); use direct goroutine.
		go func() {
			if b.hub == nil {
				b.logger.Error("hub not initialized, dropping message", "user_id", userID)
				return
			}
			if _, err := b.hub.Receive(b.telegramHubContext(context.Background(), userID), chat.ChannelTelegram, c); err != nil {
				b.logger.Error("hub receive failed", "user_id", userID, "error", err)
			}
		}()
		return
	}

	entry := concurrency.Entry{
		Process: func(ctx context.Context) {
			if b.hub == nil {
				b.logger.Error("hub not initialized, dropping message", "user_id", userID)
				return
			}
			if _, err := b.hub.Receive(b.telegramHubContext(ctx, userID), chat.ChannelTelegram, c); err != nil {
				b.logger.Error("hub receive failed", "user_id", userID, "error", err)
			}
		},
	}

	// Acquire blocks until the entry is enqueued in the per-user actor's inbox.
	// On inbox overflow, drops oldest entry and calls OnOverflow (D-03).
	// The actor goroutine processes entries one at a time (D-01 serialization).
	if err := gate.Acquire(context.Background(), userID, entry); err != nil {
		b.logger.Warn("failed to acquire user gate",
			"user_id", userID,
			"error", err,
		)
	}
}

func (b *Bot) telegramHubContext(ctx context.Context, userID string) context.Context {
	actorID := identity.TelegramSessionActorID(userID)
	ctx = identity.WithActorID(ctx, actorID)
	if b != nil && b.rt != nil && b.rt.authDB != nil {
		ctx = identity.WithAuthority(ctx, b.rt.authDB)
	}
	return ctx
}
