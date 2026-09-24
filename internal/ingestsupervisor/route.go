package ingestsupervisor

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/settings"
)

// EmbedRoute is the embedding binding every child receives (services/ingest/embed.py). A
// child derives none of it: the space in particular is named here, by the resolution the
// daemon runs, so a Python vector and a Go vector from one route carry one stamp (spec §1).
type EmbedRoute struct {
	BaseURL    string
	Model      string // empty on the local route
	APIKey     string // empty on the local route; never logged
	Space      string
	Dimensions int
	// InputLimit is a hosted model's published limit in tokens, which the child turns into a
	// byte cut; 0 on the local route, where the child asks the tokenizer instead.
	InputLimit int
	// TokenizerURL is the local sidecar whichever route embeds: chunk boundaries are counted
	// with its tokenizer, so a model change never re-chunks a document (audit F9).
	TokenizerURL string
}

// Environment is the route as the child reads it.
func (r EmbedRoute) Environment() []string {
	return []string{
		"AURA_EMBED_BASE_URL=" + r.BaseURL,
		"AURA_EMBED_MODEL=" + r.Model,
		"AURA_EMBED_API_KEY=" + r.APIKey,
		"AURA_EMBED_SPACE=" + r.Space,
		"AURA_EMBED_DIMENSIONS=" + strconv.Itoa(r.Dimensions),
		"AURA_EMBED_INPUT_LIMIT=" + strconv.Itoa(r.InputLimit),
		"AURA_EMBED_TOKENIZER_URL=" + r.TokenizerURL,
	}
}

// RouteResolver reads the route from aura.settings on every call. The supervisor never
// overlays the rows onto its own environment, so LookupEnv is the environment as it was
// before any row: the fallback a deleted row must fall back to (spec §6).
type RouteResolver struct {
	Store      settings.SecretLister
	LookupEnv  func(string) (string, bool)
	Dimensions int
	HTTP       *http.Client

	limit hostedLimit
}

// hostedLimit remembers the last hosted model's input limit: the catalogue is read once per
// route change, not on every tick.
type hostedLimit struct {
	base, model string
	tokens      int
}

// Resolve names the route and its space. The local sidecar is attested on every call, as the
// daemon's route attests it (embeddings.Route), so a GGUF swapped under unchanged settings
// moves the space on the next tick. A route that cannot embed -- a hosted model without a
// credential, a local route without a base -- is an error, and the caller keeps what runs.
func (r *RouteResolver) Resolve(ctx context.Context) (EmbedRoute, error) {
	embed, key, err := settings.EmbedRoute(ctx, r.Store, r.LookupEnv, settings.DefaultEmbedBaseURL)
	if err != nil {
		return EmbedRoute{}, err
	}
	base, credential, model := config.ResolveEmbedRoute(embed, key)
	hosted := config.EmbedRouteKind(embed) != config.EmbedLocal
	if hosted && credential == "" {
		return EmbedRoute{}, embeddings.ErrNoCredential
	}
	space, err := embeddings.RouteSpace(ctx, r.HTTP, embed, r.Dimensions)
	if err != nil {
		return EmbedRoute{}, fmt.Errorf("embedding space: %w", err)
	}
	route := EmbedRoute{
		BaseURL: base, Model: model, APIKey: credential, Space: space.ID,
		Dimensions: r.Dimensions, TokenizerURL: embed.BaseURL,
	}
	if hosted {
		if route.InputLimit, err = r.inputLimit(ctx, base, model, credential); err != nil {
			return EmbedRoute{}, err
		}
	}
	return route, nil
}

func (r *RouteResolver) inputLimit(ctx context.Context, base, model, key string) (int, error) {
	if r.limit.base == base && r.limit.model == model {
		return r.limit.tokens, nil
	}
	client := &embeddings.Client{BaseURL: base, Model: model, APIKey: key, Client: r.HTTP}
	tokens, err := client.InputLimit(ctx)
	if err != nil {
		return 0, fmt.Errorf("embedding input limit: %w", err)
	}
	r.limit = hostedLimit{base: base, model: model, tokens: tokens}
	return tokens, nil
}
