package embeddings

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

// Route is one resolved embedding route at one width: it embeds, and it names the space
// its vectors are in. The space is read, never remembered: the local route asks the
// sidecar on every call (AttestLocal), so a model swapped under a running process is named
// by the next write, not by a restart.
type Route struct {
	client *Client
	embed  config.EmbedConfig
}

// NewRoute builds the route embed resolves to, or nil when dense embedding is switched off
// (the local route with no base). credential is read on every request of a cloud route; a
// cloud route built without one refuses to embed instead of sending an unauthenticated
// request.
func NewRoute(embed config.EmbedConfig, credential func() string, dims int, timeout time.Duration) *Route {
	base, _, model := config.ResolveEmbedRoute(embed, "")
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		return nil
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	client := &Client{
		BaseURL: base, Model: model, Dimensions: dims,
		Client: &http.Client{Timeout: timeout}, Timeout: timeout,
	}
	if config.EmbedRouteKind(embed) != config.EmbedLocal {
		if credential == nil {
			credential = func() string { return "" }
		}
		client.Credential = credential
	}
	return &Route{client: client, embed: embed}
}

// Embed embeds texts through the route.
func (r *Route) Embed(ctx context.Context, texts []string) ([][]float64, error) {
	return r.client.Embed(ctx, texts)
}

// Space names the space this route's vectors are in. A hosted route without a credential
// produces no vector, so it has no space either.
func (r *Route) Space(ctx context.Context) (Space, error) {
	if r.client.hosted() && r.client.key() == "" {
		return Space{}, ErrNoCredential
	}
	return RouteSpace(ctx, r.client.httpClient(), r.embed, r.client.Dimensions)
}
