package telegram

import (
	tele "gopkg.in/telebot.v4"
)

func toolArgumentsForTool(name string, args map[string]any, chatID int64) map[string]any {
	if name != "search_memory" {
		return args
	}
	out := make(map[string]any, len(args)+1)
	for k, v := range args {
		out[k] = v
	}
	if chatID > 0 {
		out["chat_id"] = float64(chatID)
	}
	return out
}

func isTerminalTool(name string) bool {
	return name == "execute_code" || name == "execute_shell" || isFileGenerationTool(name)
}

func chatIDFromTeleContext(c tele.Context) int64 {
	if c == nil || c.Chat() == nil {
		return 0
	}
	return c.Chat().ID
}
