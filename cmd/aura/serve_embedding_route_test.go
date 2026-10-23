package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/settings"
)

type spaceOnlyEmbedder struct {
	space embeddings.Space
	err   error
}

func (e spaceOnlyEmbedder) Embed(context.Context, []string) ([][]float64, error) { return nil, nil }

func (e spaceOnlyEmbedder) Space(context.Context) (embeddings.Space, error) { return e.space, e.err }

func TestEmbeddingRoutesCurrentNamesBothFamiliesSpaces(t *testing.T) {
	routes := &embeddingRoutes{
		memory:    spaceOnlyEmbedder{space: embeddings.Space{ID: "es1-mem"}},
		documents: spaceOnlyEmbedder{space: embeddings.Space{ID: "es1-docs"}}, dims: 1024,
	}
	memory, documents, err := routes.Current(context.Background())
	if err != nil || memory.ID != "es1-mem" || documents.ID != "es1-docs" || routes.Dimensions() != 1024 {
		t.Fatalf("memory %v documents %v err %v", memory, documents, err)
	}
}

// Dense retrieval switched off leaves no space to count against: the card says so.
func TestEmbeddingRoutesCurrentWithoutARouteIsErrNoRoute(t *testing.T) {
	if _, _, err := (&embeddingRoutes{}).Current(context.Background()); !errors.Is(err, embeddings.ErrNoRoute) {
		t.Fatalf("err = %v, want ErrNoRoute", err)
	}
	failing := &embeddingRoutes{
		memory:    spaceOnlyEmbedder{space: embeddings.Space{ID: "es1-mem"}},
		documents: spaceOnlyEmbedder{err: errors.New("sidecar down")},
	}
	if _, _, err := failing.Current(context.Background()); err == nil {
		t.Fatal("a documents embedder that cannot name its space passed")
	}
}

// The preview probes the route being chosen with that route's own key: a manual endpoint is
// never sent the services key, and without its own the page shows key_missing.
func TestEmbeddingRouteProbeSendsTheRoutesOwnKey(t *testing.T) {
	server := newAuthorizationRecorder(t)
	endpoint := config.EmbedConfig{CloudModel: "vendor/embed", CloudBaseURL: server.URL}
	creds, store := storedCredentials(map[string]string{settings.ServicesKeySetting: "services"})
	routes := &embeddingRoutes{credentials: creds, dims: 768, http: server.Client()}
	if _, _, err := routes.Probe(t.Context(), endpoint); !errors.Is(err, embeddings.ErrNoCredential) {
		t.Fatalf("probe without the endpoint's key: err = %v, want ErrNoCredential", err)
	}
	store.set(settings.EmbedEndpointKeySetting, "endpoint")
	_, _, _ = routes.Probe(t.Context(), endpoint)
	server.mu.Lock()
	defer server.mu.Unlock()
	if len(server.all) == 0 || server.all[0] != "Bearer endpoint" {
		t.Fatalf("probe requests carried %q, want the endpoint's own key first", server.all)
	}
	for _, got := range server.all {
		if got == "Bearer services" {
			t.Fatal("the probe sent the services key to a manual endpoint")
		}
	}
}
