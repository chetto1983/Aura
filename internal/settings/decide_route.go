package settings

import (
	"context"
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/decide"
	"github.com/chetto1983/aura/internal/llm"
)

// DecideRoute reads the decision model's route from aura.settings on every call. AURA_DECIDE_MODEL
// is a present row first, even when empty, then lookupEnv: empty is the local sidecar, "off" is no
// route at all, and anything else is an OpenRouter model id paid with the services key.
// AURA_DECIDE_BASE_URL is read from lookupEnv only. The sidecar's address is the installer's, so
// no settings row can move it, and without one the empty model has nowhere to go and is off.
func DecideRoute(ctx context.Context, store SecretLister, lookupEnv func(string) (string, bool)) (decide.Route, error) {
	rows, err := store.List(ctx)
	if err != nil {
		return decide.Route{}, fmt.Errorf("decision route: %w", err)
	}
	model, stored := "", false
	for _, row := range rows {
		if row.Key == "AURA_DECIDE_MODEL" {
			model, stored = strings.TrimSpace(row.Value), true
		}
	}
	if !stored {
		value, _ := lookupEnv("AURA_DECIDE_MODEL")
		model = strings.TrimSpace(value)
	}
	switch model {
	case config.DecideOff:
		return decide.Route{}, nil
	case "":
		base, _ := lookupEnv("AURA_DECIDE_BASE_URL")
		return decide.Route{BaseURL: strings.TrimSpace(base)}, nil
	}
	return decide.Route{
		BaseURL: llm.DefaultBaseURL,
		Model:   model,
		Key:     Credentials{Store: store, LookupEnv: lookupEnv}.ServicesKey,
	}, nil
}
