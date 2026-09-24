package embeddings

import (
	"errors"
	"net/http"
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
	keyed := NewRoute(cloud, func() string { return "sk-or" }, 768, time.Second)
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
