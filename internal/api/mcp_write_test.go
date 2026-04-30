package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/aura/aura/internal/mcp"
)

// fakeMCPServer captures the last tools/call invocation and lets each
// test pre-program the response. It implements just enough of the MCP
// JSON-RPC dance for `mcp.NewHTTPClient` to bootstrap and call a tool.
type fakeMCPServer struct {
	tb         testing.TB
	mu         sync.Mutex
	tools      []mcp.Tool
	resultText string
	isError    bool
	failHTTP   int    // non-zero → return that status on tools/call
	lastArgs   string // JSON snapshot of the last tools/call arguments
}

func newFakeMCPServer(tb testing.TB, tools []mcp.Tool) (*httptest.Server, *fakeMCPServer) {
	state := &fakeMCPServer{tb: tb, tools: tools}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var probe struct {
			Method string          `json:"method"`
			ID     json.RawMessage `json:"id,omitempty"`
			Params json.RawMessage `json:"params,omitempty"`
		}
		body := make([]byte, r.ContentLength)
		if r.ContentLength > 0 {
			_, _ = r.Body.Read(body)
		}
		_ = json.Unmarshal(body, &probe)
		w.Header().Set("Content-Type", "application/json")
		switch probe.Method {
		case "initialize":
			w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(probe.ID) + `,"result":{"capabilities":{}}}`))
		case "notifications/initialized":
			w.WriteHeader(http.StatusAccepted)
		case "tools/list":
			toolsJSON, _ := json.Marshal(state.tools)
			w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(probe.ID) + `,"result":{"tools":` + string(toolsJSON) + `}}`))
		case "tools/call":
			state.mu.Lock()
			state.lastArgs = string(probe.Params)
			fail := state.failHTTP
			isErr := state.isError
			text := state.resultText
			state.mu.Unlock()
			if fail != 0 {
				http.Error(w, "boom", fail)
				return
			}
			payload := map[string]any{
				"content": []map[string]any{{"type": "text", "text": text}},
			}
			if isErr {
				payload["isError"] = true
			}
			result, _ := json.Marshal(payload)
			w.Write([]byte(`{"jsonrpc":"2.0","id":` + string(probe.ID) + `,"result":` + string(result) + `}`))
		default:
			http.Error(w, "unknown method", http.StatusBadRequest)
		}
	}))
	tb.Cleanup(srv.Close)
	return srv, state
}

func newInvokeRouter(t *testing.T, name string, state *fakeMCPServer, srv *httptest.Server) http.Handler {
	t.Helper()
	client, err := mcp.NewHTTPClient(name, srv.URL, nil)
	if err != nil {
		t.Fatalf("mcp client: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	_ = state // captured by handler closure via the server
	return NewRouter(Deps{MCP: []*mcp.Client{client}})
}

func postRaw(t *testing.T, router http.Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", path, bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	router.ServeHTTP(rr, req)
	return rr
}

func TestMCPInvoke_HappyPath(t *testing.T) {
	srv, state := newFakeMCPServer(t, []mcp.Tool{{Name: "echo"}})
	state.resultText = "pong"
	router := newInvokeRouter(t, "srv1", state, srv)

	rr := postRaw(t, router, "/mcp/srv1/tools/echo", `{"q":"hello","n":42}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got MCPInvokeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.OK || got.Output != "pong" {
		t.Fatalf("response: %+v", got)
	}
	state.mu.Lock()
	args := state.lastArgs
	state.mu.Unlock()
	// Server gets the args nested under "arguments" by the MCP client.
	if !strings.Contains(args, `"q":"hello"`) || !strings.Contains(args, `"n":42`) {
		t.Errorf("server args missing payload: %s", args)
	}
}

func TestMCPInvoke_EmptyBodyMeansNoArgs(t *testing.T) {
	srv, state := newFakeMCPServer(t, []mcp.Tool{{Name: "ping"}})
	state.resultText = "ok"
	router := newInvokeRouter(t, "srv1", state, srv)

	rr := postRaw(t, router, "/mcp/srv1/tools/ping", "")
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	state.mu.Lock()
	args := state.lastArgs
	state.mu.Unlock()
	// Empty body → arguments:{} — no panic, server still sees a valid call.
	if !strings.Contains(args, `"arguments":{}`) {
		t.Errorf("expected empty args, got: %s", args)
	}
}

func TestMCPInvoke_RejectsNonObjectBody(t *testing.T) {
	srv, state := newFakeMCPServer(t, []mcp.Tool{{Name: "ping"}})
	router := newInvokeRouter(t, "srv1", state, srv)

	for _, body := range []string{`"string"`, `42`, `[]`, `{`, `not json`} {
		t.Run(body, func(t *testing.T) {
			rr := postRaw(t, router, "/mcp/srv1/tools/ping", body)
			if rr.Code != http.StatusBadRequest {
				t.Fatalf("body %q: status %d, body %s", body, rr.Code, rr.Body)
			}
		})
	}
}

func TestMCPInvoke_UnknownServer(t *testing.T) {
	router := NewRouter(Deps{})
	rr := postRaw(t, router, "/mcp/nope/tools/anything", `{}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestMCPInvoke_UnknownTool(t *testing.T) {
	srv, state := newFakeMCPServer(t, []mcp.Tool{{Name: "ping"}})
	router := newInvokeRouter(t, "srv1", state, srv)

	rr := postRaw(t, router, "/mcp/srv1/tools/missing", `{}`)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestMCPInvoke_BadToolName(t *testing.T) {
	// Path validation runs after server resolution but before the call.
	srv, state := newFakeMCPServer(t, []mcp.Tool{{Name: "ping"}})
	router := newInvokeRouter(t, "srv1", state, srv)

	rr := postRaw(t, router, "/mcp/srv1/tools/has%20space", `{}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
}

func TestMCPInvoke_ServerToolError(t *testing.T) {
	srv, state := newFakeMCPServer(t, []mcp.Tool{{Name: "ping"}})
	state.resultText = "you broke it"
	state.isError = true
	router := newInvokeRouter(t, "srv1", state, srv)

	rr := postRaw(t, router, "/mcp/srv1/tools/ping", `{}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got MCPInvokeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.OK {
		t.Errorf("expected ok=false on isError")
	}
	if !got.IsError {
		t.Errorf("expected is_error=true")
	}
	if !strings.Contains(got.Error, "you broke it") {
		t.Errorf("error missing tool message: %q", got.Error)
	}
}

func TestMCPInvoke_TransportError(t *testing.T) {
	srv, state := newFakeMCPServer(t, []mcp.Tool{{Name: "ping"}})
	state.failHTTP = http.StatusInternalServerError
	router := newInvokeRouter(t, "srv1", state, srv)

	rr := postRaw(t, router, "/mcp/srv1/tools/ping", `{}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got MCPInvokeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.OK {
		t.Errorf("expected ok=false on transport error")
	}
	if got.IsError {
		t.Errorf("transport failure should not be flagged is_error (which means upstream returned isError:true)")
	}
}

func TestMCPInvoke_ClipsLargeOutput(t *testing.T) {
	srv, state := newFakeMCPServer(t, []mcp.Tool{{Name: "ping"}})
	state.resultText = strings.Repeat("x", mcpInvokeMaxOutput+1024)
	router := newInvokeRouter(t, "srv1", state, srv)

	rr := postRaw(t, router, "/mcp/srv1/tools/ping", `{}`)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d, body %s", rr.Code, rr.Body)
	}
	var got MCPInvokeResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got.Output, "[truncated]") {
		t.Fatalf("expected truncation marker, got tail %q", got.Output[len(got.Output)-32:])
	}
}
