package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// roundTripFunc lets a test see the request its client built before a fake answers it.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

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

// The speech and embedding clients run on OpenRouter whatever route the chat runs on, so the
// pickers list from OpenRouter with no view of the chat route at all.
func TestVoiceCatalogRouteListsTheModalityFromOpenRouter(t *testing.T) {
	var seen []*http.Request
	var hosts []string
	fake := voiceModelsServer(t, &seen)
	route := modalityCatalogRoute{client: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		hosts = append(hosts, r.URL.Host)
		return fake.Transport.RoundTrip(r)
	})}}

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
	if len(seen) != 1 || len(hosts) != 1 || hosts[0] != "openrouter.ai" {
		t.Fatalf("provider reads = %d to %v, want 1 to openrouter.ai", len(seen), hosts)
	}
	if got := seen[0].URL.Query().Get("output_modalities"); got != "speech" {
		t.Fatalf("output_modalities = %q, want speech", got)
	}
	// The list is public; the route's key has no business on this request.
	if auth := seen[0].Header.Get("Authorization"); auth != "" {
		t.Fatalf("Authorization = %q, want none", auth)
	}
}

func TestNewVoiceCatalogRouteBoundsItsReads(t *testing.T) {
	route := newModalityCatalogRoute()
	if route.client == nil || route.client.Timeout != modalityCatalogTimeout {
		t.Fatalf("client = %+v, want one bounded by modalityCatalogTimeout", route.client)
	}
}
