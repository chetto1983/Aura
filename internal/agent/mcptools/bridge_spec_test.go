package mcptools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

func TestBridge_Namespaced(t *testing.T) {
	srv := &fakeServer{defs: []mcp.ToolDef{{Name: "create_issue", Description: "Open an issue."}}}
	got, err := Bridge(context.Background(), "github", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	// Model-facing name is namespaced <ns>__<tool>.
	if got[0].Spec().Name != "github__create_issue" {
		t.Fatalf("model-facing name = %q, want github__create_issue", got[0].Spec().Name)
	}
}

func TestBridge_NullInputSchemaFallback(t *testing.T) {
	srv := &fakeServer{defs: []mcp.ToolDef{{Name: "ping", Description: "Ping.", InputSchema: json.RawMessage(`null`)}}}
	got, err := Bridge(context.Background(), "srv", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	if params := string(got[0].Spec().Parameters); params != `{"type":"object"}` {
		t.Fatalf("null inputSchema fallback = %s", params)
	}
}

// TestBridge_CapsArgSchemaDescriptions asserts that a server-controlled argument
// description inside inputSchema is capped, not forwarded raw into the loaded spec
// (B-15) — an unbounded arg description is an injection/flood surface.
func TestBridge_CapsArgSchemaDescriptions(t *testing.T) {
	longDesc := strings.Repeat("A", 5000) // far over the per-field cap
	schema := json.RawMessage(`{"type":"object","properties":{"q":{"type":"string","description":"` + longDesc + `"}}}`)
	srv := &fakeServer{defs: []mcp.ToolDef{{Name: "search", Description: "Search.", InputSchema: schema}}}
	got, err := Bridge(context.Background(), "srv", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	var parsed struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(got[0].Spec().Parameters, &parsed); err != nil {
		t.Fatalf("parameters not valid JSON after capping: %v", err)
	}
	desc := parsed.Properties["q"].Description
	if len(desc) == 0 {
		t.Fatal("arg description should be capped, not dropped")
	}
	if len(desc) > maxMCPArgDescBytes+len(mcpArgDescTruncated) {
		t.Errorf("arg description not capped: %d bytes (cap %d)", len(desc), maxMCPArgDescBytes)
	}
}
