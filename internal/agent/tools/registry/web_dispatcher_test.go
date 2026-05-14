package tools

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aura/aura/internal/llm"
)

func TestWebTool_Schema(t *testing.T) {
	tool := NewWebTool("http://searx.local")
	if tool.Name() != "web" {
		t.Fatalf("name = %q, want web", tool.Name())
	}
	params := tool.Parameters()
	required, _ := params["required"].([]string)
	if len(required) != 1 || required[0] != "action" {
		t.Fatalf("required = %v, want [action]", required)
	}
	props, _ := params["properties"].(map[string]any)
	action, _ := props["action"].(map[string]any)
	enum, _ := action["enum"].([]string)
	if len(enum) != 2 {
		t.Fatalf("action enum = %v, want [search fetch]", enum)
	}
}

func TestWebTool_MissingAction(t *testing.T) {
	tool := NewWebTool("http://searx.local")
	_, err := tool.Execute(t.Context(), map[string]any{})
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("missing action: err = %v, want ErrSchemaValidation", err)
	}
}

func TestWebTool_UnknownAction(t *testing.T) {
	tool := NewWebTool("http://searx.local")
	_, err := tool.Execute(t.Context(), map[string]any{"action": "scrape"})
	if !errors.Is(err, llm.ErrSchemaValidation) {
		t.Fatalf("unknown action: err = %v, want ErrSchemaValidation", err)
	}
}

func TestWebTool_SearchDispatches(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"results":[{"title":"R1","url":"https://example.com","content":"S1"}]}`)
	}))
	defer server.Close()

	tool := NewWebTool(server.URL)
	out, err := tool.Execute(t.Context(), map[string]any{
		"action": "search",
		"query":  "aura",
	})
	if err != nil {
		t.Fatalf("search: %v", err)
	}
	if !strings.Contains(out, "R1") || !strings.Contains(out, "https://example.com") {
		t.Fatalf("search output missing expected content: %q", out)
	}
}

func TestWebTool_FetchDispatches(t *testing.T) {
	t.Setenv("AURA_WEB_FETCH_ALLOW_LOOPBACK", "1")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<html><head><title>Dispatch OK</title></head><body><p>Body text</p></body></html>`)
	}))
	defer server.Close()

	tool := NewWebTool("")
	out, err := tool.Execute(t.Context(), map[string]any{
		"action": "fetch",
		"url":    server.URL,
	})
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if !strings.Contains(out, "Dispatch OK") || !strings.Contains(out, "Body text") {
		t.Fatalf("fetch output missing expected content: %q", out)
	}
}
