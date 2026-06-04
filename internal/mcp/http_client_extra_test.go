package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// httpInitHandler answers initialize/notifications/initialized so OpenHTTP
// succeeds, then delegates every other method to next. The request body is
// buffered and restored before delegating so next can decode it again.
func httpInitHandler(t *testing.T, sessionID string, next http.HandlerFunc) http.HandlerFunc {
	t.Helper()
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			next(w, r)
			return
		}
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		var req rpcReq
		if err := json.Unmarshal(raw, &req); err != nil {
			// notifications carry no id and decode fine; a genuine failure is fatal.
			t.Fatalf("decode request: %v", err)
		}
		switch req.Method {
		case "initialize":
			if sessionID != "" {
				w.Header().Set("Mcp-Session-Id", sessionID)
			}
			writeHTTPRPC(t, w, req.ID, map[string]any{"protocolVersion": "2025-06-18"})
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		default:
			r.Body = io.NopCloser(bytes.NewReader(raw))
			next(w, r)
		}
	}
}

func openHTTPTest(t *testing.T, srv *httptest.Server, cfg HTTPConfig) *HTTPClient {
	t.Helper()
	cfg.URL = srv.URL
	c, err := OpenHTTP(context.Background(), "x", cfg)
	if err != nil {
		t.Fatalf("OpenHTTP: %v", err)
	}
	return c
}

func TestHTTPDecodeSSEResponse(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "sse-1", func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "text/event-stream")
		// Decoy event for a different id, a comment line, then the real one.
		fmt.Fprintf(w, "event: message\n")
		fmt.Fprintf(w, "data: %s\n\n", mustRaw(t, rpcResp{JSONRPC: "2.0", ID: int64Ptr(999), Result: mustRaw(t, map[string]any{"tools": []any{}})}))
		fmt.Fprintf(w, ": keep-alive comment\n")
		fmt.Fprintf(w, "data: %s\n\n", mustRaw(t, rpcResp{JSONRPC: "2.0", ID: &req.ID, Result: mustRaw(t, map[string]any{
			"content": []map[string]any{{"type": "text", "text": "sse-pong"}}})}))
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	got, err := c.CallTool(context.Background(), "echo", map[string]any{})
	if err != nil {
		t.Fatalf("CallTool over SSE: %v", err)
	}
	if got != "sse-pong" {
		t.Fatalf("CallTool = %q, want sse-pong", got)
	}
}

func TestHTTPDecodeSSEMissingID(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Only a mismatched id is ever sent, so the wanted id is never found.
		fmt.Fprintf(w, "data: %s\n\n", mustRaw(t, rpcResp{JSONRPC: "2.0", ID: int64Ptr(424242), Result: mustRaw(t, map[string]any{})}))
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	if err := c.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "missing response id") {
		t.Fatalf("want missing id error, got %v", err)
	}
}

func TestHTTPDecodeSSEMalformedData(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprintf(w, "data: {not-json}\n\n")
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	if err := c.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "decode sse") {
		t.Fatalf("want sse decode error, got %v", err)
	}
}

func TestHTTPCallToolIsError(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeHTTPRPC(t, w, req.ID, map[string]any{
			"content": []map[string]any{{"type": "text", "text": "tool blew up"}}, "isError": true})
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	_, err := c.CallTool(context.Background(), "boom", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "tool blew up") {
		t.Fatalf("want isError surfaced, got %v", err)
	}
}

