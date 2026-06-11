package telegram

import (
	"context"
	"log/slog"

	tele "gopkg.in/telebot.v4"
)

func (t *Telegram) onStatusCancelCallback() tele.HandlerFunc {
	return func(c tele.Context) error {
		cb := c.Callback()
		if cb == nil || cb.Message == nil || cb.Message.Chat == nil {
			return nil
		}
		if !t.requireLinkedCallback(context.Background(), c, cb) {
			return nil
		}
		reply := "Nessun turno in corso da annullare."
		if t.cmds != nil {
			reply = t.cmds.cancel(cb.Message.Chat.ID)
		}
		_ = c.Respond(&tele.CallbackResponse{Text: reply})
		t.disarmCallbackKeyboard(c.Bot(), cb.Message)
		return nil
	}
}

func (t *Telegram) onSearchCallback() tele.HandlerFunc {
	return func(c tele.Context) error {
		cb := c.Callback()
		if cb == nil || cb.Message == nil || cb.Message.Chat == nil {
			return nil
		}
		if !t.requireLinkedCallback(context.Background(), c, cb) {
			return nil
		}
		page, closePager, ok := parseSearchCallback(cb.Data)
		_ = c.Respond(&tele.CallbackResponse{Text: "Ricevuto"})
		if !ok {
			return nil
		}
		if closePager {
			if deleter, ok := c.Bot().(botDeleter); ok {
				_ = deleter.Delete(cb.Message)
			}
			return nil
		}
		out := t.cmds.searchPage(cb.Message.Chat.ID, page)
		if out.text == "" {
			return nil
		}
		opts := []any{}
		if out.markup != nil {
			opts = append(opts, &tele.SendOptions{ReplyMarkup: out.markup})
		}
		if _, err := c.Bot().Edit(cb.Message, out.text, opts...); err != nil {
			slog.Warn("telegram search: page edit failed", "err", err)
		}
		return nil
	}
}

func (t *Telegram) onProfileCallback(daemonCtx context.Context) tele.HandlerFunc {
	return func(c tele.Context) error {
		cb := c.Callback()
		if cb == nil || cb.Message == nil || cb.Message.Chat == nil {
			return nil
		}
		if !t.requireLinkedCallback(daemonCtx, c, cb) {
			return nil
		}
		chatID := cb.Message.Chat.ID
		out, handled := t.profileForDispatch().handleCallback(daemonCtx, chatID, cb.Data)
		if !handled {
			_ = c.Respond(&tele.CallbackResponse{Text: "Non disponibile"})
			return nil
		}
		ack := out.ack
		if ack == "" {
			ack = "Ricevuto"
		}
		_ = c.Respond(&tele.CallbackResponse{Text: ack})
		t.disarmCallbackKeyboard(c.Bot(), cb.Message)
		t.replyProfile(c, out)
		return nil
	}
}
