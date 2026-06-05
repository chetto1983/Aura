package skills

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// spike003Body mirrors the skills.sh /api/search shape (spike 003) WITH drift
// fields the lax decoder must ignore: a top-level `count` and a per-item
// `isDuplicate`. Results are intentionally out of install order so the test can
// assert the client ranks them.
const spike003Body = `{
  "query": "xlsx",
  "searchType": "fuzzy",
  "count": 3,
  "skills": [
    {"id":"openai/skills/spreadsheet","skillId":"spreadsheet","name":"spreadsheet","installs":500,"source":"openai","isDuplicate":false},
    {"id":"anthropics/skills/xlsx","skillId":"xlsx","name":"xlsx","installs":102055,"source":"anthropics","isDuplicate":false},
    {"id":"vercel-labs/skills/find-skills","skillId":"find-skills","name":"find-skills","installs":1861217,"source":"vercel-labs","isDuplicate":true}
  ]
}`

func TestCatalogSearch_LaxDecodeAndRanking(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/search" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		if r.URL.Query().Get("q") == "" {
			http.Error(w, "empty query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(spike003Body))
	}))
	defer srv.Close()

	c := NewCatalogClient(CatalogConfig{BaseURL: srv.URL}, srv.Client())
	items, err := c.Search(context.Background(), "xlsx")
	if err != nil {
		t.Fatalf("Search returned error despite unknown drift fields (lax decode must succeed): %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
	// Ranked by installs descending.
	wantOrder := []int{1861217, 102055, 500}
	for i, want := range wantOrder {
		if items[i].Installs != want {
			t.Fatalf("item[%d].Installs = %d, want %d (ranking by installs desc)", i, items[i].Installs, want)
		}
	}
	if items[0].ID != "vercel-labs/skills/find-skills" {
		t.Fatalf("top item ID = %q, want the highest-install handle", items[0].ID)
	}
}

func TestCatalogSearch_EmptyQueryGuardedPreCall(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()

	c := NewCatalogClient(CatalogConfig{BaseURL: srv.URL}, srv.Client())
	for _, q := range []string{"", "   ", "\t\n"} {
		if _, err := c.Search(context.Background(), q); !errors.Is(err, ErrEmptyQuery) {
			t.Fatalf("Search(%q) err = %v, want ErrEmptyQuery", q, err)
		}
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("server received %d requests for empty queries, want 0 (guard pre-call)", n)
	}
}

func TestCatalogSearch_Non2xxSurfacesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "upstream boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	c := NewCatalogClient(CatalogConfig{BaseURL: srv.URL}, srv.Client())
	if _, err := c.Search(context.Background(), "xlsx"); err == nil {
		t.Fatalf("non-2xx must surface an error")
	} else if !contains(err.Error(), "502") {
		t.Fatalf("error %q must carry the HTTP status", err.Error())
	}
}

func TestCatalogSearch_DisabledReturnsSentinel(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer srv.Close()

	c := NewCatalogClient(CatalogConfig{BaseURL: srv.URL, Disabled: true}, srv.Client())
	if _, err := c.Search(context.Background(), "xlsx"); !errors.Is(err, ErrCatalogDisabled) {
		t.Fatalf("disabled catalog err = %v, want ErrCatalogDisabled", err)
	}
	if n := atomic.LoadInt32(&hits); n != 0 {
		t.Fatalf("disabled catalog must not dial; server saw %d requests", n)
	}
}

func TestNewCatalogClient_Defaults(t *testing.T) {
	c := NewCatalogClient(CatalogConfig{}, nil)
	if c.baseURL != DefaultCatalogURL {
		t.Fatalf("default baseURL = %q, want %q", c.baseURL, DefaultCatalogURL)
	}
	if c.http == nil || c.http.Timeout == 0 {
		t.Fatalf("default client must have a non-zero timeout")
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