func TestHTTPCallToolMalformedBody(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{not json"))
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	_, err := c.CallTool(context.Background(), "echo", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("want decode response error, got %v", err)
	}
}

func TestHTTPPingSuccess(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeHTTPRPC(t, w, req.ID, map[string]any{})
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestHTTPPostNon2xx(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	if err := c.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "http 500") {
		t.Fatalf("want http 500, got %v", err)
	}
}

func TestHTTPPostUnauthorized(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	if err := c.Ping(context.Background()); err == nil || !strings.Contains(err.Error(), "unauthorized") {
		t.Fatalf("want unauthorized, got %v", err)
	}
}

func TestHTTPCloseDeletesSession(t *testing.T) {
	var sawDelete bool
	srv := httptest.NewServer(httpInitHandler(t, "del-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			sawDelete = true
			if r.Header.Get("Mcp-Session-Id") != "del-1" {
				t.Fatalf("delete missing session id: %q", r.Header.Get("Mcp-Session-Id"))
			}
			w.WriteHeader(http.StatusOK)
			return
		}
		t.Fatalf("unexpected method %s", r.Method)
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !sawDelete {
		t.Fatal("expected DELETE to be issued")
	}
	if c.sessionID != "" {
		t.Fatalf("sessionID = %q, want cleared", c.sessionID)
	}
	// Second Close is a no-op (sessionID already empty).
	if err := c.Close(); err != nil {
		t.Fatalf("idempotent Close: %v", err)
	}
}

func TestHTTPCloseTreats405And404AsClosed(t *testing.T) {
	for _, status := range []int{http.StatusMethodNotAllowed, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(httpInitHandler(t, "sid", func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodDelete {
					w.WriteHeader(status)
					return
				}
				t.Fatalf("unexpected method %s", r.Method)
			}))
			defer srv.Close()
			c := openHTTPTest(t, srv, HTTPConfig{})
			if err := c.Close(); err != nil {
				t.Fatalf("Close with %d should be nil, got %v", status, err)
			}
			if c.sessionID != "" {
				t.Fatalf("sessionID not cleared after %d", status)
			}
		})
	}
}

func TestHTTPCloseServerErrorReturnsError(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "sid", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		t.Fatalf("unexpected method %s", r.Method)
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	if err := c.Close(); err == nil || !strings.Contains(err.Error(), "http 500") {
		t.Fatalf("want http 500 on Close, got %v", err)
	}
}

func TestHTTPCloseNoSessionIsNoop(t *testing.T) {
	c := &HTTPClient{client: http.DefaultClient}
	if err := c.Close(); err != nil {
		t.Fatalf("Close with no session = %v, want nil", err)
	}
}

func TestOpenHTTPEmptyURL(t *testing.T) {
	if _, err := OpenHTTP(context.Background(), "blank", HTTPConfig{URL: "  "}); err == nil {
		t.Fatal("want error for empty URL")
	}
}

func TestHTTPListToolsAndCallToolContextCanceled(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeHTTPRPC(t, w, req.ID, map[string]any{"tools": []map[string]any{}})
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	ctx := canceledCtx()
	if _, err := c.ListTools(ctx); err == nil {
		t.Fatal("ListTools want context error")
	}
	if _, err := c.CallTool(ctx, "x", nil); err == nil {
		t.Fatal("CallTool want context error")
	}
}

func TestHTTPListToolsDecodeError(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		// A non-object result makes the tools envelope decode fail.
		writeHTTPRPC(t, w, req.ID, []string{"not", "an", "object"})
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	if _, err := c.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "decode tools/list") {
		t.Fatalf("want decode tools/list error, got %v", err)
	}
}

func TestHTTPListToolsSuccess(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		writeHTTPRPC(t, w, req.ID, map[string]any{"tools": []map[string]any{
			{"name": "t1", "inputSchema": map[string]any{"type": "object"}},
		}})
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "t1" {
		t.Fatalf("tools = %#v", tools)
	}
}

func TestHTTPCloseDeleteNetworkError(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "sid", func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected method %s", r.Method)
	}))
	c := openHTTPTest(t, srv, HTTPConfig{})
	srv.Close() // server gone: the DELETE in Close fails at the transport layer.
	if err := c.Close(); err == nil {
		t.Fatal("want network error from Close DELETE")
	}
}

func TestHTTPCallToolNilArgsAndResultDecodeError(t *testing.T) {
	srv := httptest.NewServer(httpInitHandler(t, "", func(w http.ResponseWriter, r *http.Request) {
		var req rpcReq
		_ = json.NewDecoder(r.Body).Decode(&req)
		// A non-object result makes decodeToolResult's envelope unmarshal fail.
		writeHTTPRPC(t, w, req.ID, "plain string")
	}))
	defer srv.Close()
	c := openHTTPTest(t, srv, HTTPConfig{})
	defer func() { _ = c.Close() }()

	// nil args exercises the args==nil normalization branch.
	if _, err := c.CallTool(context.Background(), "x", nil); err == nil || !strings.Contains(err.Error(), "tool result envelope") {
		t.Fatalf("want envelope decode error, got %v", err)
	}
}

func int64Ptr(v int64) *int64 { return &v }
