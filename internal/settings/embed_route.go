package settings

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/config"
)

// SecretLister is what reading a route needs: the rows, and the one sealed credential.
type SecretLister interface {
	Lister
	Secret(ctx context.Context, key string) (string, error)
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
// deleted row. The credential is the daemon's (cmd/aura applySecretSettings): the sealed
// OPENROUTER_API_KEY when it is set, else the environment's. There is no
// embedding-specific key.
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
	key, err := store.Secret(ctx, "OPENROUTER_API_KEY")
	if err != nil {
		return config.EmbedConfig{}, "", fmt.Errorf("embedding credential: %w", err)
	}
	if key = strings.TrimSpace(key); key == "" {
		key, _ = lookupEnv("OPENROUTER_API_KEY")
	}
	return config.EmbedConfig{
		BaseURL:      value("AURA_EMBED_BASE_URL", defaultLocalBase),
		CloudModel:   value("AURA_EMBED_MODEL", ""),
		CloudBaseURL: value("AURA_EMBED_CLOUD_BASE_URL", ""),
	}, strings.TrimSpace(key), nil
}
