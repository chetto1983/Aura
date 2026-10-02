package chatgpt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/llm/openai_compat"
)

func TestAccountCatalog(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("Authorization") != "Bearer oauth-test-token" {
			t.Errorf("catalog request %s", r.URL.Path)
		}
		fmt.Fprint(w, `{"models":[{"slug":"z-model","display_name":"First choice","visibility":"list","context_window":64000},{"slug":"hidden","visibility":"hidden"},{"slug":"a-model","display_name":"Second choice","visibility":"list","context_length":32000},{"slug":"unknown-budget","visibility":"list"},{"slug":" ","visibility":"list"}]}`)
	})
	entries, err := fetchModels(context.Background(), client.httpClient, client.baseURL, "oauth-test-token")
	if err != nil || len(entries) != 3 {
		t.Fatalf("models %#v, error %v", entries, err)
	}
	if entries[0].ID != "z-model" || entries[0].DisplayName != "First choice" || entries[0].ContextWindow != 64000 || entries[1].ID != "a-model" || entries[1].ContextWindow != 32000 || entries[2].ContextWindow != 0 {
		t.Fatalf("models %#v", entries)
	}
	for _, entry := range entries {
		if entry.HasPrice {
			t.Fatal("catalog fabricated price")
		}
	}
}

func TestAccountCatalogAdvertisedReasoningLevels(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[{"slug":"thinking","visibility":"list","supported_reasoning_levels":[{"effort":"low"},{"effort":"medium"},{"effort":"high"},{"effort":"xhigh"},{"effort":"max"},{"effort":"ultra"},{"effort":"high"}]},{"slug":"optional","visibility":"list","supported_reasoning_levels":[{"effort":"none"},{"effort":"low"}]},{"slug":"unknown","visibility":"list","supported_reasoning_levels":[{"effort":"untrusted"}]},{"slug":"absent","visibility":"list"}]}`)
	})
	entries, err := fetchModels(context.Background(), client.httpClient, client.baseURL, "oauth-test-token")
	if err != nil || len(entries) != 4 {
		t.Fatalf("catalog %v, error %v", entries, err)
	}
	want := []llm.ReasoningEffort{llm.ReasoningEffortLow, llm.ReasoningEffortMedium, llm.ReasoningEffortHigh, llm.ReasoningEffortXHigh, llm.ReasoningEffortMax}
	if !slices.Equal(entries[0].SupportedReasoningEfforts, want) || !entries[0].ReasoningMandatory {
		t.Fatalf("thinking model %#v", entries[0])
	}
	if !slices.Equal(entries[1].SupportedReasoningEfforts, []llm.ReasoningEffort{llm.ReasoningEffortNone, llm.ReasoningEffortLow}) || entries[1].ReasoningMandatory {
		t.Fatalf("optional model %#v", entries[1])
	}
	for _, entry := range entries[2:] {
		if len(entry.SupportedReasoningEfforts) != 0 || entry.ReasoningMandatory {
			t.Fatalf("fabricated capability %#v", entry)
		}
	}
}

func TestCatalogSizeBound(t *testing.T) {
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"models":[],"extra":"`+strings.Repeat("x", maxCatalogBytes)+`"}`)
	})
	_, err := fetchModels(context.Background(), client.httpClient, client.baseURL, "oauth-test-token")
	if !errors.Is(err, llm.ErrModelCatalogUnavailable) || !strings.Contains(err.Error(), "size limit") {
		t.Fatalf("catalog size bound: %v", err)
	}
}

func TestCatalogFailures(t *testing.T) {
	for _, body := range []string{`{"data":[]}`, `{"models":null}`, `{"models":"invalid"}`, `broken`} {
		t.Run(body, func(t *testing.T) {
			client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, body) })
			_, err := fetchModels(context.Background(), client.httpClient, client.baseURL, "oauth-test-token")
			if !errors.Is(err, llm.ErrModelCatalogUnavailable) {
				t.Fatalf("error %v", err)
			}
		})
	}
	client := fixtureClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"expired oauth-test-token"}}`)
	})
	_, err := fetchModels(context.Background(), client.httpClient, client.baseURL, "oauth-test-token")
	httpErr, ok := errors.AsType[*openai_compat.HTTPError](err)
	if !errors.Is(err, llm.ErrModelCatalogUnavailable) || !ok || httpErr.StatusCode != 401 {
		t.Fatalf("error %v", err)
	}
	if _, err := FetchModels(context.Background(), nil); err == nil {
		t.Fatal("missing credential accepted")
	}
}
