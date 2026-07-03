package main

import (
	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

func wireSettingsProviders(server *agui.Server, pool *pgxpool.Pool) {
	server.SetSettingsStore(settings.NewStore(pool))
	server.SetTelegramBotProbe(telegramGetMeProbe)
}
