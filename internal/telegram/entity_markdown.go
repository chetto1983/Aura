package telegram

import (
	"fmt"

	tgmd "github.com/eekstunt/telegramify-markdown-go"
	tele "gopkg.in/telebot.v4"
)

const telegramMaxMessageUTF16 = 4096

type renderedTelegramMessage struct {
	Text     string
	Entities tele.Entities
}

func renderForTelegramEntities(s string) []renderedTelegramMessage {
	if s == "" {
		return nil
	}
	msgs := tgmd.ConvertAndSplit(s,
		tgmd.WithHeadingSymbols([6]string{"", "", "", "", "", ""}),
		tgmd.WithBulletMarker("-"),
		tgmd.WithMaxMessageLen(telegramMaxMessageUTF16),
	)
	out := make([]renderedTelegramMessage, 0, len(msgs))
	for _, msg := range msgs {
		if msg.Text == "" {
			continue
		}
		out = append(out, renderedTelegramMessage{
			Text:     msg.Text,
			Entities: toTelebotEntities(msg.Entities),
		})
	}
	return out
}

func toTelebotEntities(entities []tgmd.Entity) tele.Entities {
	if len(entities) == 0 {
		return nil
	}
	out := make(tele.Entities, 0, len(entities))
	for _, e := range entities {
		out = append(out, tele.MessageEntity{
			Type:     tele.EntityType(e.Type),
			Offset:   e.Offset,
			Length:   e.Length,
			URL:      e.URL,
			Language: e.Language,
		})
	}
	return out
}

func sendTelegramEntityMessages(bot tele.API, recipient tele.Recipient, text string) error {
	if bot == nil {
		return fmt.Errorf("telegram bot is nil")
	}
	parts := renderForTelegramEntities(text)
	if len(parts) == 0 {
		return nil
	}
	for _, part := range parts {
		if _, err := bot.Send(recipient, part.Text, part.Entities); err != nil {
			return err
		}
	}
	return nil
}

func editTelegramEntityMessage(bot tele.API, msg tele.Editable, part renderedTelegramMessage) (*tele.Message, error) {
	if bot == nil {
		return nil, fmt.Errorf("telegram bot is nil")
	}
	return bot.Edit(msg, part.Text, part.Entities)
}
