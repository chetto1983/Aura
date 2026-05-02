package telegram

import (
	"strconv"

	tele "gopkg.in/telebot.v4"
)

func (b *Bot) registerHandlers() {
	b.bot.Handle("/start", b.onStart)
	b.bot.Handle("/login", b.onLogin)
	b.bot.Handle("/token", b.onLogin)
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

	// Launch conversation in its own goroutine
	go b.handleConversation(c)

	return nil
}
