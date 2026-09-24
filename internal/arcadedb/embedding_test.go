package arcadedb

import (
	"context"
	"errors"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

// A nil *embeddings.Route in a DenseEmbedder would be non-nil, and every "no embedder"
// branch would call through it.
func TestNewMemoryEmbedderIsABareNilWhenDenseRetrievalIsOff(t *testing.T) {
	if embedder := NewMemoryEmbedder(config.EmbedConfig{BaseURL: " "}, nil); embedder != nil {
		t.Fatalf("got %#v, want a bare nil", embedder)
	}
}

func TestNewMemoryEmbedderNamesItsSpaceAtTheIndexWidth(t *testing.T) {
	embedder := NewMemoryEmbedder(config.EmbedConfig{CloudModel: "vendor/embed"}, func() string { return "key" })
	space, err := embedder.Space(context.Background())
	if err != nil {
		t.Fatalf("Space: %v", err)
	}
	if want := embeddings.SpaceFor(config.EmbedOpenRouter, "vendor/embed", "", vectorDimensions, ""); space != want {
		t.Fatalf("space = %+v, want %+v", space, want)
	}
	keyless := NewMemoryEmbedder(config.EmbedConfig{CloudModel: "vendor/embed"}, nil)
	if _, err := keyless.Space(context.Background()); !errors.Is(err, embeddings.ErrNoCredential) {
		t.Fatalf("keyless cloud route: err = %v, want ErrNoCredential", err)
	}
}
