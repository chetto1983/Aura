package main

import (
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Without ArcadeDB there is NO retrieval at all, and Retrieve must say so rather than
// answer emptily.
//
// This test asserted the opposite until 2026-08-08: that a PostgreSQL control plane
// survived so the cascade could still degrade to card-only. That fallback is gone with the
// store behind it -- the cards ARE ArcadeDB records now, so when ArcadeDB is unreachable
// there is nothing left to rank, not merely a thinner ranking.
//
// Failing is the point. A retriever with no control plane that returned an empty success
// would report "no matching documents" for a corpus it never searched, which is the
// skip-as-green shape this codebase refuses everywhere else.
func TestNewHostDocumentRetrieverHasNoControlPlaneWithoutArcadeDB(t *testing.T) {
	retriever, err := newHostDocumentRetriever(&config.Config{}, &pgxpool.Pool{})
	if err != nil {
		t.Fatal(err)
	}
	if retriever == nil || retriever.ControlPlane != nil || retriever.Projection != nil {
		t.Fatalf("retriever = %#v, want neither a control plane nor a projection", retriever)
	}
	if _, err := retriever.Retrieve(t.Context(), documents.RetrievalRequest{
		IdentityID: "00000000-0000-0000-0000-000000000001", Query: "clienti",
	}); err == nil {
		t.Fatal("Retrieve answered without a control plane instead of failing")
	}
}
