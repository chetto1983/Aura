package main

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The quoting rule reaches the model where it writes the query: memory_recall and memory_search say,
// in their query parameter, that a quoted name ranks first (spec
// 2026-10-25-document-exactness-and-judge, decision 1, "Memory"). memory_recall's description is
// held under its 768-byte cap, so the parameter is where the rule lives.
func TestMemoryQueriesTellTheAgentToQuoteVerbatimNames(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "exact-description", Version: "1"}, nil)
	addMemoryRecallTool(server, nil)
	addMemorySearchTool(server, nil)
	session := inMemoryIdentityServer(t, server)
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(listed.Tools) != 2 {
		t.Fatalf("tools = %d, want memory_recall and memory_search", len(listed.Tools))
	}
	for _, tool := range listed.Tools {
		schema, ok := tool.InputSchema.(map[string]any)
		if !ok {
			t.Fatalf("%s input schema = %T", tool.Name, tool.InputSchema)
		}
		query := schemaMap(t, schemaMap(t, schema, "properties"), "query")
		if description, _ := query["description"].(string); !strings.Contains(description, "Quote a name or phrase that must appear verbatim") {
			t.Fatalf("%s query = %q, want the quoting rule", tool.Name, description)
		}
	}
}
