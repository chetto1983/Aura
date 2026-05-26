package main

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	tools "github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/mcp"
)

func TestMCPRuntimeRefreshSyncsRegistry(t *testing.T) {
	state := &runtimeMCPServerState{
		tools: []mcp.Tool{{Name: "one", Description: "first"}},
	}
	srv := newRuntimeMCPServer(t, state)
	reg := tools.NewRegistry(nil)
	rt := newMCPServerRuntime("srv", mcp.ServerConfig{URL: srv.URL}, reg, slog.Default())
	if err := rt.reconnect(context.Background()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	defer func() { _ = rt.Close() }()

	if _, ok := reg.DefinitionFor("mcp_srv_one"); !ok {
		t.Fatal("initial tool was not registered")
	}

	state.setTools([]mcp.Tool{{Name: "two", Description: "second"}})
	if err := rt.refreshAndSync(context.Background()); err != nil {
		t.Fatalf("refreshAndSync: %v", err)
	}

	if _, ok := reg.DefinitionFor("mcp_srv_one"); ok {
		t.Fatal("stale tool remained in registry")
	}
	if _, ok := reg.DefinitionFor("mcp_srv_two"); !ok {
		t.Fatal("new tool was not registered")
	}
}

func TestMCPRuntimeCircuitBreakerHidesToolsAndReconnectRestoresThem(t *testing.T) {
	state := &runtimeMCPServerState{
		tools: []mcp.Tool{{Name: "one", Description: "first"}},
	}
	srv := newRuntimeMCPServer(t, state)
	reg := tools.NewRegistry(nil)
	rt := newMCPServerRuntime("srv", mcp.ServerConfig{URL: srv.URL}, reg, slog.Default())
	if err := rt.reconnect(context.Background()); err != nil {
		t.Fatalf("reconnect: %v", err)
	}
	defer func() { _ = rt.Close() }()

	state.setFailCalls(true)
	for i := 0; i < mcpCircuitFailureThreshold; i++ {
		if _, err := rt.CallTool(context.Background(), "one", nil); err == nil {
			t.Fatalf("CallTool attempt %d succeeded, want failure", i+1)
		}
	}
	if _, ok := reg.DefinitionFor("mcp_srv_one"); ok {
		t.Fatal("circuit-open tool remained visible in manifest")
	}
	if got := len(rt.Tools()); got != 0 {
		t.Fatalf("runtime Tools() length = %d, want 0 while circuit open", got)
	}

	state.setFailCalls(false)
	if err := rt.reconnect(context.Background()); err != nil {
		t.Fatalf("reconnect after failure: %v", err)
	}
	if _, ok := reg.DefinitionFor("mcp_srv_one"); !ok {
		t.Fatal("reconnected tool was not restored")
	}
}

func TestMCPRuntimeRefreshDueThrottlesToolList(t *testing.T) {
	rt := newMCPServerRuntime("srv", mcp.ServerConfig{URL: "http://example.test"}, nil, slog.Default())
	now := time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)

	if !rt.refreshDue(now) {
		t.Fatal("fresh runtime should refresh when no successful list is recorded")
	}
	rt.markToolRefresh(now)
	if rt.refreshDue(now.Add(mcpToolRefreshInterval - time.Second)) {
		t.Fatal("refresh became due before the throttling interval elapsed")
	}
	if !rt.refreshDue(now.Add(mcpToolRefreshInterval)) {
		t.Fatal("refresh should become due at the throttling interval")
	}
}

type runtimeMCPServerState struct {
	mu        sync.Mutex
	tools     []mcp.Tool
	failCalls bool
}

func (s *runtimeMCPServerState) setTools(tools []mcp.Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools = append([]mcp.Tool(nil), tools...)
}

func (s *runtimeMCPServerState) setFailCalls(fail bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.failCalls = fail
}

func (s *runtimeMCPServerState) snapshot() ([]mcp.Tool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]mcp.Tool(nil), s.tools...), s.failCalls
}

func newRuntimeMCPServer(t *testing.T, state *runtimeMCPServerState) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID     json.RawMessage `json:"id,omitempty"`
			Method string          `json:"method"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "initialize":
			writeRuntimeMCPResult(w, req.ID, map[string]any{"capabilities": map[string]any{}})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			tools, _ := state.snapshot()
			writeRuntimeMCPResult(w, req.ID, map[string]any{"tools": tools})
		case "tools/call":
			_, failCalls := state.snapshot()
			if failCalls {
				http.Error(w, "call failed", http.StatusInternalServerError)
				return
			}
			writeRuntimeMCPResult(w, req.ID, map[string]any{
				"content": []map[string]string{{"type": "text", "text": "ok"}},
			})
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeRuntimeMCPResult(w http.ResponseWriter, id json.RawMessage, result any) {
	payload, _ := json.Marshal(result)
	_ = json.NewEncoder(w).Encode(struct {
		JSONRPC string          `json:"jsonrpc"`
		ID      json.RawMessage `json:"id,omitempty"`
		Result  json.RawMessage `json:"result"`
	}{
		JSONRPC: "2.0",
		ID:      id,
		Result:  payload,
	})
}
