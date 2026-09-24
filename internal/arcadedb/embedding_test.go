package arcadedb

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/embeddings"
)

// The space is only logged at boot, and the listener starts after it: a sidecar that
// accepts the connection and never answers must not hold boot for the client's full minute.
func TestSpaceWithinGivesUpOnAStalledSidecar(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); srv.Close() })

	done := make(chan error, 1)
	go func() {
		_, err := SpaceWithin(NewMemoryEmbedder(config.EmbedConfig{BaseURL: srv.URL}, nil), 50*time.Millisecond)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("SpaceWithin succeeded against a sidecar that never answered")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SpaceWithin still waiting after 5s: the deadline was not applied")
	}
}

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
