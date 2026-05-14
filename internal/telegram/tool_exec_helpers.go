package telegram

import (
	tele "gopkg.in/telebot.v4"
)

func chatIDFromTeleContext(c tele.Context) int64 {
	if c == nil || c.Chat() == nil {
		return 0
	}
	return c.Chat().ID
}
