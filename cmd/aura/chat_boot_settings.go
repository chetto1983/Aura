// chat_boot_settings.go is the aura.settings half of the boot: it opens the pool, overlays
// the allowlisted settings rows onto the environment and reloads the config they feed.
// Split out of chat_boot.go (refactor-on-touch, 600-LOC cap).
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

type bootSettingsOps struct {
	overlay func(context.Context, *pgxpool.Pool) error
	// secrets hands the loaded config the credentials aura.settings holds; nil in tests that
	// do not exercise it.
	secrets func(context.Context, *pgxpool.Pool, *config.Config) error
}

// resolveConfigAndPool loads the config and opens the DB pool, handing BOTH to
// migration gate and assembleChatEnv. It owns the pool across every post-open
// failure: if the settings overlay or the config reload fails it closes the pool
// before returning. On success it returns the OPEN pool and the caller takes over
// its lifecycle.
func resolveConfigAndPool(ctx context.Context, loadConfig func() (*config.Config, error), open dbOpener) (*config.Config, *pgxpool.Pool, error) {
	return resolveConfigAndPoolWithSettings(
		ctx,
		loadConfig,
		open,
		bootSettingsOps{
			overlay: overlayStoreSettings,
			secrets: applyStoreSecrets,
		},
	)
}

// overlayStoreSettings converts any secret row still stored in the clear, then overlays the
// allowlisted rows onto the environment for config.Load to read.
func overlayStoreSettings(ctx context.Context, pool *pgxpool.Pool) error {
	store, err := settings.NewStore(pool, os.Getenv("AURA_AUTHULA_SECRET"))
	if err != nil {
		return err
	}
	if _, err := store.EncryptPlaintextSecrets(ctx); err != nil {
		return err
	}
	return settings.OverlayEnv(ctx, store)
}

func resolveConfigAndPoolWithSettings(
	ctx context.Context,
	loadConfig func() (*config.Config, error),
	open dbOpener,
	settingsOps bootSettingsOps,
) (*config.Config, *pgxpool.Pool, error) {
	cfg, err := loadConfig()
	if err != nil {
		return nil, nil, err
	}
	// Fail fast on an empty required infra secret (O-04) BEFORE opening any connection,
	// so a misconfigured deploy errors at boot with a named cause instead of a late DB
	// auth failure or a silently degraded graph. This pre-open Validate is the
	// load-bearing half of the intentional double-Validate: it must run before open()
	// (assembleChatEnv re-checks the RELOADED config after the overlay).
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	pool, err := open(ctx, &cfg.DB)
	if err != nil {
		return nil, nil, fmt.Errorf("db open: %w", err)
	}
	overlayBootSettings(ctx, pool, settingsOps)
	cfg, err = loadConfig()
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	applyBootSecrets(ctx, pool, cfg, settingsOps)
	return cfg, pool, nil
}

func overlayBootSettings(
	ctx context.Context,
	pool *pgxpool.Pool,
	settingsOps bootSettingsOps,
) {
	if err := settingsOps.overlay(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, "warn: settings overlay:", err)
	}
}

func applyBootSecrets(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config, settingsOps bootSettingsOps) {
	if settingsOps.secrets == nil {
		return
	}
	if err := settingsOps.secrets(ctx, pool, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "warn: settings secrets:", err)
	}
}

// secretReader is settings.Store.Secret, narrowed so applySecretSettings is testable.
type secretReader interface {
	Secret(ctx context.Context, key string) (string, error)
}

func applyStoreSecrets(ctx context.Context, pool *pgxpool.Pool, cfg *config.Config) error {
	store, err := settings.NewStore(pool, cfg.AuthulaSecret)
	if err != nil {
		return err
	}
	return applySecretSettings(ctx, store, cfg)
}

// applySecretSettings puts the credentials aura.settings holds into the loaded config. They
// never pass through the environment (settings.OverlayEnv skips them), so this is how the
// daemon's LLM client, and every backend that reuses its key, and the management key see them.
// A stored row wins over the environment.
func applySecretSettings(ctx context.Context, secrets secretReader, cfg *config.Config) error {
	llmKey, err := secrets.Secret(ctx, "OPENROUTER_API_KEY")
	if err != nil {
		return err
	}
	if llmKey != "" {
		cfg.LLM.APIKey = llmKey
	}
	managementKey, err := secrets.Secret(ctx, "AURA_OPENROUTER_MANAGEMENT_KEY")
	if err != nil {
		return err
	}
	if managementKey != "" {
		cfg.OpenRouterManagementKey = managementKey
	}
	return nil
}

// openSettingsOverlayPool opens a pool from the DB settings alone, for the CLI commands that
// read aura.settings without booting the daemon (settingsListerForCLI).
func openSettingsOverlayPool(ctx context.Context) (*pgxpool.Pool, bool, error) {
	dbCfg := config.LoadDB()
	if strings.TrimSpace(dbCfg.DB.URL) == "" {
		return nil, false, nil
	}
	pool, err := db.Open(ctx, &dbCfg.DB)
	if err != nil {
		return nil, false, err
	}
	return pool, true, nil
}
