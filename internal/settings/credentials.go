package settings

import (
	"context"
	"strings"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/llm"
)

// The two sealed credentials the shared cloud services send.
const (
	// ServicesKeySetting is the deployment's OpenRouter key, minted as aura-services: embeddings,
	// cloud speech and vision on OpenRouter spend it, whatever provider the chat runs on.
	ServicesKeySetting = "OPENROUTER_API_KEY"
	// EmbedEndpointKeySetting is a manual embedding endpoint's own key. The services key never
	// goes to a manual endpoint: it is another provider, at whatever URL the admin typed.
	EmbedEndpointKeySetting = "AURA_EMBED_CLOUD_API_KEY"
)

// SecretReader reads one sealed row, "" when it is absent.
type SecretReader interface {
	Secret(ctx context.Context, key string) (string, error)
}

// Credentials reads the shared cloud services' credentials from aura.settings on every call, so
// a key the reconciler replaces reaches a running process on its next request. It never reads
// the chat profile: a ChatGPT profile carries no key, and the services' key is not the chat's
// (docs/superpowers/specs/2026-10-23-cloud-service-credentials-design.md). A nil Store reads the
// environment only; a nil LookupEnv reads none.
type Credentials struct {
	Store     SecretReader
	LookupEnv func(string) (string, bool)
}

// ServicesKey is the deployment's OpenRouter key.
func (c Credentials) ServicesKey(ctx context.Context) (string, error) {
	return c.read(ctx, ServicesKeySetting)
}

// EmbedKeySetting names the setting embed's route takes its credential from: the services key
// for OpenRouter, the endpoint's own key for a manual endpoint, "" for the local sidecar, which
// sends none.
func EmbedKeySetting(embed config.EmbedConfig) string {
	switch config.EmbedRouteKind(embed) {
	case config.EmbedOpenRouter:
		return ServicesKeySetting
	case config.EmbedEndpoint:
		return EmbedEndpointKeySetting
	default:
		return ""
	}
}

// EmbedKey is the credential embed's route sends (EmbedKeySetting).
func (c Credentials) EmbedKey(ctx context.Context, embed config.EmbedConfig) (string, error) {
	setting := EmbedKeySetting(embed)
	if setting == "" {
		return "", nil
	}
	return c.read(ctx, setting)
}

// PrimaryRouteKey is the credential vision sends through the primary chat route: the services
// key when that route is OpenRouter, none on any other host.
func (c Credentials) PrimaryRouteKey(ctx context.Context, provider, baseURL string) (string, error) {
	if llm.ReasoningTarget(provider, baseURL) != llm.ReasoningTargetOpenRouter {
		return "", nil
	}
	return c.ServicesKey(ctx)
}

// read is a sealed row's value, else the environment's: a row wins when it is set.
func (c Credentials) read(ctx context.Context, key string) (string, error) {
	if c.Store != nil {
		value, err := c.Store.Secret(ctx, key)
		if err != nil {
			return "", err
		}
		if value = strings.TrimSpace(value); value != "" {
			return value, nil
		}
	}
	if c.LookupEnv == nil {
		return "", nil
	}
	value, _ := c.LookupEnv(key)
	return strings.TrimSpace(value), nil
}
