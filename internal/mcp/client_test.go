package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"sync"
	"testing"

	"github.com/chetto1983/aura/internal/boundedbuffer"
)

// fakeServer runs a scripted MCP server over the client's pipes in a goroutine. It
// reads newline-delimited requests and replies via reply(id, ...) per method. A
// notification (no id) is injected before each tools/call response to prove
// readResponse skips interleaved traffic.
type fakeServer struct {
	in  io.Reader // client's stdin (server reads requests here)
	out io.Writer // client's stdout (server writes responses here)
}

func (f *fakeServer) run() {
	dec := json.NewDecoder(f.in)
	for {
		var req map[string]any
		if err := dec.Decode(&req); err != nil {
			return // pipe closed
		}
		method, _ := req["method"].(string)
		id, hasID := req["id"]
		switch method {
		case "initialize":
			f.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"protocolVersion": protocolVersion, "serverInfo": map[string]any{"name": "fake"}}})
		case "notifications/initialized":
			// no response
		case "tools/list":
			f.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"tools": []map[string]any{
					{"name": "sandbox_exec", "description": "run", "inputSchema": map[string]any{"type": "object"}},
					{"name": "sandbox_stop", "description": "stop", "inputSchema": map[string]any{"type": "object"}},
				}}})
		case "tools/call":
			// interleaved notification (no id) — client must skip it.
			f.write(map[string]any{"jsonrpc": "2.0", "method": "notifications/message", "params": map[string]any{"level": "info"}})
			params, _ := req["params"].(map[string]any)
			args, _ := params["arguments"].(map[string]any)
			if boom, _ := args["boom"].(bool); boom {
				f.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
					"content": []map[string]any{{"type": "text", "text": "kaboom"}}, "isError": true}})
				continue
			}
			f.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "hello "}, {"type": "text", "text": "world"}}}})
		case "ping":
			f.write(map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		default:
			if hasID {
				f.write(map[string]any{"jsonrpc": "2.0", "id": id, "error": map[string]any{"code": -32601, "message": "method not found"}})
			}
		}
	}
}

func (f *fakeServer) write(v map[string]any) {
	enc, _ := json.Marshal(v)
	_, _ = f.out.Write(append(enc, '\n'))
}

// newClientForTest wires the protocol onto raw pipes without spawning a process,
// so the request/response logic is unit-testable against an in-memory fake server.
func newClientForTest(name string, stdin io.WriteCloser, stdout io.Reader) *Client {
	return &Client{name: name, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: boundedbuffer.New(0)}
}

// newTestPair wires a Client to a fakeServer over two io.Pipes and starts the
// server goroutine. It returns the client and a cleanup that closes both ends.
func newTestPair(t *testing.T) (*Client, func()) {
	t.Helper()
	cliStdinR, cliStdinW := io.Pipe()   // client writes requests -> server reads
	srvStdoutR, srvStdoutW := io.Pipe() // server writes responses -> client reads
	fs := &fakeServer{in: cliStdinR, out: srvStdoutW}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() { defer wg.Done(); fs.run() }()
	c := newClientForTest("fake", cliStdinW, srvStdoutR)
	cleanup := func() {
		_ = cliStdinW.Close() // unblocks the server decode loop
		_ = srvStdoutW.Close()
		_ = srvStdoutR.Close()
		wg.Wait()
	}
	return c, cleanup
}

func TestClient_Handshake_List_Call(t *testing.T) {
	c, cleanup := newTestPair(t)
	defer cleanup()

	if err := c.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	tools, err := c.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 2 || tools[0].Name != "sandbox_exec" || tools[1].Name != "sandbox_stop" {
		t.Fatalf("unexpected tools: %+v", tools)
	}
	// CallTool must concatenate the two text parts AND skip the interleaved notification.
	out, err := c.CallTool(context.Background(), "sandbox_exec", map[string]any{"container_id": "x"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out != "hello world" {
		t.Fatalf("CallTool text = %q, want %q", out, "hello world")
	}
}

func TestClient_CallTool_IsError(t *testing.T) {
	c, cleanup := newTestPair(t)
	defer cleanup()
	if err := c.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	_, err := c.CallTool(context.Background(), "sandbox_exec", map[string]any{"boom": true})
	if err == nil {
		t.Fatal("want error for isError=true result, got nil")
	}
	if !strings.Contains(err.Error(), "kaboom") {
		t.Fatalf("error should carry tool text, got %q", err.Error())
	}
}

func TestClient_RpcError(t *testing.T) {
	c, cleanup := newTestPair(t)
	defer cleanup()
	if err := c.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	// An unknown method roundtrip surfaces the JSON-RPC error object.
	_, err := c.roundtrip("nonexistent/method", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "method not found") {
		t.Fatalf("want rpc error, got %v", err)
	}
}

func TestDecodeToolResult(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		want    string
		isError bool
		wantErr bool
	}{
		{"empty", "", "", false, false},
		{"single", `{"content":[{"type":"text","text":"42\n"}]}`, "42", false, false},
		{"concat", `{"content":[{"type":"text","text":"a "},{"type":"text","text":"b"}]}`, "a b", false, false},
		{"skip-nontext", `{"content":[{"type":"image","text":"x"},{"type":"text","text":"ok"}]}`, "ok", false, false},
		{"is-error", `{"content":[{"type":"text","text":"boom"}],"isError":true}`, "boom", true, false},
		{"malformed", `{not json`, "", false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			text, isErr, err := decodeToolResult(json.RawMessage(tc.in))
			if tc.wantErr {
				if err == nil {
					t.Fatal("want decode error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected err: %v", err)
			}
			if text != tc.want || isErr != tc.isError {
				t.Fatalf("got (%q,%v), want (%q,%v)", text, isErr, tc.want, tc.isError)
			}
		})
	}
}

func TestStderrTailRedactsSecrets(t *testing.T) {
	buf := boundedbuffer.New(0)
	_, _ = buf.Write([]byte("TOKEN=abc123 SMTP_PASS=hunter2 Authorization: Bearer hidden"))
	c := &Client{stderr: buf}
	got := c.stderrTail()
	for _, bad := range []string{"abc123", "hunter2", "Bearer hidden"} {
		if strings.Contains(got, bad) {
			t.Fatalf("stderr tail leaked %q: %s", bad, got)
		}
	}
	if !strings.Contains(got, "TOKEN=<redacted>") {
		t.Fatalf("stderr tail missing redaction placeholder: %s", got)
	}
}

func TestBoundedStderrBufferKeepsTail(t *testing.T) {
	buf := boundedbuffer.New(64)
	_, _ = buf.Write([]byte(strings.Repeat("a", 120)))
	_, _ = buf.Write([]byte("THE-END"))
	if got := buf.Len(); got > 64 {
		t.Fatalf("buffer len = %d, want <= 64", got)
	}
	c := &Client{stderr: buf}
	tail := c.stderrTail()
	if !strings.Contains(tail, "THE-END") {
		t.Fatalf("stderr tail lost newest bytes: %q", tail)
	}
	if strings.Contains(buf.String(), strings.Repeat("a", 70)) {
		t.Fatalf("buffer retained too much old content")
	}
}
