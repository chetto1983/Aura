package settings

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/config"
)

// SecretLister is what reading a route needs: the rows, and the route's sealed credential.
type SecretLister interface {
	Lister
	SecretReader
}

// DefaultEmbedBaseURL is the product's local sidecar, the base a route falls back to when
// neither a row nor the environment names one. An explicitly empty row still wins: that is
// how the operator switches dense retrieval off.
const DefaultEmbedBaseURL = "http://aura-llama-embed:8081"

// EmbedRoute reads the embedding route from aura.settings without touching the process
// environment. A present row wins even when empty (an empty AURA_EMBED_BASE_URL switches
// dense retrieval off); an absent row falls back to lookupEnv, then to defaultLocalBase
// for the local base only -- the same precedence OverlayEnv gives at boot, minus its one
// flaw: OverlayEnv never unsets, so a process re-reading through it could not see a
// deleted row. The credential is the route's own (Credentials.EmbedKey): none for the local
// sidecar, the services key for OpenRouter, AURA_EMBED_CLOUD_API_KEY for a manual endpoint.
func EmbedRoute(
	ctx context.Context, store SecretLister, lookupEnv func(string) (string, bool), defaultLocalBase string,
) (config.EmbedConfig, string, error) {
	rows, err := store.List(ctx)
	if err != nil {
		return config.EmbedConfig{}, "", fmt.Errorf("embedding route: %w", err)
	}
	stored := make(map[string]string, len(rows))
	for _, row := range rows {
		stored[row.Key] = row.Value
	}
	value := func(key, fallback string) string {
		if v, ok := stored[key]; ok {
			return strings.TrimSpace(v)
		}
		if v, ok := lookupEnv(key); ok {
			return strings.TrimSpace(v)
		}
		return fallback
	}
	embed := config.EmbedConfig{
		BaseURL:      value("AURA_EMBED_BASE_URL", defaultLocalBase),
		CloudModel:   value("AURA_EMBED_MODEL", ""),
		CloudBaseURL: value("AURA_EMBED_CLOUD_BASE_URL", ""),
	}
	key, err := Credentials{Store: store, LookupEnv: lookupEnv}.EmbedKey(ctx, embed)
	if err != nil {
		return config.EmbedConfig{}, "", fmt.Errorf("embedding credential: %w", err)
	}
	return embed, key, nil
}
