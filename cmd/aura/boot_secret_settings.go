// boot_secret_settings.go reads the secret aura.settings rows the daemon needs outside its LLM
// config. They never reach the process environment (settings.OverlayEnv skips them).
package main

import (
	"context"
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/settings"
)

// settingsSecret reads one secret row, or "" when the store cannot be built or read. That is
// logged, never fatal: the caller falls back to the environment.
func settingsSecret(ctx context.Context, chat *chatEnv, key string) string {
	if chat == nil || chat.pool == nil || chat.cfg == nil {
		return ""
	}
	store, err := settings.NewStore(chat.pool, chat.cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("settings secret unavailable", "key", key, "err", err)
		return ""
	}
	value, err := store.Secret(ctx, key)
	if err != nil {
		slog.Warn("settings secret unreadable", "key", key, "err", err)
		return ""
	}
	return strings.TrimSpace(value)
}

// effectiveTelegramToken is the saved token when there is one, else the environment's.
func effectiveTelegramToken(ctx context.Context, chat *chatEnv) string {
	if token := settingsSecret(ctx, chat, "TELEGRAM_BOT_TOKEN"); token != "" {
		return token
	}
	return strings.TrimSpace(telegram.LoadConfig().BotToken)
}
