package telegram

import (
	"context"
	"strconv"

	"github.com/aura/aura/internal/chat"
	"github.com/aura/aura/internal/concurrency"
	tele "gopkg.in/telebot.v4"
)

func (b *Bot) registerHandlers() {
	b.bot.Handle("/start", b.onStart)
	b.bot.Handle("/login", b.onLogin)
	b.bot.Handle("/token", b.onLogin)
	b.bot.Handle("/help", b.onHelp)
	b.bot.Handle("/clear", b.onClear)
	b.bot.Handle("/reset", b.onClear)
	b.bot.Handle("/tools", b.onTools)
	b.bot.Handle(tele.OnText, b.onMessage)
	b.bot.Handle("/status", b.onStatus)
	if b.docs != nil {
		b.bot.Handle(tele.OnDocument, b.docs.onDocument)
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
			if _, err := b.hub.Receive(context.Background(), chat.ChannelTelegram, c); err != nil {
				b.logger.Error("hub receive failed", "user_id", userID, "error", err)
			}
		}()
		return nil
	}

	entry := concurrency.Entry{
		Process: func(_ context.Context) {
			if b.hub == nil {
				b.logger.Error("hub not initialized, dropping message", "user_id", userID)
				return
			}
			if _, err := b.hub.Receive(context.Background(), chat.ChannelTelegram, c); err != nil {
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
	return nil
}
