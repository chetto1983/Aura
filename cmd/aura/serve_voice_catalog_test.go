package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/llm"
)

// voiceModelsServer answers GET /api/v1/models the way OpenRouter does for one output
// modality, recording what each request asked for.
func voiceModelsServer(t *testing.T, seen *[]*http.Request) *http.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*seen = append(*seen, r.Clone(context.Background()))
		if r.URL.Path != "/api/v1/models" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"microsoft/mai-voice-2-flash","supported_voices":["en-US-Harper:MAI-Voice-2"]},{"id":"fish-audio/s1"}]}`))
	}))
	t.Cleanup(server.Close)
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse fake server URL: %v", err)
	}
	return &http.Client{Transport: rewriteHost{target: target}}
}

func TestVoiceCatalogRouteListsTheModalityFromTheOpenRouterRoute(t *testing.T) {
	var seen []*http.Request
	route := voiceCatalogRoute{runtime: routeRuntime("openrouter", openRouterBaseURL), client: voiceModelsServer(t, &seen)}

	models, err := route.List(context.Background(), "speech")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(models) != 2 || models[0].ID != "fish-audio/s1" || models[1].ID != "microsoft/mai-voice-2-flash" {
		t.Fatalf("models = %+v, want both ids sorted", models)
	}
	if got := models[1].SupportedVoices; len(got) != 1 || got[0] != "en-US-Harper:MAI-Voice-2" {
		t.Fatalf("supported voices = %v, want the OpenRouter model voice", got)
	}
	if len(seen) != 1 {
		t.Fatalf("provider reads = %d, want 1", len(seen))
	}
	if got := seen[0].URL.Query().Get("output_modalities"); got != "speech" {
		t.Fatalf("output_modalities = %q, want speech", got)
	}
	// The list is public; the route's key has no business on this request.
	if auth := seen[0].Header.Get("Authorization"); auth != "" {
		t.Fatalf("Authorization = %q, want none", auth)
	}
}

func TestVoiceCatalogRouteRefusesEveryRouteThatIsNotOpenRouter(t *testing.T) {
	for _, tc := range []struct {
		name    string
		runtime *llm.Runtime
	}{
		{"llama.cpp", routeRuntime("llamacpp", "http://aura-llm:8084/v1")},
		{"ollama", routeRuntime("ollama", "http://host.docker.internal:11434/v1")},
		{"openrouter provider on a local host", routeRuntime("openrouter", "http://host.docker.internal:8084/v1")},
		{"no published route", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var seen []*http.Request
			route := voiceCatalogRoute{runtime: tc.runtime, client: voiceModelsServer(t, &seen)}
			models, err := route.List(context.Background(), "transcription")
			if !errors.Is(err, agui.ErrMediaCatalogLocalRoute) || models != nil {
				t.Fatalf("List = %v, %v, want the local-route refusal", models, err)
			}
			if len(seen) != 0 {
				t.Fatalf("a refused route still reached the provider %d times", len(seen))
			}
		})
	}
}

func TestNewVoiceCatalogRouteBoundsItsReads(t *testing.T) {
	route := newVoiceCatalogRoute(nil)
	if route.client == nil || route.client.Timeout != voiceCatalogTimeout {
		t.Fatalf("client = %+v, want one bounded by voiceCatalogTimeout", route.client)
	}
}
