package telegram

import (
	"fmt"
	"strconv"
	"strings"

	tele "gopkg.in/telebot.v4"
)

const maxCommandToolNames = 80

func botCommands() []tele.Command {
	return []tele.Command{
		{Text: "start", Description: "Avvia Aura"},
		{Text: "status", Description: "Stato runtime"},
		{Text: "help", Description: "Comandi disponibili"},
		{Text: "clear", Description: "Cancella la conversazione"},
		{Text: "tools", Description: "Mostra strumenti visibili"},
		{Text: "login", Description: "Apri dashboard"},
	}
}

func (b *Bot) installBotCommands() {
	if b == nil || b.bot == nil {
		return
	}
	if err := b.bot.SetCommands(botCommands()); err != nil {
		if b.logger != nil {
			b.logger.Warn("telegram command menu setup failed", "error", err)
		}
	}
}

func (b *Bot) onClear(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	if !b.isAllowlisted(userID) {
		return nil
	}
	b.sessionStore().Clear(userID)
	return c.Send("Conversazione cancellata. Riparto pulito dal prossimo messaggio.")
}

func (b *Bot) onHelp(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	if !b.isAllowlisted(userID) {
		return nil
	}
	return c.Send(strings.Join([]string{
		"Comandi Aura:",
		"/status - stato runtime",
		"/tools - strumenti visibili",
		"/clear - cancella la conversazione",
		"/login - token dashboard",
		"/start - accesso e onboarding",
	}, "\n"))
}

func (b *Bot) onTools(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	if !b.isAllowlisted(userID) {
		return nil
	}
	var names []string
	if reg := b.ToolRegistry(); reg != nil {
		names = reg.Names()
	}
	if len(names) == 0 {
		return c.Send("Strumenti visibili ora: nessuno.")
	}
	visible := names
	suffix := ""
	if len(visible) > maxCommandToolNames {
		visible = visible[:maxCommandToolNames]
		suffix = fmt.Sprintf("\n... altri %d", len(names)-len(visible))
	}
	return c.Send(fmt.Sprintf("Strumenti visibili ora (%d):\n%s%s", len(names), strings.Join(visible, "\n"), suffix))
}
