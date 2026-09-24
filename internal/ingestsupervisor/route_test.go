package ingestsupervisor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/embeddings"
)

type routeRows struct {
	rows   []sqlc.AuraSettings
	secret string
}

func (r routeRows) List(context.Context) ([]sqlc.AuraSettings, error) { return r.rows, nil }
func (r routeRows) Secret(context.Context, string) (string, error)    { return r.secret, nil }

func noEnv(string) (string, bool) { return "", false }

// localSidecar answers /v1/models as llama.cpp b10964 did on the lab VM (trimmed to what the
// attestation reads), with a GGUF size the test can change under it.
func localSidecar(t *testing.T, size *atomic.Int64) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = fmt.Fprintf(w, `{"data":[{"id":"/models/embeddinggemma-300M-Q8_0.gguf",`+
			`"meta":{"n_embd":768,"n_params":307581696,"size":%d,"ftype":"Q8_0"}}]}`, size.Load())
	}))
	t.Cleanup(server.Close)
	return server
}

// hostedCatalogue serves an OpenRouter-shaped /v1/embeddings/models and counts its reads.
func hostedCatalogue(t *testing.T, reads *atomic.Int32) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/embeddings/models" {
			http.NotFound(w, r)
			return
		}
		reads.Add(1)
		_, _ = io.WriteString(w, `{"data":[{"id":"vendor/embed","context_length":8192},`+
			`{"id":"vendor/other","context_length":512}]}`)
	}))
	t.Cleanup(server.Close)
	return server
}

func hostedRows(base, model string) []sqlc.AuraSettings {
	return []sqlc.AuraSettings{
		{Key: "AURA_EMBED_BASE_URL", Value: "http://aura-llama-embed:8081"},
		{Key: "AURA_EMBED_MODEL", Value: model},
		{Key: "AURA_EMBED_CLOUD_BASE_URL", Value: base},
	}
}

func TestRouteResolverNamesTheLocalSidecarsSpace(t *testing.T) {
	var size atomic.Int64
	size.Store(327060480)
	sidecar := localSidecar(t, &size)
	resolver := &RouteResolver{
		Store:     routeRows{rows: []sqlc.AuraSettings{{Key: "AURA_EMBED_BASE_URL", Value: sidecar.URL}}},
		LookupEnv: noEnv, Dimensions: 768, HTTP: sidecar.Client(),
	}

	route, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := embeddings.SpaceFor(config.EmbedLocal, "", "", 768,
		"embeddinggemma-300M-Q8_0.gguf|size=327060480|params=307581696|embd=768|ftype=Q8_0")
	if route.Space != want.ID {
		t.Fatalf("space = %q, want %q: a child would stamp a space the daemon never names", route.Space, want.ID)
	}
	if route.BaseURL != sidecar.URL || route.TokenizerURL != sidecar.URL || route.Model != "" ||
		route.APIKey != "" || route.InputLimit != 0 || route.Dimensions != 768 {
		t.Fatalf("route = %+v, want the local sidecar with no model, key or limit", route)
	}
}

// A GGUF swapped under unchanged settings moves the space on the next tick, as it moves on the
// daemon's next call (embeddings.Route): attesting only on a route change would stamp the new
// model's vectors with the old space.
func TestRouteResolverAttestsTheSidecarOnEveryCall(t *testing.T) {
	var size atomic.Int64
	size.Store(327060480)
	sidecar := localSidecar(t, &size)
	resolver := &RouteResolver{
		Store:     routeRows{rows: []sqlc.AuraSettings{{Key: "AURA_EMBED_BASE_URL", Value: sidecar.URL}}},
		LookupEnv: noEnv, Dimensions: 768, HTTP: sidecar.Client(),
	}

	before, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("first Resolve: %v", err)
	}
	same, err := resolver.Resolve(context.Background())
	if err != nil || same.Space != before.Space {
		t.Fatalf("an unchanged sidecar moved the space (%q -> %q, %v): every tick would restart the children",
			before.Space, same.Space, err)
	}
	size.Store(999)
	after, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve after the swap: %v", err)
	}
	if after.Space == before.Space {
		t.Fatal("a swapped GGUF kept the space")
	}
}

func TestRouteResolverReadsAHostedModelsLimitOncePerRoute(t *testing.T) {
	var reads atomic.Int32
	catalogue := hostedCatalogue(t, &reads)
	resolver := &RouteResolver{
		Store:     routeRows{rows: hostedRows(catalogue.URL, "vendor/embed"), secret: "sk-test"},
		LookupEnv: noEnv, Dimensions: 768, HTTP: catalogue.Client(),
	}

	route, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	want := embeddings.SpaceFor(config.EmbedEndpoint, "vendor/embed", catalogue.URL, 768, "")
	if route.Space != want.ID || route.Model != "vendor/embed" || route.APIKey != "sk-test" ||
		route.BaseURL != catalogue.URL || route.InputLimit != 8192 ||
		route.TokenizerURL != "http://aura-llama-embed:8081" {
		t.Fatalf("route = %+v, want vendor/embed at %s, limit 8192, local tokenizer", route, catalogue.URL)
	}
	if _, err := resolver.Resolve(context.Background()); err != nil {
		t.Fatalf("second Resolve: %v", err)
	}
	if got := reads.Load(); got != 1 {
		t.Fatalf("catalogue read %d times for one route, want once: it would be read on every tick", got)
	}

	resolver.Store = routeRows{rows: hostedRows(catalogue.URL, "vendor/other"), secret: "sk-test"}
	other, err := resolver.Resolve(context.Background())
	if err != nil {
		t.Fatalf("Resolve after the model change: %v", err)
	}
	if other.InputLimit != 512 || reads.Load() != 2 {
		t.Fatalf("limit %d after %d reads, want 512 after a second read: a new model kept the old limit",
			other.InputLimit, reads.Load())
	}
}

func TestRouteResolverFailsWhenTheHostedCatalogueIsDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	resolver := &RouteResolver{
		Store:     routeRows{rows: hostedRows(server.URL, "vendor/embed"), secret: "sk-test"},
		LookupEnv: noEnv, Dimensions: 768, HTTP: server.Client(),
	}

	if route, err := resolver.Resolve(context.Background()); err == nil {
		t.Fatalf("resolved %+v with no input limit: the child would cut every input to nothing", route)
	}
}

func TestRouteResolverRefusesAHostedRouteWithoutACredential(t *testing.T) {
	var reads atomic.Int32
	catalogue := hostedCatalogue(t, &reads)
	resolver := &RouteResolver{
		Store:     routeRows{rows: hostedRows(catalogue.URL, "vendor/embed")},
		LookupEnv: noEnv, Dimensions: 768, HTTP: catalogue.Client(),
	}

	_, err := resolver.Resolve(context.Background())
	if !errors.Is(err, embeddings.ErrNoCredential) {
		t.Fatalf("Resolve error = %v, want ErrNoCredential", err)
	}
	if reads.Load() != 0 {
		t.Fatal("the catalogue was asked for a route that cannot embed")
	}
}

func TestRouteResolverRefusesALocalRouteWithNoBase(t *testing.T) {
	resolver := &RouteResolver{
		Store:     routeRows{rows: []sqlc.AuraSettings{{Key: "AURA_EMBED_BASE_URL", Value: ""}}},
		LookupEnv: noEnv, Dimensions: 768,
	}

	if _, err := resolver.Resolve(context.Background()); !errors.Is(err, embeddings.ErrNoRoute) {
		t.Fatalf("Resolve error = %v, want ErrNoRoute", err)
	}
}
