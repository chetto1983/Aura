package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestNewHostDocumentRetrieverKeepsCardOnlyFallbackWithoutArcadeDB(t *testing.T) {
	retriever, err := newHostDocumentRetriever(&config.Config{}, &pgxpool.Pool{})
	if err != nil {
		t.Fatal(err)
	}
	if retriever == nil || retriever.ControlPlane == nil || retriever.Projection != nil {
		t.Fatalf("retriever = %#v, want PostgreSQL control plane and no projection", retriever)
	}
}
