package main

import (
	"github.com/chetto1983/aura/internal/channels/telegram"
	"github.com/chetto1983/aura/internal/cron/handlers"
)

// buildTelegramTokenSweep gives the nightly retention job the Telegram onboarding tokens a
// setup abandoned before consuming. Each setup mints one row with a short expiry, and before
// this nothing deleted the ones nobody redeemed.
//
// The return type is the INTERFACE and a missing pool returns a bare nil, which the retention
// handler skips; a nil-pointer sweep inside a non-nil interface would be called.
func buildTelegramTokenSweep(chat *chatEnv) handlers.Sweeper {
	if chat.pool == nil {
		return nil
	}
	return telegram.New(chat.pool)
}
