package tools

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearXNGSearchToolExecute(t *testing.T) {
	var gotAuth string
	var gotQuery string
	var gotFormat string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("path = %q, want /search", r.URL.Path)
		}
		gotAuth = r.Header.Get("Authorization")
		gotQuery = r.URL.Query().Get("q")
		gotFormat = r.URL.Query().Get("format")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[`)
		for i := 1; i <= 12; i++ {
			if i > 1 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"title":"Result %d","url":"https://example.com/%d","content":"Snippet %d"}`, i, i, i)
		}
		fmt.Fprint(w, `]}`)
	}))
	defer server.Close()

	tool := NewSearXNGSearchTool(server.URL)
	out, err := tool.Execute(t.Context(), map[string]any{"query": "aura", "max_results": float64(3)})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("Authorization header = %q, want empty", gotAuth)
	}
	if gotQuery != "aura" || gotFormat != "json" {
		t.Fatalf("query params q=%q format=%q", gotQuery, gotFormat)
	}
	if !containsAll(out, "Web search results", "Result 1", "https://example.com/1", "Snippet 3") {
		t.Fatalf("output missing expected content: %q", out)
	}
	if strings.Contains(out, "Result 4") {
		t.Fatalf("output was not capped to max_results: %q", out)
	}
}

func TestSearXNGSearchToolJSONDisabledError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"json format is disabled in settings.yml"}`, http.StatusForbidden)
	}))
	defer server.Close()

	tool := NewSearXNGSearchTool(server.URL)
	_, err := tool.Execute(t.Context(), map[string]any{"query": "aura"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !containsAll(err.Error(), "SearXNG returned HTTP 403", "JSON API may be disabled") {
		t.Fatalf("error = %q", err.Error())
	}
}

func TestDirectWebFetchToolRejectsNonHTTPURLs(t *testing.T) {
	tool := NewDirectWebFetchTool()
	for _, target := range []string{"file:///etc/passwd", "ftp://example.com/file", "mailto:test@example.com"} {
		if _, err := tool.Execute(t.Context(), map[string]any{"url": target}); err == nil {
			t.Fatalf("Execute(%q) returned nil error", target)
		}
	}
}

func TestDirectWebFetchToolExtractsHTML(t *testing.T) {
	t.Setenv("AURA_WEB_FETCH_ALLOW_LOOPBACK", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Page title</title><script>ignoreMe()</script></head><body><main><h1>Hello Aura</h1><p>Main content here.</p><a href="/next">Next</a></main></body></html>`)
	}))
	defer server.Close()

	tool := NewDirectWebFetchTool()
	out, err := tool.Execute(t.Context(), map[string]any{"url": server.URL})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !containsAll(out, "# Page title", "Hello Aura", "Main content here.", server.URL+"/next") {
		t.Fatalf("output missing expected content: %q", out)
	}
	if strings.Contains(out, "ignoreMe") {
		t.Fatalf("script text leaked into output: %q", out)
	}
}

func TestDirectWebFetchToolCapsLargePages(t *testing.T) {
	t.Setenv("AURA_WEB_FETCH_ALLOW_LOOPBACK", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte(strings.Repeat("x", maxDirectFetchResponseBytes+1024)))
	}))
	defer server.Close()

	tool := NewDirectWebFetchTool()
	out, err := tool.Execute(t.Context(), map[string]any{"url": server.URL})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !strings.Contains(out, "[truncated") {
		t.Fatalf("output should mention truncation: %q", out)
	}
	if len(out) > maxWebToolChars+64 {
		t.Fatalf("output len = %d, want bounded near %d", len(out), maxWebToolChars)
	}
}

func TestDirectWebFetchToolBlocksLoopbackByDefault(t *testing.T) {
	t.Setenv("AURA_WEB_FETCH_ALLOW_LOOPBACK", "")
	t.Setenv("AURA_WEB_FETCH_ALLOW_HOSTS", "")
	tool := NewDirectWebFetchTool()
	for _, target := range []string{
		"http://127.0.0.1/",
		"http://localhost:8088/",
		"http://169.254.169.254/latest/meta-data/",
		"http://10.0.0.1/",
		"http://192.168.1.1/",
		"http://[::1]/",
	} {
		_, err := tool.Execute(t.Context(), map[string]any{"url": target})
		if err == nil {
			t.Fatalf("Execute(%q) returned nil error; expected SSRF block", target)
		}
	}
}

func TestDirectWebFetchToolAllowHostsOverride(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "ok")
	}))
	defer server.Close()

	host, _, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatalf("split host: %v", err)
	}
	t.Setenv("AURA_WEB_FETCH_ALLOW_LOOPBACK", "")
	t.Setenv("AURA_WEB_FETCH_ALLOW_HOSTS", host)
	tool := NewDirectWebFetchTool()
	if _, err := tool.Execute(t.Context(), map[string]any{"url": server.URL}); err != nil {
		t.Fatalf("expected allowlisted fetch to succeed, got %v", err)
	}
}

func TestWebToolValidationHelpers(t *testing.T) {
	if _, err := requiredString(map[string]any{}, "query"); err == nil {
		t.Fatal("expected missing string error")
	}
	if got := intArg(map[string]any{"n": float64(99)}, "n", 5, 1, 10); got != 10 {
		t.Fatalf("intArg upper clamp = %d, want 10", got)
	}
	if got := intArg(map[string]any{"n": float64(-1)}, "n", 5, 1, 10); got != 1 {
		t.Fatalf("intArg lower clamp = %d, want 1", got)
	}
	if got := truncateForToolContext("abcdef", 3); got != "abc\n\n[truncated]" {
		t.Fatalf("truncateForToolContext() = %q", got)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}
