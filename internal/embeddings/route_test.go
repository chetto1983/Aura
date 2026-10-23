package embeddings

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
)

func TestNewRouteIsNilWhenDenseEmbeddingIsOff(t *testing.T) {
	if route := NewRoute(config.EmbedConfig{BaseURL: "  "}, nil, 768, 0); route != nil {
		t.Fatalf("an empty local base built %+v, want nil", route)
	}
}

// The local space is the sidecar's own answer, read at the route's width, on every call.
func TestRouteNamesTheLocalSpaceFromTheSidecar(t *testing.T) {
	server := modelsServer(t, measuredModels, http.StatusOK)
	route := NewRoute(config.EmbedConfig{BaseURL: server.URL}, nil, 768, time.Second)
	space, err := route.Space(t.Context())
	if err != nil {
		t.Fatalf("Space: %v", err)
	}
	want, err := RouteSpace(t.Context(), server.Client(), config.EmbedConfig{BaseURL: server.URL}, 768)
	if err != nil {
		t.Fatalf("RouteSpace: %v", err)
	}
	if space != want {
		t.Fatalf("space = %+v, want %+v", space, want)
	}
	if route.client.hosted() {
		t.Fatal("the local route was built as a hosted one")
	}
}

// A cloud route without its key produces no vector, so it names no space; with the key it
// is OpenRouter's, whatever the chat route is (spec §0).
func TestRouteWithoutAKeyHasNoSpace(t *testing.T) {
	cloud := config.EmbedConfig{CloudModel: "perplexity/pplx-embed-v1-0.6b"}
	if _, err := NewRoute(cloud, nil, 768, time.Second).Space(t.Context()); !errors.Is(err, ErrNoCredential) {
		t.Fatalf("keyless cloud route: err = %v, want ErrNoCredential", err)
	}
	keyed := NewRoute(cloud, staticCredential("sk-or"), 768, time.Second)
	space, err := keyed.Space(t.Context())
	if err != nil {
		t.Fatalf("Space: %v", err)
	}
	if want := SpaceFor(config.EmbedOpenRouter, "perplexity/pplx-embed-v1-0.6b", "", 768, ""); space != want {
		t.Fatalf("space = %+v, want %+v", space, want)
	}
	if keyed.client.BaseURL != "https://openrouter.ai/api" {
		t.Fatalf("base = %q, want OpenRouter", keyed.client.BaseURL)
	}
}

func staticCredential(key string) CredentialFunc {
	return func(context.Context) (string, error) { return key, nil }
}

// countingServer counts every request that reaches it; a route refusing to embed must leave it
// at zero.
func countingServer(t *testing.T) (*httptest.Server, func() int) {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { requests.Add(1) }))
	t.Cleanup(server.Close)
	return server, func() int { return int(requests.Load()) }
}

// A credential the store could not read is not a missing one: the operator must see the
// store's failure, never "no key" over a key that is there.
func TestRouteReturnsTheCredentialReadError(t *testing.T) {
	errBoom := errors.New("sealed row unreadable")
	server, requests := countingServer(t)
	route := NewRoute(config.EmbedConfig{CloudModel: "mistral-embed", CloudBaseURL: server.URL},
		func(context.Context) (string, error) { return "", errBoom }, 768, time.Second)
	_, embedErr := route.Embed(t.Context(), []string{"a"})
	_, spaceErr := route.Space(t.Context())
	for name, err := range map[string]error{"Embed": embedErr, "Space": spaceErr} {
		if !errors.Is(err, errBoom) || errors.Is(err, ErrNoCredential) {
			t.Fatalf("%s: err = %v, want the read error and not ErrNoCredential", name, err)
		}
	}
	if n := requests(); n != 0 {
		t.Fatalf("%d request(s) reached the endpoint", n)
	}
}

func TestRouteRefusesAnEmptyCredential(t *testing.T) {
	server, requests := countingServer(t)
	route := NewRoute(config.EmbedConfig{CloudModel: "mistral-embed", CloudBaseURL: server.URL},
		staticCredential(""), 768, time.Second)
	_, embedErr := route.Embed(t.Context(), []string{"a"})
	_, spaceErr := route.Space(t.Context())
	for name, err := range map[string]error{"Embed": embedErr, "Space": spaceErr} {
		if !errors.Is(err, ErrNoCredential) {
			t.Fatalf("%s: err = %v, want ErrNoCredential", name, err)
		}
	}
	if n := requests(); n != 0 {
		t.Fatalf("%d request(s) reached the endpoint", n)
	}
}
