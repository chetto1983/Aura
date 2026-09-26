package mcp

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestProtocolLinesPassesJSONRPCAndDropsTheRest(t *testing.T) {
	var out, logged bytes.Buffer
	w := &protocolLines{dst: &out, name: "fetch", logger: slog.New(slog.NewTextHandler(&logged, nil))}
	for _, chunk := range []string{
		"added 39 pack", "ages\n", "\n", `{"jsonrpc":"2.0",`, `"id":1}` + "\n",
		"  [{\"jsonrpc\":\"2.0\",\"id\":2}]\r\n", "found 0 vulnerabilities\n", `{"jsonrpc":"2.0","id":3}`,
	} {
		if n, err := w.Write([]byte(chunk)); err != nil || n != len(chunk) {
			t.Fatalf("Write(%q) = %d, %v", chunk, n, err)
		}
	}
	want := `{"jsonrpc":"2.0","id":1}` + "\n" + "  [{\"jsonrpc\":\"2.0\",\"id\":2}]\r\n"
	if out.String() != want {
		t.Fatalf("passed %q, want %q (the unterminated id 3 waits for its newline)", out.String(), want)
	}
	for _, dropped := range []string{"added 39 packages", "found 0 vulnerabilities"} {
		if !strings.Contains(logged.String(), dropped) {
			t.Errorf("dropped line %q was not logged:\n%s", dropped, logged.String())
		}
	}
	if strings.Count(logged.String(), "non-protocol line") != 2 {
		t.Errorf("a blank line was logged as noise:\n%s", logged.String())
	}
}

func TestProtocolLinesLeavesAnOverlongLineToTheTransport(t *testing.T) {
	var out bytes.Buffer
	w := &protocolLines{dst: &out, name: "big", logger: slog.New(slog.DiscardHandler)}
	huge := bytes.Repeat([]byte("x"), sdkmcp.DefaultMaxLineLength+1)
	if _, err := w.Write(huge); err != nil {
		t.Fatal(err)
	}
	if out.Len() != len(huge) || len(w.buf) != 0 {
		t.Fatalf("forwarded %d bytes, kept %d: an unterminated line past the limit must not grow the buffer", out.Len(), len(w.buf))
	}
}
