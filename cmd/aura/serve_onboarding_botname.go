package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

// botUsernameResolverTTL bounds how often the onboarding deep-link resolver hits the Telegram
// getMe endpoint. Short enough that a token saved via the Settings/onboarding Telegram step
// takes effect within seconds (no daemon restart — the sentinel/refresh pattern), long enough
// that a burst of mints inside one provision saga reuses a single getMe round-trip.
const botUsernameResolverTTL = 30 * time.Second

// newBotUsernameResolver returns the resolve-on-use bot-username function wired into the
// onboarding service. It reads the EFFECTIVE TELEGRAM_BOT_TOKEN (an aura.settings row wins
// over the boot env, mirroring settings_api.effectiveSettingValue) and getMe's it, caching the
// result per token for botUsernameResolverTTL. An empty or invalid token yields "" so
// provisioning stays unavailable rather than minting a dead deep-link. pool may be nil (no DB
// / interview-only) → the resolver falls back to the boot env token only.
func newBotUsernameResolver(pool *pgxpool.Pool) func(context.Context) string {
	var (
		mu         sync.Mutex
		cachedTok  string
		cachedName string
		cachedAt   time.Time
	)
	var store *settings.Store
	if pool != nil {
		store = settings.NewStore(pool)
	}
	effectiveToken := func(ctx context.Context) string {
		if store != nil {
			if rows, err := store.List(ctx); err == nil {
				for _, row := range rows {
					if row.Key == "TELEGRAM_BOT_TOKEN" {
						if v := strings.TrimSpace(row.Value); v != "" {
							return v
						}
					}
				}
			}
		}
		return strings.TrimSpace(telegram.LoadConfig().BotToken)
	}
	return func(ctx context.Context) string {
		tok := effectiveToken(ctx)
		if tok == "" {
			return ""
		}
		mu.Lock()
		defer mu.Unlock()
		if tok == cachedTok && time.Since(cachedAt) < botUsernameResolverTTL {
			return cachedName
		}
		name := resolveBotUsername(ctx, tok)
		cachedTok, cachedName, cachedAt = tok, name, time.Now()
		return name
	}
}
