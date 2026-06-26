package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPInitializeSessionProtocolAndListTools(t *testing.T) {
	var sawInitialized bool
	var sawSessionHeader bool
	var sawProtocolHeader bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Accept"); !strings.Contains(got, "application/json") || !strings.Contains(got, "text/event-stream") {
			t.Fatalf("Accept = %q", got)
		}
		var req rpcReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			if r.Header.Get("MCP-Protocol-Version") != "" {
				t.Fatalf("initialize should not send protocol header before negotiation")
			}
			w.Header().Set("Mcp-Session-Id", "session-1")
			writeHTTPRPC(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18", "serverInfo": map[string]any{"name": "http-fixture"}})
		case "notifications/initialized":
			sawInitialized = true
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			sawSessionHeader = r.Header.Get("Mcp-Session-Id") == "session-1"
			sawProtocolHeader = r.Header.Get("MCP-Protocol-Version") == "2025-06-18"
			writeHTTPRPC(t, w, req.ID, map[string]any{"tools": []map[string]any{
				{
					"name":        "echo",
					"description": "Echo text",
					"inputSchema": map[string]any{"type": "object"},
					"annotations": map[string]any{"readOnlyHint": true},
				},
			}})
		default:
			t.Fatalf("unexpected method %q", req.Method)
		}
	}))
	defer server.Close()

	c, err := OpenHTTP(context.Background(), "fixture", HTTPConfig{URL: server.URL})
	if err != nil {
		t.Fatalf("OpenHTTP: %v", err)
	}
	defer func() { _ = c.Close() }()
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %#v", tools)
	}
	if !tools[0].Annotations.ReadOnlyHint {
		t.Fatalf("annotations = %#v, want readOnlyHint", tools[0].Annotations)
	}
	if !sawInitialized || !sawSessionHeader || !sawProtocolHeader {
		t.Fatalf("headers/notification: initialized=%v session=%v protocol=%v", sawInitialized, sawSessionHeader, sawProtocolHeader)
	}
}

func TestHTTPJSONBodyCap(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(strings.Repeat("x", httpRPCMaxBodyBytes+1)))
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	err := c.Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("want response body cap error, got %v", err)
	}
}

func TestHTTPSSEBodyCap(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write([]byte(strings.Repeat(": keep-alive\n", (httpRPCMaxBodyBytes/len(": keep-alive\n"))+1)))
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	err := c.Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "response body exceeds") {
		t.Fatalf("want response body cap error, got %v", err)
	}
}

func TestHTTPPostTransportErrorWrapsErrTransport(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeHTTPRPC(t, w, req.ID, map[string]any{})
	}))
	c := openHTTPTest(t, srv, HTTPConfig{})
	srv.Close()
	defer func() { _ = c.Close() }()

	err := c.Ping(context.Background())
	if !errors.Is(err, ErrTransport) || !strings.Contains(err.Error(), "http post") {
		t.Fatalf("want ErrTransport http post error, got %v", err)
	}
}

func TestHTTPCallToolSendsAuthHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-123" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("X-API-Key"); got != "key-456" {
			t.Fatalf("X-API-Key = %q", got)
		}
		var req rpcReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if req.Method == "initialize" {
			writeHTTPRPC(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
			return
		}
		writeHTTPRPC(t, w, req.ID, map[string]any{"content": []map[string]any{{"type": "text", "text": "pong"}}})
	}))
	defer server.Close()

	c, err := OpenHTTP(context.Background(), "auth", HTTPConfig{
		URL:         server.URL,
		BearerToken: "token-123",
		Headers:     map[string]string{"X-API-Key": "key-456"},
	})
	if err != nil {
		t.Fatalf("OpenHTTP: %v", err)
	}
	got, err := c.CallTool(context.Background(), "echo", map[string]any{"text": "ping"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if got != "pong" {
		t.Fatalf("CallTool = %q, want pong", got)
	}
}

func TestHTTPSession404ResetsSession(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			w.Header().Set("Mcp-Session-Id", "expired")
			writeHTTPRPC(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c, err := OpenHTTP(context.Background(), "reset", HTTPConfig{URL: server.URL})
	if err != nil {
		t.Fatalf("OpenHTTP: %v", err)
	}
	_, err = c.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "session expired") {
		t.Fatalf("want session expired error, got %v", err)
	}
	if c.sessionID != "" {
		t.Fatalf("sessionID = %q, want reset", c.sessionID)
	}
}

func TestHTTPRPCErrorAndContextCancel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Method == "notifications/initialized" {
			w.WriteHeader(http.StatusAccepted)
			return
		}
		if req.Method == "initialize" {
			writeHTTPRPC(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
			return
		}
		writeHTTPRPCError(t, w, req.ID, -32601, "no tools")
	}))
	defer server.Close()

	c, err := OpenHTTP(context.Background(), "errors", HTTPConfig{URL: server.URL})
	if err != nil {
		t.Fatalf("OpenHTTP: %v", err)
	}
	_, err = c.ListTools(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no tools") {
		t.Fatalf("want rpc error, got %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Ping(ctx); err == nil || !strings.Contains(err.Error(), context.Canceled.Error()) {
		t.Fatalf("want context canceled, got %v", err)
	}
}

func TestHTTPTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	_, err := OpenHTTP(ctx, "timeout", HTTPConfig{URL: server.URL})
	if err == nil {
		t.Fatal("want timeout error")
	}
}

func TestHTTPCloseBoundedOnUnresponsiveDelete(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(httpInitHandler(t, "session-close", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("method = %s, want DELETE", r.Method)
			return
		}
		<-release // block until released, simulating an unresponsive endpoint
	}))
	defer srv.Close()
	defer close(release) // LIFO: unblock the handler before srv.Close waits on it
	c := openHTTPTest(t, srv, HTTPConfig{})

	done := make(chan error, 1)
	start := time.Now()
	go func() { done <- c.Close() }()

	select {
	case err := <-done:
		elapsed := time.Since(start)
		if elapsed > httpCloseTimeout+2*time.Second {
			t.Fatalf("Close took %v, want bounded by ~%v", elapsed, httpCloseTimeout)
		}
		if err == nil {
			t.Fatalf("want error from timed-out DELETE, got nil")
		}
	case <-time.After(httpCloseTimeout + 5*time.Second):
		t.Fatalf("Close hung past the bounded timeout")
	}
}

func writeHTTPRPC(t *testing.T, w http.ResponseWriter, id int64, result any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rpcResp{JSONRPC: "2.0", ID: &id, Result: mustRaw(t, result)}); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func writeHTTPRPCError(t *testing.T, w http.ResponseWriter, id int64, code int, message string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(rpcResp{JSONRPC: "2.0", ID: &id, Error: &rpcError{Code: code, Message: message}}); err != nil {
		t.Fatalf("encode response: %v", err)
	}
}

func mustRaw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal raw: %v", err)
	}
	return data
}
