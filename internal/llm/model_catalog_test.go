package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The three payloads are the three real catalogue shapes: OpenRouter publishes
// context_length and pricing as JSON STRINGS, llama.cpp publishes meta.n_ctx and nothing
// else, Ollama's /v1/models publishes ids alone.
const (
	openRouterCatalogBody = `{"data":[
		{"id":"z-ai/glm-5.3","context_length":204800,
		 "pricing":{"prompt":"0.00000014","completion":"0.00000028","input_cache_read":"0.00000003"}},
		{"id":"deepseek/deepseek-v4-flash","context_length":1000000,
		 "pricing":{"prompt":"0.0000002","completion":"0.0000008"}},
		{"id":"  ","context_length":4096,"pricing":{"prompt":"0","completion":"0"}}
	]}`
	llamaCppCatalogBody = `{"data":[{"id":"gemma-4-12b","meta":{"n_ctx":131072}}]}`
	ollamaCatalogBody   = `{"data":[{"id":"qwen4:14b"},{"id":"gemma4:31b-cloud"}]}`
)

func catalogServer(t *testing.T, body string, authSeen *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %s, want /v1/models", r.URL.Path)
		}
		if authSeen != nil {
			*authSeen = r.Header.Get("Authorization")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestFetchModelCatalogOpenRouterSortsAndPricesEntries(t *testing.T) {
	var auth string
	srv := catalogServer(t, openRouterCatalogBody, &auth)

	entries, err := FetchModelCatalog(
		context.Background(), srv.Client(), "openrouter", srv.URL+"/v1", "sk-or-test",
	)
	if err != nil {
		t.Fatalf("FetchModelCatalog: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d (%+v), want 2 — the blank id must be dropped", len(entries), entries)
	}
	if entries[0].ID != "deepseek/deepseek-v4-flash" || entries[1].ID != "z-ai/glm-5.3" {
		t.Fatalf("entries not sorted by id: %+v", entries)
	}
	glm := entries[1]
	if glm.ContextWindow != 204800 || !glm.HasPrice {
		t.Fatalf("glm entry = %+v, want context 204800 with a price", glm)
	}
	// Rates are per 1M tokens, so the string "0.00000014" per token is $0.14.
	if glm.Price.InputPer1M != 0.14 || glm.Price.OutputPer1M != 0.28 || glm.Price.CacheReadPer1M != 0.03 {
		t.Fatalf("glm price = %+v, want 0.14/0.28/0.03 per 1M", glm.Price)
	}
	if auth != "Bearer sk-or-test" {
		t.Fatalf("Authorization = %q, want the OpenRouter key forwarded", auth)
	}
}

func TestFetchModelCatalogLocalProvidersCarryNoPriceAndNoKey(t *testing.T) {
	for _, tc := range []struct {
		name          string
		provider      string
		body          string
		wantIDs       []string
		wantFirstCtx  int
		wantHasPrices bool
	}{
		{"llamacpp reads meta.n_ctx", "llamacpp", llamaCppCatalogBody, []string{"gemma-4-12b"}, 131072, false},
		{"ollama publishes ids only", "ollama", ollamaCatalogBody, []string{"gemma4:31b-cloud", "qwen4:14b"}, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var auth string
			srv := catalogServer(t, tc.body, &auth)
			entries, err := FetchModelCatalog(
				context.Background(), srv.Client(), tc.provider, srv.URL+"/v1", "sk-or-test",
			)
			if err != nil {
				t.Fatalf("FetchModelCatalog: %v", err)
			}
			if len(entries) != len(tc.wantIDs) {
				t.Fatalf("entries = %+v, want %d", entries, len(tc.wantIDs))
			}
			for i, want := range tc.wantIDs {
				if entries[i].ID != want {
					t.Fatalf("entries[%d].ID = %q, want %q", i, entries[i].ID, want)
				}
			}
			if entries[0].ContextWindow != tc.wantFirstCtx {
				t.Fatalf("context window = %d, want %d", entries[0].ContextWindow, tc.wantFirstCtx)
			}
			if entries[0].HasPrice != tc.wantHasPrices {
				t.Fatalf("has price = %v, want %v — a local server bills nothing per token",
					entries[0].HasPrice, tc.wantHasPrices)
			}
			// The key belongs to OpenRouter alone: a local endpoint must not be handed one.
			if auth != "" {
				t.Fatalf("Authorization = %q, want no credential on a local catalogue", auth)
			}
		})
	}
}

func TestFetchModelCatalogUnreachableIsWrapped(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, err := FetchModelCatalog(context.Background(), srv.Client(), "openrouter", srv.URL+"/v1", "")
	if !errors.Is(err, ErrModelCatalogUnavailable) {
		t.Fatalf("err = %v, want ErrModelCatalogUnavailable", err)
	}
	// The status has to survive into the message: "401" is what tells the operator the key
	// is the problem rather than the host.
	if got := err.Error(); !strings.Contains(got, "401") {
		t.Fatalf("err = %q, want the upstream status in the message", got)
	}
}
