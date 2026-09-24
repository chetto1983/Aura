package main

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/embeddings"
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
