package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/settings"
)

// This is the product default when neither a row nor the environment names a local base.
// An explicitly empty row still wins, so the operator can disable dense retrieval.
const defaultMemoryEmbedBaseURL = "http://aura-llama-embed:8081"

type bootSettingsStore = settings.SecretLister

type bootSettingsOpener func(context.Context, string, string) (bootSettingsStore, func(), error)

type embeddingRoute struct {
	embed   config.EmbedConfig
	baseURL string
	model   string
	apiKey  string
}

func loadBootSettings(ctx context.Context) (embeddingRoute, error) {
	return loadBootSettingsWith(
		ctx,
		os.Getenv("AURA_DB_URL"),
		os.Getenv("AURA_AUTHULA_SECRET"),
		openBootSettings,
	)
}

func loadBootSettingsWith(
	ctx context.Context,
	dsn string,
	authulaSecret string,
	open bootSettingsOpener,
) (embeddingRoute, error) {
	if strings.TrimSpace(dsn) == "" {
		return embeddingRoute{}, fmt.Errorf("AURA_DB_URL is required for the settings authority")
	}
	if strings.TrimSpace(authulaSecret) == "" {
		return embeddingRoute{}, fmt.Errorf("AURA_AUTHULA_SECRET is required to read sealed settings")
	}
	store, closeStore, err := open(ctx, dsn, authulaSecret)
	if err != nil {
		return embeddingRoute{}, fmt.Errorf("settings database: %w", err)
	}
	defer closeStore()
	return applyBootSettings(ctx, store)
}

func openBootSettings(ctx context.Context, dsn, authulaSecret string) (bootSettingsStore, func(), error) {
	pool, err := db.Open(ctx, &db.Config{URL: dsn})
	if err != nil {
		return nil, nil, err
	}
	store, err := settings.NewStore(pool, authulaSecret)
	if err != nil {
		pool.Close()
		return nil, nil, fmt.Errorf("settings store: %w", err)
	}
	return store, pool.Close, nil
}

// applyBootSettings overlays the non-embedding rows (memory bounds, timeouts) onto the
// environment, as the daemon does, and resolves the embedding route from the rows
// themselves: settings.EmbedRoute is the one mapping the ingest supervisor shares, and it
// can see a deleted row where a re-read through the environment could not.
func applyBootSettings(ctx context.Context, store bootSettingsStore) (embeddingRoute, error) {
	if err := settings.OverlayEnv(ctx, store); err != nil {
		return embeddingRoute{}, fmt.Errorf("settings overlay: %w", err)
	}
	embed, key, err := settings.EmbedRoute(ctx, store, os.LookupEnv, defaultMemoryEmbedBaseURL)
	if err != nil {
		return embeddingRoute{}, err
	}
	baseURL, credential, model := config.ResolveEmbedRoute(embed, key)
	return embeddingRoute{embed: embed, baseURL: baseURL, model: model, apiKey: credential}, nil
}

// errString keeps a failed attestation visible in the boot log without failing boot: the
// local sidecar may still be loading, and the space is informational until plan 2 stamps it.
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
