package rerank

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// wireRequest mirrors the JSON body the client POSTs to /v1/rerank so a stub can
// assert the on-the-wire shape (truncated documents, untruncated query, model).
type wireRequest struct {
	Model     string   `json:"model"`
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

// captureWarn swaps slog.Default for a buffer-backed WARN handler and returns the
// buffer plus a restore func. Used to assert the fail-soft single-warning contract.
func captureWarn(t *testing.T) (*bytes.Buffer, func()) {
	t.Helper()
	buf := &bytes.Buffer{}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	return buf, func() { slog.SetDefault(prev) }
}

// assertIdentity fails unless got is docs in input order, score 0, index == position.
func assertIdentity(t *testing.T, got []Scored, docs []string) {
	t.Helper()
	if len(got) != len(docs) {
		t.Fatalf("identity len = %d, want %d (%#v)", len(got), len(docs), got)
	}
	for i := range docs {
		if got[i].Index != i || got[i].Document != docs[i] ||
			got[i].Score != 0 || !got[i].Degraded {
			t.Fatalf(
				"identity[%d] = %#v, want {Index:%d Document:%q Score:0 Degraded:true}",
				i, got[i], i, docs[i],
			)
		}
	}
}

func TestRerankReordersByScoreDescending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			t.Errorf("path = %q, want /v1/rerank", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 2, "relevance_score": 0.9},
				{"index": 0, "relevance_score": 0.5},
				{"index": 1, "relevance_score": 0.1},
			},
		})
	}))
	defer srv.Close()

	docs := []string{"docA", "docB", "docC"}
	c := &RerankClient{BaseURL: srv.URL, Client: srv.Client()}
	got, err := c.Rerank(t.Context(), "torque", docs)
	if err != nil {
		t.Fatalf("Rerank err = %v, want nil", err)
	}
	want := []Scored{
		{Index: 2, Document: "docC", Score: 0.9},
		{Index: 0, Document: "docA", Score: 0.5},
		{Index: 1, Document: "docB", Score: 0.1},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d scored, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("scored[%d] = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestRerankEmptyDocsNoHTTPCall(t *testing.T) {
	called := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer srv.Close()

	c := &RerankClient{BaseURL: srv.URL, Client: srv.Client()}
	got, err := c.Rerank(t.Context(), "q", nil)
	if err != nil {
		t.Fatalf("Rerank err = %v, want nil", err)
	}
	if len(got) != 0 {
		t.Fatalf("empty docs scored = %#v, want empty", got)
	}
	if called {
		t.Fatal("server was called for an empty docs slice; want no HTTP call")
	}
}

func TestRerankEmptyBaseURLIdentity(t *testing.T) {
	warnOnce = sync.Once{}
	docs := []string{"a", "b", "c"}
	c := &RerankClient{}
	got, err := c.Rerank(t.Context(), "q", docs)
	if err != nil {
		t.Fatalf("Rerank err = %v, want nil (fail-soft)", err)
	}
	assertIdentity(t, got, docs)
}

func TestRerankHTTP503ReturnsIdentity(t *testing.T) {
	warnOnce = sync.Once{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	docs := []string{"a", "b"}
	c := &RerankClient{BaseURL: srv.URL, Client: srv.Client()}
	got, err := c.Rerank(t.Context(), "q", docs)
	if err != nil {
		t.Fatalf("Rerank err = %v, want nil (fail-soft)", err)
	}
	assertIdentity(t, got, docs)
}

func TestRerankMalformedJSONReturnsIdentity(t *testing.T) {
	warnOnce = sync.Once{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "this is not json{{{")
	}))
	defer srv.Close()

	docs := []string{"a", "b", "c"}
	c := &RerankClient{BaseURL: srv.URL, Client: srv.Client()}
	got, err := c.Rerank(t.Context(), "q", docs)
	if err != nil {
		t.Fatalf("Rerank err = %v, want nil (fail-soft)", err)
	}
	assertIdentity(t, got, docs)
}

