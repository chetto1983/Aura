// chat_boot_settings.go is the aura.settings half of the boot: it opens the pool, overlays
// the allowlisted settings rows onto the environment and reloads the config they feed.
// Split out of chat_boot.go (refactor-on-touch, 600-LOC cap).
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/jackc/pgx/v5/pgxpool"
)

type bootSettingsOps struct {
	openKeyless func(context.Context) (*pgxpool.Pool, bool, error)
	overlay     func(context.Context, *pgxpool.Pool) error
}

// resolveConfigAndPool loads the config and opens the DB pool, handing BOTH to
// migration gate and assembleChatEnv. It owns the pool across every post-open
// failure: if the settings overlay or the config reload fails it closes the pool
// before returning. On success it returns the OPEN pool and the caller takes over
// its lifecycle. Returning the pool from here also fixes a latent shadow bug: the
// previous inline form declared the overlay pool with `:=` inside the keyless branch,
// shadowing the outer var, so an overlay-success boot proceeded on a nil pool.
func resolveConfigAndPool(ctx context.Context, loadConfig func() (*config.Config, error), open dbOpener) (*config.Config, *pgxpool.Pool, error) {
	return resolveConfigAndPoolWithSettings(
		ctx,
		loadConfig,
		open,
		bootSettingsOps{
			openKeyless: openSettingsOverlayPool,
			overlay:     overlayStoreSettings,
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
		if !errors.Is(err, llm.ErrMissingAPIKey) && !isMissingAPIKey(err) {
			return nil, nil, err
		}
		// Keyless first load: the required infra secrets may live in the DB settings
		// overlay. openSettingsOverlayPool returns a nil pool whenever !ok/err, so a
		// failed overlay path never leaks a live pool.
		pool, ok, overlayErr := settingsOps.openKeyless(ctx)
		if overlayErr != nil || !ok {
			return nil, nil, err
		}
		overlayBootSettings(ctx, pool, settingsOps)
		cfg, err = loadConfig()
		if err != nil {
			pool.Close()
			return nil, nil, err
		}
		return cfg, pool, nil
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
