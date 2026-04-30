package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aura/aura/internal/mcp"
)

// mcpFakeServer spawns an in-memory MCP HTTP server that advertises one
// tool per call so the api/mcp endpoint has something to serialize.
func mcpFakeServer(t *testing.T, toolName, toolDesc string) *mcp.Client {
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
			tools, _ := json.Marshal([]mcp.Tool{{
				Name:        toolName,
				Description: toolDesc,
				InputSchema: map[string]any{"type": "object", "properties": map[string]any{"q": map[string]any{"type": "string"}}},
			}})
			w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(probe.ID) + `,"result":{"tools":` + string(tools) + `}}`))
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

func TestMCPServers_Empty(t *testing.T) {
	router := NewRouter(Deps{})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/mcp/servers", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []MCPServerSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("want empty list, got %+v", got)
	}
}

func TestMCPServers_ReturnsToolMetadata(t *testing.T) {
	c := mcpFakeServer(t, "search", "Search the index")
	router := NewRouter(Deps{MCP: []*mcp.Client{c}})

	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/mcp/servers", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []MCPServerSummary
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("want 1 server, got %d", len(got))
	}
	srv := got[0]
	if srv.Name != "srv1" {
		t.Errorf("name = %q, want srv1", srv.Name)
	}
	if srv.Transport != mcp.TransportHTTP {
		t.Errorf("transport = %q, want %q", srv.Transport, mcp.TransportHTTP)
	}
	if srv.ToolCount != 1 || len(srv.Tools) != 1 {
		t.Fatalf("expected 1 tool, got count=%d tools=%+v", srv.ToolCount, srv.Tools)
	}
	tool := srv.Tools[0]
	if tool.Name != "search" {
		t.Errorf("tool name = %q", tool.Name)
	}
	if tool.Description != "Search the index" {
		t.Errorf("tool desc = %q", tool.Description)
	}
	if tool.InputSchema == nil {
		t.Errorf("input_schema missing")
	}
}

func TestMCPServers_HandlesNilClient(t *testing.T) {
	router := NewRouter(Deps{MCP: []*mcp.Client{nil}})
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, httptest.NewRequest("GET", "/mcp/servers", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got []MCPServerSummary
	_ = json.Unmarshal(rr.Body.Bytes(), &got)
	if len(got) != 0 {
		t.Fatalf("nil client should be skipped, got %+v", got)
	}
}
