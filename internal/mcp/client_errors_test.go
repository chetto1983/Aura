package mcp

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/boundedbuffer"
)

func canceledCtx() context.Context {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	return ctx
}

func TestStdioClientContextCanceledShortCircuits(t *testing.T) {
	c, cleanup := newTestPair(t)
	defer cleanup()
	if err := c.initialize(); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	ctx := canceledCtx()
	if _, err := c.ListTools(ctx); err == nil {
		t.Fatal("ListTools want context error")
	}
	if _, err := c.CallTool(ctx, "x", nil); err == nil {
		t.Fatal("CallTool want context error")
	}
	if err := c.Ping(ctx); err == nil {
		t.Fatal("Ping want context error")
	}
}

func TestStdioRoundtripSendErrorIncludesStderrTail(t *testing.T) {
	// A client whose stdin is already closed makes roundtrip's write fail; the
	// error must wrap the send failure and carry the redacted stderr tail.
	pr, pw := io.Pipe()
	_ = pw.Close() // any write to pw now errors
	c := newClientForTest("send", pw, pr)
	_, _ = c.stderr.Write([]byte("startup failed"))
	_, err := c.roundtrip("ping", map[string]any{})
	if err == nil || !strings.Contains(err.Error(), "send ping") {
		t.Fatalf("want send error, got %v", err)
	}
	if !IsTransportError(err) {
		t.Fatalf("send error must be classified as transport, got %v", err)
	}
	if !strings.Contains(err.Error(), "startup failed") {
		t.Fatalf("error should carry stderr tail, got %v", err)
	}
}

func TestStdioReadResponseRecvEOF(t *testing.T) {
	// stdout already at EOF: readResponse must surface a recv error.
	c := newClientForTest("eof", nopWriteCloser{}, strings.NewReader(""))
	_, err := c.readResponse(1)
	if err == nil || !strings.Contains(err.Error(), "recv") {
		t.Fatalf("want recv error, got %v", err)
	}
	if !IsTransportError(err) {
		t.Fatalf("recv EOF must be classified as transport, got %v", err)
	}
}

func TestStdioReadResponseSkipsBlanksAndMismatchedIDs(t *testing.T) {
	// Blank lines and a response for a different id are skipped; the matching id
	// is returned.
	stream := "\n   \n" +
		`{"jsonrpc":"2.0","id":99,"result":{"other":true}}` + "\n" +
		`{"jsonrpc":"2.0","result":{"notification":true}}` + "\n" +
		`{"jsonrpc":"2.0","id":7,"result":{"ok":true}}` + "\n"
	c := newClientForTest("skip", nopWriteCloser{}, strings.NewReader(stream))
	res, err := c.readResponse(7)
	if err != nil {
		t.Fatalf("readResponse: %v", err)
	}
	if !strings.Contains(string(res), "ok") {
		t.Fatalf("result = %s, want ok", res)
	}
}

func TestStdioReadResponseMalformedLine(t *testing.T) {
	c := newClientForTest("bad", nopWriteCloser{}, strings.NewReader("{not json\n"))
	_, err := c.readResponse(1)
	if err == nil || !strings.Contains(err.Error(), "decode response") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func TestStdioListToolsRpcError(t *testing.T) {
	// roundtrip's response carries a JSON-RPC error; ListTools wraps it.
	resp := `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"no list"}}` + "\n"
	c := newClientForTest("le", nopWriteCloser{}, strings.NewReader(resp))
	if _, err := c.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "tools/list") {
		t.Fatalf("want tools/list error, got %v", err)
	}
}

func TestStdioListToolsDecodeError(t *testing.T) {
	// A non-object result makes the tools envelope decode fail.
	resp := `{"jsonrpc":"2.0","id":1,"result":["not","an","object"]}` + "\n"
	c := newClientForTest("ld", nopWriteCloser{}, strings.NewReader(resp))
	if _, err := c.ListTools(context.Background()); err == nil || !strings.Contains(err.Error(), "decode tools/list") {
		t.Fatalf("want decode error, got %v", err)
	}
}

func TestStdioCallToolRpcError(t *testing.T) {
	resp := `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"call failed"}}` + "\n"
	c := newClientForTest("ce", nopWriteCloser{}, strings.NewReader(resp))
	if _, err := c.CallTool(context.Background(), "x", nil); err == nil || !strings.Contains(err.Error(), "call x") {
		t.Fatalf("want call error, got %v", err)
	}
}

func TestStdioCallToolDecodeError(t *testing.T) {
	// A non-object tool result envelope makes decodeToolResult fail.
	resp := `{"jsonrpc":"2.0","id":1,"result":"plain string"}` + "\n"
	c := newClientForTest("cd", nopWriteCloser{}, strings.NewReader(resp))
	if _, err := c.CallTool(context.Background(), "x", nil); err == nil || !strings.Contains(err.Error(), "tool result envelope") {
		t.Fatalf("want envelope decode error, got %v", err)
	}
}

func TestStderrTailEmpty(t *testing.T) {
	c := &Client{stderr: boundedbuffer.New(0)}
	if got := c.stderrTail(); got != "" {
		t.Fatalf("stderrTail = %q, want empty", got)
	}
}

func TestStderrTailCapsLength(t *testing.T) {
	c := &Client{stderr: boundedbuffer.New(0)}
	_, _ = c.stderr.Write([]byte(strings.Repeat("a", 500)))
	got := c.stderrTail()
	// Two leading chars are ": "; the payload is capped at 200.
	if len(got) > 202 {
		t.Fatalf("stderrTail length = %d, want <= 202", len(got))
	}
}
