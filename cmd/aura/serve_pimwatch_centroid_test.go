package main

import (
	"context"
	"testing"

	"github.com/chetto1983/aura/internal/arcadedb"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/pimwatch"
)

type centroidRouteEmbedder struct{}

func (centroidRouteEmbedder) Embed(context.Context, []string) ([][]float64, error) { return nil, nil }

func (centroidRouteEmbedder) Space(context.Context) (embeddings.Space, error) {
	return embeddings.Space{ID: "space-1"}, nil
}

func TestThePIMWatchShadowNeedsMemoryAndAnEmbedder(t *testing.T) {
	tenants := arcadedb.NewTenantClients(arcadedb.Config{BaseURL: "http://127.0.0.1:1"}, nil, nil, nil)
	store := pimwatch.NewStore(nil)
	for name, chat := range map[string]*chatEnv{
		"no memory":   {documentEmbedder: centroidRouteEmbedder{}},
		"no embedder": {memoryClients: tenants},
	} {
		if centroid := newPIMWatchCentroid(chat, store); centroid != nil {
			t.Fatalf("%s: centroid = %#v, want an untyped nil", name, centroid)
		}
	}
	if centroid := newPIMWatchCentroid(&chatEnv{memoryClients: tenants, documentEmbedder: centroidRouteEmbedder{}}, store); centroid == nil {
		t.Fatal("memory and an embedder gave no centroid")
	}
}
