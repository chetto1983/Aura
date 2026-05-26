package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBuiltinToolsExposeExpandedAuraSurface(t *testing.T) {
	tools := builtinTools()
	seen := map[string]bool{}
	for _, tool := range tools {
		if strings.TrimSpace(tool.name) == "" {
			t.Fatal("tool with empty name")
		}
		if seen[tool.name] {
			t.Fatalf("duplicate tool name %q", tool.name)
		}
		seen[tool.name] = true
		if tool.handler == nil {
			t.Fatalf("tool %s has nil handler", tool.name)
		}
		if tool.inputSchema == nil {
			t.Fatalf("tool %s has nil input schema", tool.name)
		}
	}

	want := []string{
		"aura_wiki_search",
		"aura_wiki_graph",
		"aura_wiki_godnodes",
		"aura_wiki_path",
		"aura_wiki_reindex",
		"aura_source_upload",
		"aura_source_raw",
		"aura_source_delete",
		"aura_file_write",
		"aura_file_delete_many",
		"aura_task_upsert",
		"aura_skill_install",
		"aura_mcp_invoke",
		"aura_mcp_setup_mail_save",
		"aura_mcp_setup_database_save",
		"aura_tool_registry",
		"aura_tool_call",
		"aura_tool_compact",
		"aura_tool_attempts",
	}
	for _, name := range want {
		if !seen[name] {
			t.Fatalf("missing MCP tool %q", name)
		}
	}
	if got := len(tools); got < 52 {
		t.Fatalf("builtinTools length = %d, want expanded surface >= 52", got)
	}
}

func TestMCPInvokePassesNestedArgumentsOnly(t *testing.T) {
	var gotPath, gotAuth string
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		data, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &gotBody); err != nil {
			t.Fatalf("decode body: %v; body=%s", err, string(data))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"output":"4"}`))
	}))
	defer ts.Close()

	client := &auraClient{base: ts.URL, token: "secret", http: ts.Client()}
	out, err := handleMCPInvoke(context.Background(), client, []byte(`{
		"server":"calculator",
		"tool":"calculate",
		"arguments":{"expr":"2+2"}
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if gotPath != "/api/mcp/calculator/tools/calculate" {
		t.Fatalf("path = %s", gotPath)
	}
	if gotAuth != "Bearer secret" {
		t.Fatalf("authorization header = %q", gotAuth)
	}
	if gotBody["expr"] != "2+2" || len(gotBody) != 1 {
		t.Fatalf("body = %#v", gotBody)
	}
	if !strings.Contains(out, `"output": "4"`) {
		t.Fatalf("output missing tool result: %s", out)
	}
}

func TestSourceRawReturnsBase64Envelope(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/sources/src_1234567890abcdef/raw" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", `inline; filename="sample.pdf"`)
		_, _ = w.Write([]byte("abc"))
	}))
	defer ts.Close()

	client := &auraClient{base: ts.URL, token: "secret", http: ts.Client()}
	out, err := handleSourceRaw(context.Background(), client, []byte(`{"id":"src_1234567890abcdef"}`))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("decode output: %v; output=%s", err, out)
	}
	if got["content_type"] != "application/pdf" {
		t.Fatalf("content_type = %#v", got["content_type"])
	}
	if got["content_base64"] != "YWJj" {
		t.Fatalf("content_base64 = %#v", got["content_base64"])
	}
	if got["truncated"] != false {
		t.Fatalf("truncated = %#v", got["truncated"])
	}
}
