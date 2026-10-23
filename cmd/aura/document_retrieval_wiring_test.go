package main

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/documents"
	"github.com/chetto1983/aura/internal/embeddings"
	"github.com/chetto1983/aura/internal/settings"
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
	if retriever == nil || retriever.ControlPlane != nil || retriever.PassageIndex != nil {
		t.Fatalf("retriever = %#v, want neither a control plane nor a projection", retriever)
	}
	if _, err := retriever.Retrieve(t.Context(), documents.RetrievalRequest{
		IdentityID: "00000000-0000-0000-0000-000000000001", Query: "clienti",
	}); err == nil {
		t.Fatal("Retrieve answered without a control plane instead of failing")
	}
}

// The document query route reads the same key the memory route does, at every request
// (TestMemoryEmbedderReadsTheEndpointKeyOnEveryCall).
func TestDocumentQueryEmbedderReadsTheEndpointKeyOnEveryCall(t *testing.T) {
	server := newAuthorizationRecorder(t)
	creds, store := storedCredentials(map[string]string{
		settings.ServicesKeySetting: "services", settings.EmbedEndpointKeySetting: "k1",
	})
	cfg := &config.Config{Embed: config.EmbedConfig{CloudModel: "vendor/embed", CloudBaseURL: server.URL, Dimensions: 768}}
	embedder := newQueryEmbedder(cfg, creds)
	if _, err := embedder.Embed(t.Context(), []string{"a"}); err != nil {
		t.Fatalf("Embed: %v", err)
	}
	store.set(settings.EmbedEndpointKeySetting, "k2")
	if _, err := embedder.Embed(t.Context(), []string{"b"}); err != nil {
		t.Fatalf("Embed after rotation: %v", err)
	}
	server.assertEmbeddedWith(t, "Bearer services", "Bearer k1", "Bearer k2")
}

// Measured on the lab VM 2026-10-23: with the chat on a ChatGPT plan its profile carries no
// key, and the query route, which read that profile, answered ErrNoCredential while the
// services key sat in the settings. The OpenRouter route names its space from the stored key.
func TestQueryEmbedderNamesTheOpenRouterSpaceWithTheStoredServicesKey(t *testing.T) {
	cfg := &config.Config{Embed: config.EmbedConfig{CloudModel: "perplexity/pplx-embed-v1-4b", Dimensions: 768}}
	creds, _ := storedCredentials(map[string]string{settings.ServicesKeySetting: "services"})
	if _, err := newQueryEmbedder(cfg, creds).Space(t.Context()); err != nil {
		t.Fatalf("Space with the services key stored: %v", err)
	}
	keyless, _ := storedCredentials(map[string]string{})
	if _, err := newQueryEmbedder(cfg, keyless).Space(t.Context()); !errors.Is(err, embeddings.ErrNoCredential) {
		t.Fatalf("Space with no key anywhere: err = %v, want ErrNoCredential", err)
	}
}

// The daemon must read documents in the space the ingest supervisor stamps them with, or the
// documents gate never opens. Both sides meet at one literal: the supervisor's
// TestRouteResolverNamesTheLocalSidecarsSpace and arcadedb's
// TestEmbeddingGemmaFloorsAreKeyedByTheSpaceTheVMAttests pin the same attestation to it.
func TestDocumentQuerySpaceIsTheSpaceIngestStamps(t *testing.T) {
	sidecar := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":[{"id":"/models/embeddinggemma-300M-Q8_0.gguf",`+
			`"meta":{"n_embd":768,"n_params":307581696,"size":327060480,"ftype":"Q8_0"}}]}`)
	}))
	t.Cleanup(sidecar.Close)
	space, err := newQueryEmbedder(&config.Config{Embed: config.EmbedConfig{BaseURL: sidecar.URL, Dimensions: 768}}, settings.Credentials{}).
		Space(t.Context())
	if err != nil || space.ID != "es1-e0aa6accf0b79c6b" {
		t.Fatalf("daemon reads documents in %q (err %v), want es1-e0aa6accf0b79c6b", space.ID, err)
	}
}
