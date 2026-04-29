package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWebSearchToolExecute(t *testing.T) {
	var authHeader string
	var payload map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/web_search" {
			t.Fatalf("path = %q, want /web_search", r.URL.Path)
		}
		authHeader = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode payload: %v", err)
		}
		w.Write([]byte(`{"results":[{"title":"Aura","url":"https://example.com","content":"Result snippet"}]}`))
	}))
	defer server.Close()

	tool := NewWebSearchTool("secret", server.URL)
	if tool.Name() != "web_search" || tool.Description() == "" || tool.Parameters()["type"] != "object" {
		t.Fatal("web_search metadata is incomplete")
	}
	out, err := tool.Execute(t.Context(), map[string]any{"query": "aura", "max_results": float64(3)})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if authHeader != "Bearer secret" {
		t.Errorf("Authorization = %q, want Bearer secret", authHeader)
	}
	if payload["query"] != "aura" || payload["max_results"] != float64(3) {
		t.Errorf("payload = %#v", payload)
	}
	if !containsAll(out, "Web search results", "Aura", "https://example.com", "Result snippet") {
		t.Errorf("output missing expected content: %q", out)
	}
}

func TestWebFetchToolExecute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/web_fetch" {
			t.Fatalf("path = %q, want /web_fetch", r.URL.Path)
		}
		w.Write([]byte(`{"title":"Page","content":"Main content","links":["https://example.com/a"]}`))
	}))
	defer server.Close()

	tool := NewWebFetchTool("", server.URL)
	if tool.Name() != "web_fetch" || tool.Description() == "" || tool.Parameters()["type"] != "object" {
		t.Fatal("web_fetch metadata is incomplete")
	}
	out, err := tool.Execute(t.Context(), map[string]any{"url": "https://example.com"})
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !containsAll(out, "# Page", "Main content", "https://example.com/a") {
		t.Errorf("output missing expected content: %q", out)
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
