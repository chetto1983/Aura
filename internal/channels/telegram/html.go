package telegram

import tgmd2html "github.com/PaulSonOfLars/gotg_md2html"

// RenderTelegramHTML converts the model's Markdown-ish answer to the subset of
// HTML accepted by the Telegram Bot API. The converter escapes raw HTML before
// adding Telegram-safe entity tags, so user/model text like "<script>" remains
// text, not markup.
func RenderTelegramHTML(s string) string {
	return tgmd2html.MD2HTMLV2(s)
}
