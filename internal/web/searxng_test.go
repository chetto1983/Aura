//go:build !web_integration

package web

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/chetto1983/aura/internal/config"
)

// searxJSON builds a SearXNG-shaped JSON body from the given results so the tests
// drive the real parse path without a live instance.
func searxJSON(t *testing.T, results []map[string]any) string {
	t.Helper()
	b, err := json.Marshal(map[string]any{"query": "q", "results": results})
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	return string(b)
}

func testClient(t *testing.T, searxURL string) *Client {
	t.Helper()
	cfg := &config.Config{
		SearxngURL:           searxURL,
		WebDNSPinTTLSec:      60,
		WebFetchMaxBodyBytes: 5_000_000,
		WebSearchTimeoutSec:  20,
		WebFetchTimeoutSec:   30,
		WebUserAgent:         "Aura/test web",
	}
	return NewClient(cfg)
}

func TestSearch_ParseAndFilter(t *testing.T) {
	var gotQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.RawQuery
		_, _ = w.Write([]byte(searxJSON(t, []map[string]any{
			{"title": "Wiki", "url": "https://en.wikipedia.org/wiki/Go", "content": "go lang"},
			{"title": "Off", "url": "https://example.com/go", "content": "off domain"},
			{"title": "Sub", "url": "https://m.wikipedia.org/page", "content": "subdomain"},
		})))
	}))
	defer srv.Close()

	c := testClient(t, srv.URL)
	res, err := c.Search(context.Background(), SearchParams{Query: "golang", Domains: []string{"wikipedia.org"}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.Contains(gotQuery, "format=json") {
		t.Errorf("query missing format=json: %q", gotQuery)
	}
	vals, _ := url.ParseQuery(gotQuery)
	if !strings.Contains(vals.Get("q"), "site:wikipedia.org") {
		t.Errorf("query missing site:wikipedia.org: %q", vals.Get("q"))
	}
	if len(res) != 2 {
		t.Fatalf("want 2 on-domain results, got %d: %+v", len(res), res)
	}
	for _, r := range res {
		if strings.Contains(r.URL, "example.com") {
			t.Errorf("off-domain URL leaked: %s", r.URL)
		}
	}
}

func TestSearch_Unavailable(t *testing.T) {
	t.Run("not_configured", func(t *testing.T) {
		c := testClient(t, "")
		_, err := c.Search(context.Background(), SearchParams{Query: "x"})
		assertWebErr(t, err, CodeSearchUnavailable, ReasonSearxngNotConfigured)
	})
	t.Run("unreachable", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusBadGateway)
		}))
		srv.Close() // dead endpoint
		c := testClient(t, srv.URL)
		_, err := c.Search(context.Background(), SearchParams{Query: "x"})
		assertWebErr(t, err, CodeSearchUnavailable, ReasonSearxngUnreachable)
	})
}

func TestSearch_RetryPolicy(t *testing.T) {
	t.Run("retry_on_503", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			if calls.Add(1) == 1 {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			_, _ = w.Write([]byte(searxJSON(t, []map[string]any{{"title": "ok", "url": "https://ok.test/", "content": "c"}})))
		}))
		defer srv.Close()
		c := testClient(t, srv.URL)
		res, err := c.Search(context.Background(), SearchParams{Query: "x"})
		if err != nil {
			t.Fatalf("Search: %v", err)
		}
		if calls.Load() != 2 {
			t.Errorf("want 2 calls (1 retry), got %d", calls.Load())
		}
		if len(res) != 1 {
			t.Errorf("want 1 result, got %d", len(res))
		}
	})
	t.Run("no_retry_on_404", func(t *testing.T) {
		var calls atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			calls.Add(1)
			w.WriteHeader(http.StatusNotFound)
		}))
		defer srv.Close()
		c := testClient(t, srv.URL)
		_, err := c.Search(context.Background(), SearchParams{Query: "x"})
		if err == nil {
			t.Fatal("want error on 404")
		}
		if calls.Load() != 1 {
			t.Errorf("want 1 call (no retry on 404), got %d", calls.Load())
		}
	})
}

func TestSearch_CategoryEnum(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(searxJSON(t, nil)))
	}))
	defer srv.Close()
	c := testClient(t, srv.URL)

	for _, cat := range []string{"general", "news"} {
		if _, err := c.Search(context.Background(), SearchParams{Query: "x", Category: cat}); err != nil {
			t.Errorf("category %q should be accepted: %v", cat, err)
		}
	}
	_, err := c.Search(context.Background(), SearchParams{Query: "x", Category: "images"})
	if err == nil {
		t.Fatal("unknown category must be an error, not a panic")
	}
}

func TestSearch_Metadata(t *testing.T) {
	published := "2026-01-01T00:00:00Z"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(searxJSON(t, []map[string]any{
			{"title": "T", "url": "https://ok.test/a", "content": "c", "engine": "duckduckgo", "score": 1.5, "category": "general", "publishedDate": published, "thumbnail": "https://t.test/x.png"},
		})))
	}))
	defer srv.Close()
	c := testClient(t, srv.URL)

	plain, err := c.Search(context.Background(), SearchParams{Query: "x"})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if plain[0].Metadata != nil {
		t.Errorf("default must omit metadata, got %+v", plain[0].Metadata)
	}

	withMeta, err := c.Search(context.Background(), SearchParams{Query: "x", IncludeMetadata: true})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	m := withMeta[0].Metadata
	if m == nil {
		t.Fatal("IncludeMetadata must add metadata")
	}
	if m.PublishedAt == nil || *m.PublishedAt != published {
		t.Errorf("published_at not normalized from publishedDate: %+v", m.PublishedAt)
	}
	if m.Engine != "duckduckgo" || m.Score != 1.5 {
		t.Errorf("metadata not normalized: %+v", m)
	}
}

