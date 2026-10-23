package main

import (
	"log/slog"
	"os"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

// serviceCredentials reads the shared cloud services' keys from aura.settings, then the
// environment, on every call: never from the chat profile, which a ChatGPT plan leaves keyless
// (docs/superpowers/specs/2026-10-23-cloud-service-credentials-design.md). Without a pool, or
// with a seal key the store refuses, it reads the environment alone and says so once.
func serviceCredentials(cfg *config.Config, pool *pgxpool.Pool) settings.Credentials {
	creds := settings.Credentials{LookupEnv: os.LookupEnv}
	if pool == nil {
		slog.Warn("cloud service credentials: no database, reading the environment only")
		return creds
	}
	store, err := settings.NewStore(pool, cfg.AuthulaSecret)
	if err != nil {
		slog.Warn("cloud service credentials: settings store unavailable, reading the environment only", "err", err)
		return creds
	}
	creds.Store = store
	return creds
}
