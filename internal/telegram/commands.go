package telegram

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/aura/aura/internal/audio"
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
		{Text: "voice", Description: "Modalità voce: on / tts / off"},
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
		"/voice - modalità voce (on/tts/off)",
		"/login - token dashboard",
		"/start - accesso e onboarding",
	}, "\n"))
}

// onVoice handles the /voice command.
// /voice on → ModeAll (voice + text), /voice tts → ModeVoiceOnly, /voice off → ModeOff.
// /voice with no argument shows the current mode and usage.
func (b *Bot) onVoice(c tele.Context) error {
	userID := strconv.FormatInt(c.Sender().ID, 10)
	if !b.isAllowlisted(userID) {
		return nil
	}
	chatID := strconv.FormatInt(c.Chat().ID, 10)
	store := b.voicePolicyStore()
	arg := strings.TrimSpace(c.Message().Payload)

	if arg == "" {
		mode, err := store.Get(context.Background(), chatID)
		if err != nil {
			b.logger.Warn("voice policy get failed", "chat_id", chatID, "error", err)
			mode = audio.ModeOff
		}
		return c.Send(voiceModeStatus(mode) + "\n\n" + voiceModeUsage())
	}

	var newMode audio.Mode
	switch arg {
	case "on":
		newMode = audio.ModeAll
	case "tts":
		newMode = audio.ModeVoiceOnly
	case "off":
		newMode = audio.ModeOff
	default:
		return c.Send(voiceModeUsage())
	}

	if err := store.Set(context.Background(), chatID, newMode); err != nil {
		b.logger.Warn("voice policy set failed", "chat_id", chatID, "error", err)
		return c.Send("Errore nell'aggiornamento della modalità voce.")
	}
	return c.Send("Modalità voce ora: " + voiceModeLabel(newMode))
}

// voicePolicyStore returns the voice policy store, falling back to NoopStore when nil.
func (b *Bot) voicePolicyStore() audio.Store {
	if b.rt != nil && b.rt.voicePolicy != nil {
		return b.rt.voicePolicy
	}
	return audio.NoopStore{}
}

func voiceModeLabel(m audio.Mode) string {
	switch m {
	case audio.ModeAll:
		return "voce + testo"
	case audio.ModeVoiceOnly:
		return "solo voce"
	default:
		return "off"
	}
}

func voiceModeStatus(m audio.Mode) string {
	return "Modalità voce attuale: " + voiceModeLabel(m)
}

func voiceModeUsage() string {
	return "Opzioni: /voice on (voce + testo) · /voice tts (solo voce) · /voice off (solo testo)"
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