func TestValidHostname(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"wikipedia.org", "wikipedia.org"},
		{"WIKIPEDIA.ORG", "wikipedia.org"},
		{"  example.com  ", "example.com"},
		{"", ""},                    // empty
		{"https://example.com", ""}, // scheme (contains :/ )
		{"example.com/path", ""},    // path
		{"example.com:8080", ""},    // port
		{"example.com?q=1", ""},     // query
		{"two words.com", ""},       // space
	}
	for _, tt := range tests {
		if got := validHostname(tt.in); got != tt.want {
			t.Errorf("validHostname(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDomainAllowed(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		domains []string
		want    bool
	}{
		{"empty set allows all", "https://anything.test/x", nil, true},
		{"exact match", "https://wikipedia.org/Go", []string{"wikipedia.org"}, true},
		{"subdomain suffix match", "https://en.wikipedia.org/Go", []string{"wikipedia.org"}, true},
		{"non-match", "https://example.com/x", []string{"wikipedia.org"}, false},
		{"suffix-but-not-dot-boundary", "https://notwikipedia.org/x", []string{"wikipedia.org"}, false},
		{"case-insensitive host", "https://EN.WIKIPEDIA.ORG/x", []string{"wikipedia.org"}, true},
		{"empty domain entry skipped, no match", "https://example.com/x", []string{"", "wikipedia.org"}, false},
		{"unparseable URL dropped", "://bad url", []string{"wikipedia.org"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := domainAllowed(tt.rawURL, tt.domains); got != tt.want {
				t.Errorf("domainAllowed(%q, %v) = %v, want %v", tt.rawURL, tt.domains, got, tt.want)
			}
		})
	}
}

func TestBuildQuery(t *testing.T) {
	c := testClient(t, "http://unused.test")

	t.Run("multiple domains OR-joined + language + time_range", func(t *testing.T) {
		v := c.buildQuery(SearchParams{
			Query:     "golang",
			Language:  "en",
			TimeRange: "month",
			Domains:   []string{"go.dev", "wikipedia.org"},
		}, "news")
		q := v.Get("q")
		if !strings.Contains(q, "site:go.dev") || !strings.Contains(q, "site:wikipedia.org") {
			t.Errorf("q missing site clauses: %q", q)
		}
		if !strings.Contains(q, " OR ") {
			t.Errorf("multiple domains must be OR-joined: %q", q)
		}
		if v.Get("categories") != "news" {
			t.Errorf("categories = %q, want news", v.Get("categories"))
		}
		if v.Get("language") != "en" {
			t.Errorf("language = %q, want en", v.Get("language"))
		}
		if v.Get("time_range") != "month" {
			t.Errorf("time_range = %q, want month", v.Get("time_range"))
		}
		if v.Get("format") != "json" || v.Get("pageno") != "1" {
			t.Errorf("missing fixed params: format=%q pageno=%q", v.Get("format"), v.Get("pageno"))
		}
	})

	t.Run("week time_range is forwarded", func(t *testing.T) {
		v := c.buildQuery(SearchParams{Query: "x", TimeRange: "week"}, "general")
		if v.Get("time_range") != "week" {
			t.Errorf("time_range = %q, want week (newly accepted value)", v.Get("time_range"))
		}
	})

	t.Run("invalid time_range dropped, no site clause when no valid domain", func(t *testing.T) {
		v := c.buildQuery(SearchParams{
			Query:     "x",
			TimeRange: "decade",                       // not in validTimeRanges → dropped
			Domains:   []string{"https://bad.test/p"}, // scheme/path → rejected by validHostname
		}, "general")
		if v.Has("time_range") {
			t.Errorf("invalid time_range must be dropped, got %q", v.Get("time_range"))
		}
		if strings.Contains(v.Get("q"), "site:") {
			t.Errorf("no valid domain should yield no site clause: %q", v.Get("q"))
		}
		if v.Has("language") {
			t.Errorf("empty language must be omitted, got %q", v.Get("language"))
		}
	})
}

// TestConvIDFrom covers the no-convID branch: a ctx without a stamped convID yields
// the empty string (the pin cache then keys by "").
func TestConvIDFrom(t *testing.T) {
	if got := convIDFrom(context.Background()); got != "" {
		t.Errorf("convIDFrom(empty ctx) = %q, want \"\"", got)
	}
	if got := convIDFrom(withConvID(context.Background(), "conv-7")); got != "conv-7" {
		t.Errorf("convIDFrom(stamped ctx) = %q, want conv-7", got)
	}
}

func assertWebErr(t *testing.T, err error, code, reason string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want WebError %s/%s, got nil", code, reason)
	}
	we, ok := AsWebError(err)
	if !ok {
		t.Fatalf("want *WebError, got %T: %v", err, err)
	}
	if we.Code != code || we.Reason != reason {
		t.Errorf("want %s/%s, got %s/%s", code, reason, we.Code, we.Reason)
	}
}
