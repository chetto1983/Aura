package markitdown

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// fakeSidecar mounts an httptest server that speaks just enough MCP
// Streamable-HTTP to satisfy initialize / tools/list / tools/call.
//
// Behavior:
//   - On tools/call for convert_to_markdown, it parses the data: URI back to
//     bytes, captures them for assertions, and returns the configured
//     markdown reply (default: "OK").
//   - If responseErr is non-empty, tools/call returns an isError envelope.
//   - lastBytes is what the server saw after base64-decoding the URI.
type fakeSidecar struct {
	t           *testing.T
	srv         *httptest.Server
	toolName    string
	response    string
	responseErr string

	calls     int
	lastURI   string
	lastBytes []byte
	lastMime  string
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
}

func newFakeSidecar(t *testing.T) *fakeSidecar {
	t.Helper()
	f := &fakeSidecar{t: t, toolName: convertToolName, response: "OK"}
	f.srv = httptest.NewServer(http.HandlerFunc(f.handle))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeSidecar) handle(w http.ResponseWriter, r *http.Request) {
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
		body := `{"tools":[{"name":"` + f.toolName + `"}]}`
		_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: json.RawMessage(body)})
	case "tools/call":
		f.calls++
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		if uri, ok := p.Arguments["uri"].(string); ok {
			f.lastURI = uri
			if mime, payload, ok := parseDataURI(uri); ok {
				f.lastMime = mime
				if b, err := base64.StdEncoding.DecodeString(payload); err == nil {
					f.lastBytes = b
				}
			}
		}
		if f.responseErr != "" {
			body, _ := json.Marshal(map[string]any{
				"content": []map[string]any{{"type": "text", "text": f.responseErr}},
				"isError": true,
			})
			_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: body})
			return
		}
		body, _ := json.Marshal(map[string]any{
			"content": []map[string]any{{"type": "text", "text": f.response}},
		})
		_ = json.NewEncoder(w).Encode(rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: body})
	default:
		http.Error(w, "unknown method: "+req.Method, 400)
	}
}

func parseDataURI(uri string) (mime, payload string, ok bool) {
	if !strings.HasPrefix(uri, "data:") {
		return "", "", false
	}
	rest := uri[len("data:"):]
	comma := strings.Index(rest, ",")
	if comma < 0 {
		return "", "", false
	}
	header := rest[:comma]
	payload = rest[comma+1:]
	semi := strings.Index(header, ";")
	if semi < 0 {
		return header, payload, true
	}
	return header[:semi], payload, true
}

// newClientForFake wires a Client that targets the fake sidecar. The fake
// mounts the MCP endpoint at "/" (httptest default); the real sidecar uses
// "/mcp". The fake's catch-all handler accepts the appended /mcp suffix.
func newClientForFake(f *fakeSidecar) *Client {
	// Production code appends "/mcp" to the base URL. The fake serves
	// everything at "/", so we set baseURL to the fake URL with a trailing
	// segment that, when "/mcp" is appended, still routes to the fake.
	// Simpler: strip the suffix via custom Config — but Config has no hook
	// for path override, so we point baseURL at "fake_url" and let the
	// fake's catch-all handler accept the /mcp suffix.
	return New(Config{BaseURL: f.srv.URL, Timeout: 2 * time.Second, MaxBytes: 1 << 20})
}

func TestClient_ConvertSendsDataURI(t *testing.T) {
	f := newFakeSidecar(t)
	f.response = "# hello\n\nworld\n"
	c := newClientForFake(f)
	defer func() { _ = c.Close() }()

	payload := []byte("Sheet1,42\nSheet1,43\n")
	res, err := c.Convert(context.Background(), ConvertInput{
		Bytes:    payload,
		MimeType: "text/csv",
		Filename: "budget.csv",
	})
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if res.Markdown != "# hello\n\nworld\n" {
		t.Fatalf("markdown: %q", res.Markdown)
	}
	if f.calls != 1 {
		t.Fatalf("calls: %d", f.calls)
	}
	if f.lastMime != "text/csv" {
		t.Fatalf("mime: %q", f.lastMime)
	}
	if string(f.lastBytes) != string(payload) {
		t.Fatalf("bytes mismatch")
	}
}

func TestClient_RejectsEmptyBytes(t *testing.T) {
	f := newFakeSidecar(t)
	c := newClientForFake(f)
	defer func() { _ = c.Close() }()
	_, err := c.Convert(context.Background(), ConvertInput{Bytes: nil, MimeType: "text/csv"})
	if err == nil {
		t.Fatal("expected error on empty bytes")
	}
	if f.calls != 0 {
		t.Fatalf("sidecar called despite empty input: %d", f.calls)
	}
}

func TestClient_RejectsOversize(t *testing.T) {
	f := newFakeSidecar(t)
	c := New(Config{BaseURL: f.srv.URL, MaxBytes: 16})
	defer func() { _ = c.Close() }()
	_, err := c.Convert(context.Background(), ConvertInput{Bytes: make([]byte, 32), MimeType: "text/plain"})
	if err == nil || !strings.Contains(err.Error(), "exceeds cap") {
		t.Fatalf("expected size cap error, got %v", err)
	}
	if f.calls != 0 {
		t.Fatalf("sidecar called despite oversize input: %d", f.calls)
	}
}

func TestClient_PropagatesSidecarError(t *testing.T) {
	f := newFakeSidecar(t)
	f.responseErr = "unsupported format"
	c := newClientForFake(f)
	defer func() { _ = c.Close() }()
	_, err := c.Convert(context.Background(), ConvertInput{Bytes: []byte("payload"), MimeType: "application/x-unknown"})
	if err == nil || !strings.Contains(err.Error(), "unsupported format") {
		t.Fatalf("expected sidecar error to propagate, got %v", err)
	}
}

func TestClient_RejectsMissingTool(t *testing.T) {
	f := newFakeSidecar(t)
	f.toolName = "wrong_tool"
	c := newClientForFake(f)
	defer func() { _ = c.Close() }()
	_, err := c.Convert(context.Background(), ConvertInput{Bytes: []byte("payload"), MimeType: "text/plain"})
	if err == nil || !strings.Contains(err.Error(), convertToolName) {
		t.Fatalf("expected missing-tool error, got %v", err)
	}
}

func TestClient_NoBaseURL(t *testing.T) {
	c := New(Config{BaseURL: ""})
	defer func() { _ = c.Close() }()
	_, err := c.Convert(context.Background(), ConvertInput{Bytes: []byte("x"), MimeType: "text/plain"})
	if err == nil {
		t.Fatal("expected error on missing base url")
	}
}

func TestClient_ResetsOnFailure(t *testing.T) {
	f := newFakeSidecar(t)
	c := newClientForFake(f)
	defer func() { _ = c.Close() }()
	// First call succeeds → connection cached.
	if _, err := c.Convert(context.Background(), ConvertInput{Bytes: []byte("hi"), MimeType: "text/plain"}); err != nil {
		t.Fatalf("setup convert: %v", err)
	}
	// Flip server to error, second call should fail and drop the cached connection.
	f.responseErr = "boom"
	if _, err := c.Convert(context.Background(), ConvertInput{Bytes: []byte("hi"), MimeType: "text/plain"}); err == nil {
		t.Fatal("expected error")
	}
	c.mu.Lock()
	cached := c.inner
	c.mu.Unlock()
	if cached != nil {
		t.Fatal("expected cached client to be cleared after failure")
	}
}
