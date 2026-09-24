package main

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Final review #2: the model reaches this tool through tool_search, and so does any MCP
// client. Its text used to prescribe `all` after an embedding-model change; followed while
// this server still runs on the old route, that clears every fact vector and re-embeds it in
// the OLD space, and the daemon's pass then does it all again. A route change needs no call,
// and the tool must say so where a caller reads it.
func TestMemoryReembedSaysARouteChangeNeedsNoCall(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "reembed-text", Version: "1"}, nil)
	addMemoryReembedTool(server, nil)
	session := inMemoryIdentityServer(t, server)
	listed, err := session.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	for _, tool := range listed.Tools {
		if tool.Name != "memory_reembed" {
			continue
		}
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			t.Fatalf("marshal schema: %v", err)
		}
		for where, text := range map[string]string{"description": tool.Description, "all": string(schema)} {
			if !strings.Contains(text, "needs no call") {
				t.Errorf("%s = %q, want it to say a model or route change needs no call", where, text)
			}
		}
		return
	}
	t.Fatal("memory_reembed is not advertised")
}
