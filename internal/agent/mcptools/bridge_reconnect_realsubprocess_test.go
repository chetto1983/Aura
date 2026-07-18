package mcptools

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
)

// TestMCPHelperProcessForReconnect is not a real test: it is re-executed as a
// subprocess (the standard os/exec self-exec fake-command pattern, mirroring
// internal/mcp/client_open_test.go and cmd/aura/registry_test.go) so
// TestOpenReplacement_ReconnectedSubprocessSurvivesHandshakeCtxCancel can drive a
// REAL reconnect through the REAL (unstubbed) openMCPClient/mcp.OpenWithHandshakeContext,
// not a fakeReconnectClient double — the Pitfall #2 empirical check the plan
// requires needs exec.CommandContext's actual cancellation semantics in play.
func TestMCPHelperProcessForReconnect(t *testing.T) {
	if os.Getenv("AURA_MCPTOOLS_HELPER") != "1" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer func() { _ = writer.Flush() }()
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			return
		}
		var req map[string]any
		if jsonErr := json.Unmarshal(line, &req); jsonErr != nil {
			continue
		}
		method, _ := req["method"].(string)
		id := req["id"]
		switch method {
		case "initialize":
			writeReconnectHelperMsg(writer, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"protocolVersion": "2024-11-05"}})
		case "notifications/initialized":
			// no response
		case "tools/list":
			writeReconnectHelperMsg(writer, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"tools": []map[string]any{{"name": "echo", "description": "echo", "inputSchema": map[string]any{"type": "object"}}},
			}})
		case "ping":
			writeReconnectHelperMsg(writer, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		}
	}
}

func writeReconnectHelperMsg(w *bufio.Writer, v map[string]any) {
	enc, _ := json.Marshal(v)
	_, _ = w.Write(append(enc, '\n'))
	_ = w.Flush()
}

func reconnectHelperConfig() mcp.ServerConfig {
	return mcp.ServerConfig{
		Command: os.Args[0],
		Args:    []string{"-test.run=TestMCPHelperProcessForReconnect"},
		Env:     []string{"AURA_MCPTOOLS_HELPER=1"},
	}
}

// TestOpenReplacement_ReconnectedSubprocessSurvivesHandshakeCtxCancel is the
// plan-required empirical Pitfall #2 check for bridge_reconnect.go's
// openReplacement: after a successful reconnect, the REPLACEMENT subprocess must
// still be alive and answering calls once openReplacement's own bounded,
// deferred-cancel reconnect-handshake ctx has already fired. Before the fix,
// openReplacement passed the SAME bounded/deferred-cancel ctx as both the
// handshake bound AND (via openMCPClient -> mcp.Open) the subprocess's
// exec.CommandContext process-lifetime ctx — so the deferred cancel() at the end
// of openReplacement killed the just-reconnected subprocess the instant
// openReplacement returned. This test drives a REAL subprocess through the REAL
// (unstubbed) openMCPClient, not a fake double, so exec.CommandContext's actual
// cancellation semantics are exercised.
func TestOpenReplacement_ReconnectedSubprocessSurvivesHandshakeCtxCancel(t *testing.T) {
	failed := &errReconnectClient{listErrs: []error{fmt.Errorf("pipe: %w", mcp.ErrTransport)}}
	srv := newReconnectingServer("real", reconnectHelperConfig(), failed)
	srv.reconnectTimeout = 5 * time.Second // generous enough for a real subprocess spawn+handshake, still short for a settle wait
	t.Cleanup(func() { _ = srv.Close() })

	defs, err := srv.ListTools(context.Background())
	if err != nil {
		t.Fatalf("ListTools (triggers reconnect against the real subprocess): %v", err)
	}
	if len(defs) != 1 || defs[0].Name != "echo" {
		t.Fatalf("defs after reconnect = %v, want the real subprocess's echo tool", defs)
	}

	// Grab the raw reconnected client directly (bypassing reconnectingServer's OWN
	// self-healing ListTools wrapper, which would otherwise transparently reconnect
	// AGAIN on a transport error and mask exactly the failure this test targets).
	client, err := srv.currentClient()
	if err != nil {
		t.Fatalf("currentClient after reconnect: %v", err)
	}

	// Wait well past the reconnect handshake's own (already-fired) deadline.
	time.Sleep(2 * srv.reconnectTimeout)

	defs2, err := client.ListTools(context.Background())
	if err != nil {
		t.Fatalf("raw client ListTools after the settle window: %v (want the reconnected subprocess still alive, "+
			"not killed by its own bounded reconnect-handshake ctx firing — Pitfall #2)", err)
	}
	if len(defs2) != 1 || defs2[0].Name != "echo" {
		t.Fatalf("defs after settle = %v, want the real subprocess's echo tool still answering", defs2)
	}
}