func TestRerankLengthMismatchReturnsIdentity(t *testing.T) {
	warnOnce = sync.Once{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"index": 0, "relevance_score": 0.9}},
		})
	}))
	defer srv.Close()

	docs := []string{"a", "b", "c"}
	c := &RerankClient{BaseURL: srv.URL, Client: srv.Client()}
	got, err := c.Rerank(t.Context(), "q", docs)
	if err != nil {
		t.Fatalf("Rerank err = %v, want nil (fail-soft)", err)
	}
	assertIdentity(t, got, docs)
}

func TestRerankOutOfRangeIndexReturnsIdentity(t *testing.T) {
	warnOnce = sync.Once{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": 99, "relevance_score": 0.9},
				{"index": 0, "relevance_score": 0.5},
			},
		})
	}))
	defer srv.Close()

	docs := []string{"a", "b"}
	c := &RerankClient{BaseURL: srv.URL, Client: srv.Client()}
	got, err := c.Rerank(t.Context(), "q", docs)
	if err != nil {
		t.Fatalf("Rerank err = %v, want nil (fail-soft)", err)
	}
	assertIdentity(t, got, docs)
}

func TestRerankNegativeIndexReturnsIdentity(t *testing.T) {
	warnOnce = sync.Once{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"index": -1, "relevance_score": 0.9},
				{"index": 0, "relevance_score": 0.5},
			},
		})
	}))
	defer srv.Close()

	docs := []string{"a", "b"}
	c := &RerankClient{BaseURL: srv.URL, Client: srv.Client()}
	got, err := c.Rerank(t.Context(), "q", docs)
	if err != nil {
		t.Fatalf("Rerank err = %v, want nil (fail-soft)", err)
	}
	assertIdentity(t, got, docs)
}

func TestRerankTruncatesWireBodyKeepsOriginalDocument(t *testing.T) {
	longDoc := strings.Repeat("x", 600)
	var captured wireRequest
	var authHeader string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&captured); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"index": 0, "relevance_score": 0.7}},
		})
	}))
	defer srv.Close()

	docs := []string{longDoc}
	c := &RerankClient{BaseURL: srv.URL, Client: srv.Client(), Model: "custom-rerank", APIKey: "cloud-key"}
	got, err := c.Rerank(t.Context(), "query", docs)
	if err != nil {
		t.Fatalf("Rerank err = %v, want nil", err)
	}
	if authHeader != "Bearer cloud-key" {
		t.Fatalf("Authorization header = %q, want bearer token", authHeader)
	}
	if len(captured.Documents) != 1 {
		t.Fatalf("wire documents = %d, want 1", len(captured.Documents))
	}
	if n := len([]rune(captured.Documents[0])); n != maxRerankDocChars {
		t.Fatalf("wire document len = %d runes, want %d (truncated)", n, maxRerankDocChars)
	}
	if captured.Query != "query" {
		t.Fatalf("wire query = %q, want untruncated %q", captured.Query, "query")
	}
	if captured.Model != "custom-rerank" {
		t.Fatalf("wire model = %q, want %q", captured.Model, "custom-rerank")
	}
	if len(got) != 1 || got[0].Document != longDoc {
		t.Fatalf("returned Document must be the ORIGINAL untruncated text; got len %d", len(got[0].Document))
	}
}

func TestRerankWarnsOncePerProcessWithoutLeakingDocText(t *testing.T) {
	warnOnce = sync.Once{}
	buf, restore := captureWarn(t)
	defer restore()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	const secret = "SUPERSECRETTOKEN-do-not-log"
	docs := []string{secret, "b"}
	c := &RerankClient{BaseURL: srv.URL, Client: srv.Client()}
	for range 3 {
		if _, err := c.Rerank(t.Context(), "q", docs); err != nil {
			t.Fatalf("Rerank err = %v, want nil", err)
		}
	}
	if n := strings.Count(buf.String(), "level=WARN"); n != 1 {
		t.Fatalf("warnings = %d, want exactly 1 per process: %q", n, buf.String())
	}
	if strings.Contains(buf.String(), secret) {
		t.Fatalf("log leaked document text: %q", buf.String())
	}
}

func TestIdentityHelper(t *testing.T) {
	if got := identity(nil); len(got) != 0 {
		t.Fatalf("identity(nil) = %#v, want empty", got)
	}
	docs := []string{"x", "y"}
	assertIdentity(t, identity(docs), docs)
}
