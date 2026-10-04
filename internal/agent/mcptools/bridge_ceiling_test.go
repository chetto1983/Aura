package mcptools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// The agent loop moves a call still running after its window to the background and gives it
// a ceiling of its own (tools.BackgroundCalls). The bridge's per-call timeout belongs to the
// callers that wait on it; under that ceiling it would cut the moved call at 60 s again, the
// loss prd.md §15 measured.
func TestBridgedToolTakesTheCallersCeilingOverItsOwnTimeout(t *testing.T) {
	t.Setenv(envMCPCallTimeoutSec, "0.025")
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fixture", Version: "0.0.1"}, nil)
	server.AddTool(sandboxTools()[0], func(ctx context.Context, _ *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
		select {
		case <-time.After(150 * time.Millisecond):
			return &sdkmcp.CallToolResult{Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "generated"}}}, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	})
	clientTransport, serverTransport := sdkmcp.NewInMemoryTransports()
	ctx := context.Background()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect: %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	srv := NewMountedServer("fixture", nil)
	session, err := connectClient(ctx, clientTransport, mcpSessionOptionsFor(srv))
	if err != nil {
		t.Fatalf("client.Connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	srv.Attach(session)
	got, err := bridgeDefault(ctx, "sb", srv)
	if err != nil {
		t.Fatalf("bridgeDefault: %v", err)
	}
	callCtx := tools.WithToolCallContext(context.Background(), "sess", "tc1", t.TempDir(), 2048)

	if _, err := got[0].Execute(callCtx, json.RawMessage(`{"container_id":"abc"}`)); err == nil {
		t.Fatal("without a ceiling the 25 ms call timeout should have cut the 150 ms call")
	}
	res, err := got[0].Execute(tools.WithCallCeiling(callCtx, 2*time.Second), json.RawMessage(`{"container_id":"abc"}`))
	if err != nil || !strings.Contains(res.Preview, "generated") {
		t.Fatalf("under the ceiling Execute = %q, %v; want the server's answer", res.Preview, err)
	}
}
