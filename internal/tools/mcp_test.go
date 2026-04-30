package tools

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aura/aura/internal/mcp"
)

// newTestMCPServer spins up a minimal in-memory MCP HTTP server and a
// connected client. It echoes a single tool named "ping" that just returns
// the args back as a string.
func newTestMCPServer(t *testing.T, toolName string, schema string) *mcp.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var probe struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id,omitempty"`
		}
		body := make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		_ = json.Unmarshal(body, &probe)
		w.Header().Set("Content-Type", "application/json")
		switch probe.Method {
		case "initialize":
			w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(probe.ID) + `,"result":{"capabilities":{}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			toolJSON := `{"name":"` + toolName + `"`
			if schema != "" {
				toolJSON += `,"inputSchema":` + schema
			}
			toolJSON += `}`
			w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(probe.ID) + `,"result":{"tools":[` + toolJSON + `]}}`))
		case "tools/call":
			w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(probe.ID) + `,"result":{"content":[{"type":"text","text":"pong"}]}}`))
		}
	}))
	t.Cleanup(srv.Close)

	client, err := mcp.NewHTTPClient("srv1", srv.URL, nil)
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func TestMCPToolNameAndDescription(t *testing.T) {
	client := newTestMCPServer(t, "ping", `{"type":"object"}`)
	tool := NewMCPTool(client, "srv1", client.Tools()[0])

	if got, want := tool.Name(), "mcp_srv1_ping"; got != want {
		t.Errorf("Name = %q, want %q", got, want)
	}
	desc := tool.Description()
	if !strings.HasPrefix(desc, "[MCP: srv1] ") {
		t.Errorf("description missing MCP prefix: %q", desc)
	}
}

func TestMCPToolParametersPassThroughSchema(t *testing.T) {
	client := newTestMCPServer(t, "ping", `{"type":"object","properties":{"x":{"type":"string"}}}`)
	tool := NewMCPTool(client, "srv1", client.Tools()[0])
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Fatalf("expected type=object, got %v", params["type"])
	}
	props, ok := params["properties"].(map[string]any)
	if !ok || props["x"] == nil {
		t.Fatalf("expected x in properties, got %+v", params)
	}
}

func TestMCPToolParametersFallbackWhenSchemaMissing(t *testing.T) {
	client := newTestMCPServer(t, "ping", "")
	tool := NewMCPTool(client, "srv1", client.Tools()[0])
	params := tool.Parameters()
	if params["type"] != "object" {
		t.Fatalf("fallback schema missing type:object: %+v", params)
	}
	if _, ok := params["properties"].(map[string]any); !ok {
		t.Fatalf("fallback schema missing properties map: %+v", params)
	}
}

func TestMCPToolExecuteCallsServer(t *testing.T) {
	client := newTestMCPServer(t, "ping", `{"type":"object"}`)
	tool := NewMCPTool(client, "srv1", client.Tools()[0])
	out, err := tool.Execute(context.Background(), map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out != "pong" {
		t.Fatalf("Execute = %q, want pong", out)
	}
}

func TestMCPToolExecuteRejectsNilClient(t *testing.T) {
	tool := NewMCPTool(nil, "srv1", mcp.Tool{Name: "ping"})
	if _, err := tool.Execute(context.Background(), nil); err == nil {
		t.Fatal("expected error from nil client")
	}
}
