package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// These are the two OpenAI-compatible catalogue shapes: OpenRouter publishes
// context_length and pricing as JSON STRINGS, while llama.cpp publishes meta.n_ctx.
const (
	openRouterCatalogBody = `{"data":[
		{"id":"z-ai/glm-5.3","context_length":204800,"top_provider":{"context_length":200000},
		 "pricing":{"prompt":"0.00000014","completion":"0.00000028","input_cache_read":"0.00000003"}},
		{"id":"deepseek/deepseek-v4-flash","context_length":1000000,
		 "pricing":{"prompt":"0.0000002","completion":"0.0000008"}},
		{"id":"  ","context_length":4096,"pricing":{"prompt":"0","completion":"0"}}
	]}`
	llamaCppCatalogBody = `{"data":[{"id":"gemma-4-12b","meta":{"n_ctx":131072}}]}`
)

type catalogRoundTripFunc func(*http.Request) (*http.Response, error)

func (f catalogRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

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
	if glm.ContextWindow != 204800 || glm.TopProviderContextWindow != 200000 || !glm.HasPrice {
		t.Fatalf("glm entry = %+v, want context 204800, top provider 200000, with a price", glm)
	}
	// Rates are per 1M tokens, so the string "0.00000014" per token is $0.14.
	if glm.Price.InputPer1M != 0.14 || glm.Price.OutputPer1M != 0.28 || glm.Price.CacheReadPer1M != 0.03 {
		t.Fatalf("glm price = %+v, want 0.14/0.28/0.03 per 1M", glm.Price)
	}
	if auth != "Bearer sk-or-test" {
		t.Fatalf("Authorization = %q, want the OpenRouter key forwarded", auth)
	}
}

func TestFetchModelCatalogLlamaCppCarriesNoPriceAndNoKey(t *testing.T) {
	for _, tc := range []struct {
		name          string
		provider      string
		body          string
		wantIDs       []string
		wantFirstCtx  int
		wantHasPrices bool
	}{
		{"llamacpp reads meta.n_ctx", "llamacpp", llamaCppCatalogBody, []string{"gemma-4-12b"}, 131072, false},
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

func TestFetchModelCatalogOllamaCombinesLocalAndPublicCloudTags(t *testing.T) {
	var calls []string
	client := &http.Client{Transport: catalogRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.Method+" "+req.URL.String())
		if auth := req.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization = %q, want no credential on either Ollama catalogue", auth)
		}
		body := ""
		switch req.URL.String() {
		case "http://ollama.local/api/tags":
			body = `{"models":[
				{"name":"qwen2.5:7b"},
				{"name":"gemma4:31b-cloud"},
				{"name":"  "}
			]}`
		case "https://ollama.com/api/tags":
			body = `{"models":[
				{"name":"glm-5.3","model":"glm-5.3","modified_at":"2026-08-28T08:00:00-07:00","size":755433728000,"digest":"632dfda18c6d","details":{"format":"","family":"","families":null,"parameter_size":"","quantization_level":""}},
				{"name":"gemma4:31b","model":"gemma4:31b"},
				{"name":"  ","model":"ignored"}
			]}`
		default:
			t.Fatalf("unexpected catalogue request: %s %s", req.Method, req.URL)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	entries, err := FetchModelCatalog(
		context.Background(), client, "ollama", "http://ollama.local/v1", "must-not-leak",
	)
	if err != nil {
		t.Fatalf("FetchModelCatalog: %v", err)
	}
	want := []ModelCatalogEntry{
		{ID: "gemma4:31b-cloud"},
		{ID: "glm-5.3:cloud"},
		{ID: "qwen2.5:7b"},
	}
	if !reflect.DeepEqual(entries, want) {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
	wantCalls := []string{
		"GET http://ollama.local/api/tags",
		"GET https://ollama.com/api/tags",
	}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
	}
}

func TestFetchModelProfileOllamaCloudProbesTheBridgeThenUsesPublicShow(t *testing.T) {
	var calls []string
	client := &http.Client{Transport: catalogRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		calls = append(calls, req.Method+" "+req.URL.String())
		body := ""
		status := http.StatusOK
		switch req.URL.String() {
		case "http://ollama.local/api/show":
			status = http.StatusNotFound
		case "https://ollama.com/api/show":
			var request struct {
				Model string `json:"model"`
			}
			if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			if request.Model != "glm-5.3" {
				t.Fatalf("public show model = %q, want glm-5.3", request.Model)
			}
			body = `{"model_info":{"glm_dsa_moe.context_length":1048576},"capabilities":["completion","thinking","tools"]}`
		default:
			t.Fatalf("unexpected metadata request: %s %s", req.Method, req.URL)
		}
		if auth := req.Header.Get("Authorization"); auth != "" {
			t.Fatalf("Authorization = %q, want no retained credential", auth)
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(body)),
			Request:    req,
		}, nil
	})}

	profile, err := FetchModelProfile(
		context.Background(), client, "ollama", "http://ollama.local/v1", "must-not-leak", "glm-5.3:cloud",
	)
	if err != nil {
		t.Fatalf("FetchModelProfile: %v", err)
	}
	if profile.ContextWindow != 1048576 || profile.HasPrice {
		t.Fatalf("profile = %+v, want public context and no token price", profile)
	}
	wantCalls := []string{"POST http://ollama.local/api/show", "POST https://ollama.com/api/show"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("calls = %v, want %v", calls, wantCalls)
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

func TestFetchOutputModalityCatalogFiltersByModalityWithoutACredential(t *testing.T) {
	var query, auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Errorf("path = %s, want /v1/models", r.URL.Path)
		}
		query, auth = r.URL.RawQuery, r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"data":[{"id":"qwen/qwen3-asr-1.7b","pricing":{"prompt":"0.0000075"}},
			{"id":"  "},{"id":"microsoft/mai-transcribe-2","supported_voices":[" alloy ","","alloy","nova"]}]}`))
	}))
	t.Cleanup(srv.Close)

	entries, err := FetchOutputModalityCatalog(context.Background(), srv.Client(), srv.URL+"/v1", "transcription")
	if err != nil {
		t.Fatalf("FetchOutputModalityCatalog: %v", err)
	}
	if query != "output_modalities=transcription" {
		t.Fatalf("query = %q, want output_modalities=transcription", query)
	}
	if auth != "" {
		t.Fatalf("Authorization = %q, want none on a public list", auth)
	}
	// Sorted, the blank id dropped, and no price: the rate has no unit in the payload.
	want := []ModelCatalogEntry{
		{ID: "microsoft/mai-transcribe-2", SupportedVoices: []string{"alloy", "nova"}},
		{ID: "qwen/qwen3-asr-1.7b"},
	}
	if len(entries) != len(want) || entries[0].ID != want[0].ID || entries[1].ID != want[1].ID ||
		!slices.Equal(entries[0].SupportedVoices, want[0].SupportedVoices) || entries[1].SupportedVoices != nil {
		t.Fatalf("entries = %+v, want %+v", entries, want)
	}
}

func TestFetchOutputModalityCatalogMarksAnUnreadableListUnavailable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	_, err := FetchOutputModalityCatalog(context.Background(), srv.Client(), srv.URL+"/v1", "speech")
	if !errors.Is(err, ErrModelCatalogUnavailable) || !strings.Contains(err.Error(), "503") {
		t.Fatalf("err = %v, want ErrModelCatalogUnavailable carrying the status", err)
	}
}
