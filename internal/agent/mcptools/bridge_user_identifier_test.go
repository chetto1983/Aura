package mcptools

import (
	"encoding/json"
	"testing"
)

// TestAcceptsUserIdentifier pins the rule that replaced a hand-kept name list. The list had
// omitted memory_update when that verb shipped in the vendored sidecar, so the bridge forwarded
// the call with no user_identifier; the server treats a missing identifier as "no scope" on its
// write verbs and refused the operator's own entity with "not found or not owned by this user".
func TestAcceptsUserIdentifier(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		parameters string
		want       bool
	}{
		{
			name:       "declared",
			parameters: `{"type":"object","properties":{"node_id":{"type":"string"},"user_identifier":{"type":"string"}}}`,
			want:       true,
		},
		{
			name:       "not declared",
			parameters: `{"type":"object","properties":{"query":{"type":"string"}}}`,
			want:       false,
		},
		{
			// The memory server is fail-OPEN: an unscoped call returns every tenant's memory.
			// So an unreadable schema must scope the call, not skip scoping it.
			name:       "no properties at all",
			parameters: `{"type":"object"}`,
			want:       true,
		},
		{
			name:       "unparseable",
			parameters: `not json`,
			want:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := acceptsUserIdentifier(json.RawMessage(tt.parameters)); got != tt.want {
				t.Fatalf("acceptsUserIdentifier(%s) = %v, want %v", tt.parameters, got, tt.want)
			}
		})
	}
}

// TestMemoryUpdateSchemaIsScoped is the regression the live failure earned: the exact schema the
// vendored sidecar advertises for memory_update must make the bridge inject the identity.
func TestMemoryUpdateSchemaIsScoped(t *testing.T) {
	t.Parallel()

	// Abridged from the FastMCP-generated schema for mcp/_tools.py::memory_update.
	const memoryUpdateSchema = `{
		"type":"object",
		"properties":{
			"node_type":{"type":"string"},
			"node_id":{"type":"string"},
			"name":{"anyOf":[{"type":"string"},{"type":"null"}]},
			"user_identifier":{"anyOf":[{"type":"string"},{"type":"null"}]}
		},
		"required":["node_type","node_id"]
	}`

	if !acceptsUserIdentifier(json.RawMessage(memoryUpdateSchema)) {
		t.Fatal("memory_update must be scoped to the caller; an unscoped update refuses the caller's own data")
	}
}
