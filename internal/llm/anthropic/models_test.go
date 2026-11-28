package anthropic

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// modelJSON is one /v1/models row in the shape spec §3 measured on 2026-11-27.
func modelJSON(id, lifecycle string, efforts map[string]bool, disabled bool) string {
	effort := func(level string) string { return fmt.Sprintf(`"%s":{"supported":%t}`, level, efforts[level]) }
	return fmt.Sprintf(`{"type":"model","id":%q,"display_name":%q,"lifecycle":%q,"max_input_tokens":1000000,"max_tokens":128000,`+
		`"capabilities":{"effort":{"supported":%t,%s,%s,%s,%s,%s},`+
		`"thinking":{"supported":true,"types":{"enabled":{"supported":false},"adaptive":{"supported":true},"disabled":{"supported":%t},"between_tools":{"supported":false}}},`+
		`"image_input":{"supported":true},"pdf_input":{"supported":true},"structured_outputs":{"supported":true}}}`,
		id, strings.ToUpper(id[:1])+id[1:], lifecycle, len(efforts) > 0,
		effort("low"), effort("medium"), effort("high"), effort("xhigh"), effort("max"), disabled)
}

func catalogFixture(t *testing.T, handler http.HandlerFunc) (*http.Client, string) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	hc := server.Client()
	transport := hc.Transport.(*http.Transport).Clone()
	transport.DisableKeepAlives = true
	hc.Transport = transport
	t.Cleanup(hc.CloseIdleConnections)
	return hc, server.URL
}

func TestFetchModelsReadsCapabilities(t *testing.T) {
	all := map[string]bool{"low": true, "medium": true, "high": true, "xhigh": true, "max": true}
	pages := []string{
		`{"data":[` + modelJSON("claude-opus-5-5", "active", all, false) + `,` +
			modelJSON("claude-sonnet-5-5", "active", all, false) + `,` +
			modelJSON("claude-old-4-1", "deprecated", all, true) + `],"has_more":true,"first_id":"claude-opus-5-5","last_id":"claude-old-4-1"}`,
		`{"data":[` + modelJSON("claude-haiku-5-5", "active", map[string]bool{"low": true, "medium": true, "high": true}, true) + `,` +
			modelJSON("claude-fable-5-2", "active", all, false) + `],"has_more":false,"first_id":"claude-haiku-5-5","last_id":"claude-fable-5-2"}`,
	}
	var calls atomic.Int32
	hc, url := catalogFixture(t, func(w http.ResponseWriter, r *http.Request) {
		call := calls.Add(1)
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer "+houseKey || r.Header.Get("X-Api-Key") != "" {
			t.Errorf("catalog request %s with auth %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		if got := r.URL.Query()["lifecycle[]"]; !slices.Equal(got, []string{"active"}) {
			t.Errorf("lifecycle filter %v", r.URL.Query())
		}
		if call == 2 && r.URL.Query().Get("after_id") != "claude-old-4-1" {
			t.Errorf("second page cursor %v", r.URL.Query())
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, pages[min(int(call), len(pages))-1])
	})
	entries, err := fetchModels(context.Background(), staticKey(houseKey), hc, url)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("pages fetched %d, want 2", calls.Load())
	}
	ids := make([]string, 0, len(entries))
	for _, entry := range entries {
		ids = append(ids, entry.ID)
	}
	if !slices.Equal(ids, []string{"claude-opus-5-5", "claude-sonnet-5-5", "claude-haiku-5-5", "claude-fable-5-2"}) {
		t.Fatalf("models %v: the deprecated one must be dropped, the order kept", ids)
	}
	every := []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh, llm.ReasoningEffortMax}
	for _, entry := range entries {
		if entry.ContextWindow != 1_000_000 || entry.MaxOutputTokens != 128_000 || entry.DisplayName == "" || entry.HasPrice {
			t.Fatalf("limits %#v", entry)
		}
		want, mandatory := every, true
		if entry.ID == "claude-haiku-5-5" {
			want, mandatory = every[:3], false
		}
		if !slices.Equal(entry.SupportedReasoningEfforts, want) || entry.ReasoningMandatory != mandatory {
			t.Fatalf("%s reasoning %v mandatory %v", entry.ID, entry.SupportedReasoningEfforts, entry.ReasoningMandatory)
		}
	}
}

func TestFetchModelsWithoutEffortsDeclaresNone(t *testing.T) {
	hc, url := catalogFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[`+modelJSON("claude-plain-5-0", "active", nil, true)+`],"has_more":false}`)
	})
	entries, err := fetchModels(context.Background(), staticKey(houseKey), hc, url)
	if err != nil || len(entries) != 1 || entries[0].SupportedReasoningEfforts != nil || entries[0].ReasoningMandatory {
		t.Fatalf("entries %#v, error %v", entries, err)
	}
}

func TestFetchModelsWithoutAKeyNeverDials(t *testing.T) {
	var hits atomic.Int32
	hc, url := catalogFixture(t, func(http.ResponseWriter, *http.Request) { hits.Add(1) })
	for _, key := range []KeySource{nil, staticKey("  ")} {
		if _, err := fetchModels(context.Background(), key, hc, url); !errors.Is(err, ErrNoKey) {
			t.Fatalf("error %v, want ErrNoKey", err)
		}
	}
	if _, err := FetchModels(context.Background(), staticKey("")); !errors.Is(err, ErrNoKey) {
		t.Fatalf("FetchModels error %v, want ErrNoKey", err)
	}
	if hits.Load() != 0 {
		t.Fatalf("%d requests without a key", hits.Load())
	}
}

func TestFetchModelsFailuresKeepTheKeyOut(t *testing.T) {
	hc, url := catalogFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprintf(w, `{"type":"error","error":{"type":"authentication_error","message":"invalid x-api-key %s"}}`, houseKey)
	})
	_, err := fetchModels(context.Background(), staticKey(houseKey), hc, url)
	if !errors.Is(err, llm.ErrModelCatalogUnavailable) || strings.Contains(err.Error(), houseKey) || !strings.Contains(err.Error(), "Settings") {
		t.Fatalf("error %v", err)
	}
}

func TestFetchModelsBoundsTheCatalog(t *testing.T) {
	hc, url := catalogFixture(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[],"padding":"`+strings.Repeat("x", maxCatalogBytes)+`"}`)
	})
	_, err := fetchModels(context.Background(), staticKey(houseKey), hc, url)
	var tooLarge *http.MaxBytesError
	if !errors.Is(err, llm.ErrModelCatalogUnavailable) || !errors.As(err, &tooLarge) {
		t.Fatalf("error %v, want the size bound", err)
	}
}

func TestOpenRouterIDMapping(t *testing.T) {
	for model, want := range map[string]string{
		"claude-opus-5-5":            "anthropic/claude-opus-5.5",
		"claude-sonnet-5-5":          "anthropic/claude-sonnet-5.5",
		"claude-haiku-5-5":           "anthropic/claude-haiku-5.5",
		"claude-fable-5-2":           "anthropic/claude-fable-5.2",
		" claude-opus-5-5 ":          "anthropic/claude-opus-5.5",
		"claude-sonnet-4-5-20250929": "anthropic/claude-sonnet-4-5-20250929",
		"claude-mythos":              "anthropic/claude-mythos",
	} {
		if got := OpenRouterID(model); got != want {
			t.Errorf("OpenRouterID(%q) = %q, want %q", model, got, want)
		}
	}
}
