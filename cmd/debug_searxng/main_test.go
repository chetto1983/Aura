package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchBuildsSearXNGJSONRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/search" {
			t.Fatalf("path = %q, want /search", r.URL.Path)
		}
		if got := r.URL.Query().Get("q"); got != "aura agent" {
			t.Fatalf("q = %q, want aura agent", got)
		}
		if got := r.URL.Query().Get("format"); got != "json" {
			t.Fatalf("format = %q, want json", got)
		}
		_ = json.NewEncoder(w).Encode(searxngResponse{
			Results: []searxngResult{
				{Title: "Aura", URL: "https://example.com/aura", Content: "Second brain assistant."},
			},
		})
	}))
	defer server.Close()

	rep, elapsed, err := search(context.Background(), server.URL, "aura agent")
	if err != nil {
		t.Fatalf("search returned error: %v", err)
	}
	if elapsed <= 0 {
		t.Fatalf("elapsed should be positive, got %s", elapsed)
	}
	if len(rep.Results) != 1 || rep.Results[0].Title != "Aura" {
		t.Fatalf("unexpected results: %+v", rep.Results)
	}
}

func TestFormatResults(t *testing.T) {
	out := formatResults("aura agent", searxngResponse{
		Results: []searxngResult{
			{Title: "Aura", URL: "https://example.com/aura", Content: "Second brain assistant."},
			{Title: "Docs", URL: "https://example.com/docs", Content: "Reference material."},
		},
	})

	for _, want := range []string{
		`SearXNG results for "aura agent":`,
		"1. Aura",
		"URL: https://example.com/aura",
		"Second brain assistant.",
		"2. Docs",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("formatted output missing %q:\n%s", want, out)
		}
	}
}

func TestFormatResultsEmpty(t *testing.T) {
	out := formatResults("missing", searxngResponse{})
	if !strings.Contains(out, "No results found.") {
		t.Fatalf("empty output should explain no results, got:\n%s", out)
	}
}
