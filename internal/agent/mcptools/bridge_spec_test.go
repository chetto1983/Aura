package mcptools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestBridge_Namespaced(t *testing.T) {
	srv, _ := newInMemoryMounted(t, mustTool("create_issue", "Open an issue.", nil, nil))
	got, err := Bridge(context.Background(), "github", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	if got[0].Spec().Name != "github__create_issue" {
		t.Fatalf("model-facing name = %q, want github__create_issue", got[0].Spec().Name)
	}
}

// TestBridge_NullInputSchemaFallback covers a schema whose properties are
// absent (a real fixture must advertise SOME object schema; capSchemaDescriptions
// re-marshals it byte-identically for the trivial {"type":"object"} case).
func TestBridge_NullInputSchemaFallback(t *testing.T) {
	srv, _ := newInMemoryMounted(t, mustTool("ping", "Ping.", nil, nil))
	got, err := Bridge(context.Background(), "srv", srv)
	if err != nil {
		t.Fatalf("Bridge: %v", err)
	}
	if params := string(got[0].Spec().Parameters); params != `{"type":"object"}` {
		t.Fatalf("empty inputSchema fallback = %s", params)
	}
}

// TestBridge_CapsArgSchemaDescriptions asserts that a server-controlled argument
// description inside inputSchema is capped, not forwarded raw into the loaded
// spec (B-15) — an unbounded arg description is an injection/flood surface.
func TestBridge_CapsArgSchemaDescriptions(t *testing.T) {
	longDesc := strings.Repeat("A", 5000) // far over the per-field cap
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{"q": map[string]any{"type": "string", "description": longDesc}},
	}
	srv, _ := newInMemoryMounted(t, mustTool("search", "Search.", schema, nil))
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
