package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/mcp"
)

// recordingMemoryMCPServer is a streamable-HTTP MCP fake (modeled on
// managed_mount_test.go's httpMCPServer) that completes initialize/tools/list and,
// for tools/call, records the requested RAW tool name and returns a deterministic
// text result. It is the unit-tier stand-in for the live agent-memory sidecar — the
// real seed/read + reasoning-trace round-trip lives in 15-04's memory_integration tier.
type recordingMemoryMCPServer struct {
	*httptest.Server
	mu        sync.Mutex
	lastTool  string
	lastArgs  map[string]any
	cannedTxt string
}

func newRecordingMemoryMCPServer(t *testing.T) *recordingMemoryMCPServer {
	t.Helper()
	rec := &recordingMemoryMCPServer{cannedTxt: "canned-memory-result"}
	rec.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusOK)
			return
		}
		var req struct {
			ID     *int64          `json:"id"`
			Method string          `json:"method"`
			Params json.RawMessage `json:"params"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decode request: %v", err)
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "sess-memory")
			writeMemoryRPC(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			writeMemoryRPC(t, w, req.ID, map[string]any{"tools": []map[string]any{}})
		case "tools/call":
			var params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil {
				t.Errorf("decode tools/call params: %v", err)
			}
			rec.mu.Lock()
			rec.lastTool = params.Name
			rec.lastArgs = params.Arguments
			rec.mu.Unlock()
			writeMemoryRPC(t, w, req.ID, map[string]any{
				"content": []map[string]any{{"type": "text", "text": rec.cannedTxt}},
			})
		default:
			t.Errorf("unexpected method %q", req.Method)
			http.Error(w, "unexpected", http.StatusBadRequest)
		}
	}))
	t.Cleanup(rec.Close)
	return rec
}

func (rec *recordingMemoryMCPServer) tool() string {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.lastTool
}

func (rec *recordingMemoryMCPServer) args() map[string]any {
	rec.mu.Lock()
	defer rec.mu.Unlock()
	return rec.lastArgs
}

// writeMemoryRPC runs on httptest handler goroutines — t.Fatalf is illegal off the
// test goroutine (it would leak the handler), so failures report via t.Errorf (WR-05).
func writeMemoryRPC(t *testing.T, w http.ResponseWriter, id *int64, result any) {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Errorf("marshal result: %v", err)
		http.Error(w, "marshal", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"result":  json.RawMessage(raw),
	}); err != nil {
		t.Errorf("encode rpc: %v", err)
	}
}

// withMemoryServerAt writes a managed config that points the `memory` managed server
// at url as a streamable_http server, so effectiveManagedMCPServer("memory") resolves
// to the fake transport (RemoteHTTP trust is inferred for a streamable_http server, so
// it is runnable, not blocked).
func withMemoryServerAt(t *testing.T, url string) {
	t.Helper()
	path := withTempMCPConfig(t)
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		memoryServerName: {
			Type:   mcp.ServerTypeStreamableHTTP,
			URL:    url,
			Source: "recipe:memory",
			Trust:  mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save managed config: %v", err)
	}
}

func TestMemoryVerbMapping(t *testing.T) {
	rec := newRecordingMemoryMCPServer(t)
	withMemoryServerAt(t, rec.URL)

	cases := []struct {
		name     string
		args     []string
		wantTool string
		argKey   string
		argVal   string
	}{
		{"search", []string{"search", "mario rossi"}, "memory_search", "query", "mario rossi"},
		{"context", []string{"context", "what does the user prefer"}, "memory_get_context", "query", "what does the user prefer"},
		{"sessions", []string{"sessions"}, "memory_list_sessions", "", ""},
		{"conversation", []string{"conversation", "sess-1"}, "memory_get_conversation", "session_id", "sess-1"},
		{"add-entity", []string{"add-entity", "Mario Rossi", "PERSON", "a colleague"}, "memory_add_entity", "name", "Mario Rossi"},
		{"add-fact", []string{"add-fact", "aura", "validates", "memory wiring"}, "memory_add_fact", "subject", "aura"},
		{"add-preference", []string{"add-preference", "ui", "dark mode"}, "memory_add_preference", "category", "ui"},
		{"store-message", []string{"store-message", "sess-1", "user", "hello world"}, "memory_store_message", "session_id", "sess-1"},
		{"get-entity", []string{"get-entity", "Mario Rossi"}, "memory_get_entity", "name", "Mario Rossi"},
		{"relationship", []string{"relationship", "Mario", "KNOWS", "Luigi"}, "memory_create_relationship", "from_entity", "Mario"},
		{"export", []string{"export"}, "memory_export_graph", "", ""},
		{"trace-start", []string{"trace", "start", "sess-1", "debug the plan"}, "memory_start_trace", "session_id", "sess-1"},
		{"trace-step", []string{"trace", "step", "tr-1", "read the file"}, "memory_record_step", "trace_id", "tr-1"},
		{"trace-complete", []string{"trace", "complete", "tr-1"}, "memory_complete_trace", "trace_id", "tr-1"},
		{"trace-observations", []string{"trace", "observations", "sess-1"}, "memory_get_observations", "session_id", "sess-1"},
		{"query", []string{"query", "MATCH (n) RETURN n LIMIT 1"}, "graph_query", "query", "MATCH (n) RETURN n LIMIT 1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := runMemoryCommand(context.Background(), tc.args, &buf); err != nil {
				t.Fatalf("runMemoryCommand(%v): %v", tc.args, err)
			}
			if got := rec.tool(); got != tc.wantTool {
				t.Fatalf("verb %q dispatched tool %q, want RAW %q", tc.name, got, tc.wantTool)
			}
			if strings.HasPrefix(rec.tool(), "memory__") {
				t.Fatalf("verb %q dispatched a namespaced tool %q; expected RAW wire name", tc.name, rec.tool())
			}
			if !strings.Contains(buf.String(), rec.cannedTxt) {
				t.Fatalf("verb %q output %q missing canned result %q", tc.name, buf.String(), rec.cannedTxt)
			}
			if tc.argKey != "" {
				if got, _ := rec.args()[tc.argKey].(string); got != tc.argVal {
					t.Fatalf("verb %q arg %q = %q, want %q", tc.name, tc.argKey, got, tc.argVal)
				}
			}
		})
	}
}

func TestMemoryVerbMappingNegativeCases(t *testing.T) {
	rec := newRecordingMemoryMCPServer(t)
	withMemoryServerAt(t, rec.URL)

	cases := []struct {
		name string
		args []string
	}{
		{"no-args", nil},
		{"unknown-verb", []string{"frobnicate"}},
		{"search-missing-query", []string{"search"}},
		{"add-fact-too-few", []string{"add-fact", "subject", "predicate"}},
		{"trace-unknown-subverb", []string{"trace", "bogus"}},
		{"trace-start-too-few", []string{"trace", "start", "sess-1"}},
		{"trace-step-too-few", []string{"trace", "step", "tr-1"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := runMemoryCommand(context.Background(), tc.args, &buf); err == nil {
				t.Fatalf("args %v: expected non-nil error, got nil (output %q)", tc.args, buf.String())
			}
		})
	}
}

func TestMemoryNotConfigured(t *testing.T) {
	// Memory is default-on (15-02 inject-unless-disabled), so an empty managed
	// config still resolves the catalog recipe. The unresolvable state is an
	// explicit `aura mcp disable memory` (Enabled=false, D-09).
	path := withTempMCPConfig(t)
	disabled := false
	doc := mcp.ManagedConfig{MCPServers: map[string]mcp.ManagedServer{
		memoryServerName: {
			Type:    mcp.ServerTypeStreamableHTTP,
			URL:     "http://127.0.0.1:1/mcp/",
			Source:  "recipe:memory",
			Trust:   mcp.ManagedTrust{Class: mcp.TrustTrustedRecipe},
			Enabled: &disabled,
		},
	}}
	if err := mcp.SaveManagedConfig(path, doc); err != nil {
		t.Fatalf("save managed config: %v", err)
	}
	var buf bytes.Buffer
	err := runMemoryCommand(context.Background(), []string{"search", "x"}, &buf)
	if err == nil {
		t.Fatalf("expected error when memory server is disabled")
	}
	if !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("error = %v, want a not-configured message", err)
	}
}
