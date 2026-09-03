package agui

// Handler tests for GET /api/settings/llm-models. The outbound probe is replaced by the
// modelCatalog seam, so every branch runs without a network.

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
)

type catalogCall struct {
	provider string
	baseURL  string
	apiKey   string
}

func catalogServerWith(
	t *testing.T, entries []llm.ModelCatalogEntry, err error, rows []sqlc.AuraSettings,
) (*Server, *[]catalogCall) {
	t.Helper()
	calls := &[]catalogCall{}
	s := &Server{settings: &fakeSettingsStore{rows: rows}}
	s.modelCatalog = func(_ context.Context, provider, baseURL, apiKey string) ([]llm.ModelCatalogEntry, error) {
		*calls = append(*calls, catalogCall{provider: provider, baseURL: baseURL, apiKey: apiKey})
		return entries, err
	}
	return s, calls
}

func getModels(t *testing.T, s *Server, query string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	s.handleListLLMModels(rr, httptest.NewRequest(http.MethodGet, "/api/settings/llm-models?"+query, nil))
	return rr
}

func TestHandleListLLMModelsServesEveryProvider(t *testing.T) {
	for _, tc := range []struct {
		name     string
		provider string
		baseURL  string
		entries  []llm.ModelCatalogEntry
		wantKey  string
	}{
		{
			name: "llama.cpp serves one loaded model with its window", provider: "llamacpp",
			baseURL: "http://host.docker.internal:8084/v1",
			entries: []llm.ModelCatalogEntry{{ID: "gemma-4-12b", ContextWindow: 131072}},
		},
		{
			name: "ollama serves the pulled tags", provider: "ollama",
			baseURL: "http://host.docker.internal:11434/v1",
			entries: []llm.ModelCatalogEntry{{ID: "gemma4:31b-cloud"}, {ID: "qwen4:14b"}},
		},
		{
			name: "openrouter serves the priced catalogue", provider: "openrouter",
			baseURL: "https://openrouter.ai/api/v1",
			entries: []llm.ModelCatalogEntry{
				{
					ID: "z-ai/glm-5.3", ContextWindow: 204800,
					Price: llm.Price{InputPer1M: 0.14, OutputPer1M: 0.28}, HasPrice: true,
				},
				{ID: "openrouter/auto", ContextWindow: 2000000},
			},
			wantKey: "sk-or-stored",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, calls := catalogServerWith(t, tc.entries, nil,
				[]sqlc.AuraSettings{{Key: "OPENROUTER_API_KEY", Value: "sk-or-stored", IsSecret: true}})

			rr := getModels(t, s, "provider="+tc.provider+"&base_url="+url.QueryEscape(tc.baseURL))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d (%s), want 200", rr.Code, rr.Body.String())
			}
			var out modelCatalogDTO
			if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
				t.Fatalf("decode: %v", err)
			}
			if out.Provider != tc.provider || out.BaseURL != tc.baseURL {
				t.Fatalf("echoed route = %+v, want the probed one", out)
			}
			if len(out.Models) != len(tc.entries) {
				t.Fatalf("models = %+v, want %d", out.Models, len(tc.entries))
			}
			for i, want := range tc.entries {
				got := out.Models[i]
				if got.ID != want.ID || got.ContextWindow != want.ContextWindow || got.HasPrice != want.HasPrice {
					t.Fatalf("models[%d] = %+v, want %+v", i, got, want)
				}
				// An unpriced model reports has_price false and carries no rate at all: a
				// fabricated $0 would read as "free" on a model nobody has priced.
				if got.InputPer1M != want.Price.InputPer1M || got.OutputPer1M != want.Price.OutputPer1M {
					t.Fatalf("models[%d] rates = %v/%v, want %v/%v",
						i, got.InputPer1M, got.OutputPer1M, want.Price.InputPer1M, want.Price.OutputPer1M)
				}
			}
			// The OpenRouter key belongs to OpenRouter alone: a local endpoint the operator
			// runs must never be handed the cloud credential.
			if len(*calls) != 1 || (*calls)[0].apiKey != tc.wantKey {
				t.Fatalf("catalog calls = %+v, want apiKey %q", *calls, tc.wantKey)
			}
		})
	}
}

func TestHandleListLLMModelsRejectsBadRoutes(t *testing.T) {
	for _, tc := range []struct {
		name  string
		query string
		want  string
	}{
		{"unknown provider", "provider=vllm&base_url=http%3A%2F%2Fhost%3A1%2Fv1", "provider must be"},
		{"missing provider", "base_url=http%3A%2F%2Fhost%3A1%2Fv1", "provider must be"},
		{"relative base URL", "provider=ollama&base_url=%2Fv1", "absolute http(s) URL"},
		{"credentials in URL", "provider=ollama&base_url=http%3A%2F%2Fu%3Ap%40host%2Fv1", "absolute http(s) URL"},
		{"query in base URL", "provider=ollama&base_url=http%3A%2F%2Fhost%2Fv1%3Fx%3D1", "absolute http(s) URL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, calls := catalogServerWith(t, nil, nil, nil)
			rr := getModels(t, s, tc.query)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d (%s), want 400", rr.Code, rr.Body.String())
			}
			if !strings.Contains(rr.Body.String(), tc.want) {
				t.Fatalf("body = %s, want it to mention %q", rr.Body.String(), tc.want)
			}
			if len(*calls) != 0 {
				t.Fatalf("a rejected route still probed: %+v", *calls)
			}
		})
	}
}

func TestHandleListLLMModelsReportsWhyTheProbeFailed(t *testing.T) {
	s, _ := catalogServerWith(t, nil, errors.New("model catalog unavailable: GET /models returned 401"), nil)

	rr := getModels(t, s, "provider=openrouter&base_url=https%3A%2F%2Fopenrouter.ai%2Fapi%2Fv1")
	if rr.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rr.Code)
	}
	// A shrug ("HTTP 502") would leave the operator guessing between a dead host and a bad
	// key; the upstream status is the whole value of this response.
	if !strings.Contains(rr.Body.String(), "401") {
		t.Fatalf("body = %s, want the upstream reason", rr.Body.String())
	}
}
