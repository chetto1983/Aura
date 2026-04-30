package mcp

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestHTTPClientInitializeAndListTools(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		calls = append(calls, req.Method)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(rpcResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: json.RawMessage(`{"capabilities":{},"serverInfo":{"name":"test"}}`),
			})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_ = json.NewEncoder(w).Encode(rpcResponse{
				JSONRPC: "2.0", ID: req.ID,
				Result: json.RawMessage(`{"tools":[{"name":"echo","description":"echoes input","inputSchema":{"type":"object"}}]}`),
			})
		default:
			http.Error(w, "unknown method: "+req.Method, 400)
		}
	}))
	defer srv.Close()

	client, err := NewHTTPClient("test", srv.URL, nil)
	if err != nil {
		t.Fatalf("NewHTTPClient failed: %v", err)
	}
	defer func() { _ = client.Close() }()

	if client.Name() != "test" {
		t.Fatalf("expected name 'test', got %q", client.Name())
	}
	tools := client.Tools()
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}
	if tools[0].Name != "echo" || tools[0].Description != "echoes input" {
		t.Fatalf("unexpected tool: %+v", tools[0])
	}
	if len(calls) < 3 || calls[0] != "initialize" {
		t.Fatalf("RPC ordering wrong: %v", calls)
	}
}

func TestHTTPClientCallTool(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"capabilities":{}}`)})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"tools":[{"name":"greet"}]}`)})
		case "tools/call":
			b, _ := json.Marshal(req.Params)
			var p struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			_ = json.Unmarshal(b, &p)
			text := "hello " + p.Arguments["name"].(string)
			_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"content":[{"type":"text","text":"` + text + `"}]}`)})
		}
	}))
	defer srv.Close()

	client, err := NewHTTPClient("test", srv.URL, nil)
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	result, err := client.CallTool(context.Background(), "greet", map[string]any{"name": "world"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if result != "hello world" {
		t.Fatalf("expected 'hello world', got %q", result)
	}
}

func TestHTTPClientCallToolError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"capabilities":{}}`)})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"tools":[{"name":"fail"}]}`)})
		case "tools/call":
			_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"content":[{"type":"text","text":"bad thing"}],"isError":true}`)})
		}
	}))
	defer srv.Close()

	client, err := NewHTTPClient("test", srv.URL, nil)
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	_, err = client.CallTool(context.Background(), "fail", nil)
	if err == nil {
		t.Fatal("expected error from isError:true response")
	}
}

func TestHTTPClientSSEResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Content-Type", "text/event-stream")
			resp, _ := json.Marshal(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"capabilities":{}}`)})
			_, _ = w.Write([]byte("event: message\ndata: " + string(resp) + "\n\n"))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(`{"tools":[]}`)})
		}
	}))
	defer srv.Close()

	client, err := NewHTTPClient("sse-test", srv.URL, nil)
	if err != nil {
		t.Fatalf("NewHTTPClient: %v", err)
	}
	defer func() { _ = client.Close() }()

	if len(client.Tools()) != 0 {
		t.Fatalf("expected 0 tools, got %d", len(client.Tools()))
	}
}

func TestHTTPClientServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	if _, err := NewHTTPClient("err", srv.URL, nil); err == nil {
		t.Fatal("expected initialize to fail on HTTP 500")
	}
}

func TestLoadServersMissingFileReturnsEmpty(t *testing.T) {
	servers, err := LoadServers(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("expected nil err for missing file, got %v", err)
	}
	if len(servers) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(servers))
	}
}

func TestLoadServersEmptyPath(t *testing.T) {
	servers, err := LoadServers("")
	if err != nil {
		t.Fatalf("expected nil err, got %v", err)
	}
	if len(servers) != 0 {
		t.Fatalf("expected empty map, got %d entries", len(servers))
	}
}

func TestLoadServersValid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	body := `{
		"mcpServers": {
			"local": {"command": "node", "args": ["server.js"]},
			"remote": {"url": "https://example.com/mcp", "headers": {"Authorization": "Bearer x"}}
		}
	}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	servers, err := LoadServers(path)
	if err != nil {
		t.Fatalf("LoadServers: %v", err)
	}
	if len(servers) != 2 {
		t.Fatalf("want 2 servers, got %d", len(servers))
	}
	local, ok := servers["local"]
	if !ok || local.Command != "node" || len(local.Args) != 1 || local.Args[0] != "server.js" {
		t.Fatalf("unexpected local entry: %+v", local)
	}
	remote, ok := servers["remote"]
	if !ok || remote.URL != "https://example.com/mcp" || remote.Headers["Authorization"] != "Bearer x" {
		t.Fatalf("unexpected remote entry: %+v", remote)
	}
}

func TestLoadServersRejectsAmbiguousTransport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	body := `{"mcpServers": {"bad": {"command": "node", "url": "https://example.com"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadServers(path); err == nil {
		t.Fatal("expected error when both command and url set")
	}
}

func TestLoadServersRejectsMissingTransport(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	body := `{"mcpServers": {"empty": {}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadServers(path); err == nil {
		t.Fatal("expected error when neither command nor url set")
	}
}

func TestLoadServersRejectsBadName(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	body := `{"mcpServers": {"bad name!": {"command": "node"}}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadServers(path); err == nil {
		t.Fatal("expected error for invalid server name")
	}
}

func TestLoadServersRejectsUnknownField(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.json")
	body := `{"unknownTopLevel": {}}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadServers(path); err == nil {
		t.Fatal("expected error for unknown top-level field")
	}
}
