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

// This is the product default when no row exists. OverlayEnv sets even an empty stored
// value, so the operator can still disable dense retrieval explicitly.
const defaultMemoryEmbedBaseURL = "http://aura-llama-embed:8081"

type bootSettingsStore interface {
	settings.Lister
	Secret(context.Context, string) (string, error)
}

type bootSettingsOpener func(context.Context, string, string) (bootSettingsStore, func(), error)

func loadBootSettings(ctx context.Context) (string, error) {
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
) (string, error) {
	if strings.TrimSpace(dsn) == "" {
		return "", fmt.Errorf("AURA_DB_URL is required for the settings authority")
	}
	if strings.TrimSpace(authulaSecret) == "" {
		return "", fmt.Errorf("AURA_AUTHULA_SECRET is required to read sealed settings")
	}
	store, closeStore, err := open(ctx, dsn, authulaSecret)
	if err != nil {
		return "", fmt.Errorf("settings database: %w", err)
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

func applyBootSettings(ctx context.Context, store bootSettingsStore) (string, error) {
	// Reusing OverlayEnv keeps the daemon and sidecar on the same allowlist and parsers;
	// a second direct row-to-config mapping would become another configuration authority.
	if err := settings.OverlayEnv(ctx, store); err != nil {
		return "", fmt.Errorf("settings overlay: %w", err)
	}
	key, err := store.Secret(ctx, "OPENROUTER_API_KEY")
	if err != nil {
		return "", fmt.Errorf("embedding credential: %w", err)
	}
	return strings.TrimSpace(key), nil
}

type embeddingRoute struct {
	baseURL string
	model   string
	apiKey  string
}

func embeddingRouteFromEnv(apiKey string) embeddingRoute {
	// LookupEnv preserves the distinction between no row (the product default) and an
	// explicitly empty row (lexical-only retrieval).
	localBase, present := os.LookupEnv("AURA_EMBED_BASE_URL")
	if !present {
		localBase = defaultMemoryEmbedBaseURL
	}
	baseURL, key, model := config.ResolveEmbedRoute(config.EmbedConfig{
		BaseURL:      strings.TrimSpace(localBase),
		CloudModel:   strings.TrimSpace(os.Getenv("AURA_EMBED_MODEL")),
		CloudBaseURL: strings.TrimSpace(os.Getenv("AURA_EMBED_CLOUD_BASE_URL")),
	}, apiKey)
	return embeddingRoute{baseURL: baseURL, model: model, apiKey: key}
}
