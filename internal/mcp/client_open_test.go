package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestHelperProcess is not a real test: it is re-executed as a subprocess by the
// Open/Close tests (the standard os/exec self-exec fake-command pattern) to act as
// a minimal scripted stdio MCP server, so Open can spawn a real process and run the
// initialize handshake without any external dependency.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("AURA_MCP_HELPER") != "1" {
		return
	}
	mode := os.Getenv("AURA_MCP_HELPER_MODE")
	if mode == "crash" {
		// Exit immediately so cmd.Start succeeds but the handshake read fails.
		fmt.Fprintln(os.Stderr, "boom: helper crashed")
		os.Exit(3)
	}
	reader := bufio.NewReader(os.Stdin)
	writer := bufio.NewWriter(os.Stdout)
	defer func() { _ = writer.Flush() }()
	for {
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if mode == "hang" {
				// Ignore stdin close to force Close into its kill-after-timeout
				// path. A bare select{} would trip the runtime deadlock detector
				// and exit (closing stdout); a long sleep keeps a live goroutine
				// so the process truly hangs until Close kills it.
				time.Sleep(time.Hour)
			}
			return // stdin closed: exit cleanly
		}
		if len(strings.TrimSpace(string(line))) == 0 {
			continue
		}
		var req map[string]any
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		method, _ := req["method"].(string)
		id := req["id"]
		switch method {
		case "initialize":
			writeHelper(writer, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{"protocolVersion": protocolVersion}})
		case "notifications/initialized":
			// no response
		case "tools/list":
			if mode == "hang" {
				time.Sleep(time.Hour)
				continue
			}
			writeHelper(writer, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"tools": []map[string]any{{"name": "echo", "description": "echo", "inputSchema": map[string]any{"type": "object"}}}}})
		case "tools/call":
			writeHelper(writer, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{
				"content": []map[string]any{{"type": "text", "text": "ok"}}}})
		case "ping":
			writeHelper(writer, map[string]any{"jsonrpc": "2.0", "id": id, "result": map[string]any{}})
		}
	}
}

func writeHelper(w *bufio.Writer, v map[string]any) {
	enc, _ := json.Marshal(v)
	_, _ = w.Write(append(enc, '\n'))
	_ = w.Flush()
}

// helperServerConfig builds a ServerConfig that re-execs the test binary as the
// fake MCP server in the given mode ("" = normal, "crash", "hang").
func helperServerConfig(mode string) ServerConfig {
	args := []string{"-test.run=TestHelperProcess"}
	env := []string{"AURA_MCP_HELPER=1"}
	if mode != "" {
		env = append(env, "AURA_MCP_HELPER_MODE="+mode)
	}
	return ServerConfig{Command: os.Args[0], Args: args, Env: env}
}

func TestOpenEmptyCommand(t *testing.T) {
	if _, err := Open(context.Background(), "blank", ServerConfig{Command: "   "}); err == nil {
		t.Fatal("want error for empty command")
	}
}

func TestOpenSpawnFailure(t *testing.T) {
	_, err := Open(context.Background(), "nope", ServerConfig{Command: "this-binary-does-not-exist-12345"})
	if err == nil || !strings.Contains(err.Error(), "spawn") {
		t.Fatalf("want spawn error, got %v", err)
	}
}

func TestOpenInitializeFailureReapsProcess(t *testing.T) {
	// A server that exits immediately makes the handshake read fail; Open must
	// return an error after reaping the subprocess.
	_, err := Open(context.Background(), "crash", helperServerConfig("crash"))
	if err == nil || !strings.Contains(err.Error(), "initialize") {
		t.Fatalf("want initialize error, got %v", err)
	}
}

func TestOpenCloseRoundTripRealSubprocess(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := Open(ctx, "helper", helperServerConfig(""))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	tools, err := c.ListTools(ctx)
	if err != nil {
		t.Fatalf("ListTools: %v", err)
	}
	if len(tools) != 1 || tools[0].Name != "echo" {
		t.Fatalf("tools = %#v", tools)
	}
	out, err := c.CallTool(ctx, "echo", map[string]any{"text": "hi"})
	if err != nil {
		t.Fatalf("CallTool: %v", err)
	}
	if out != "ok" {
		t.Fatalf("CallTool = %q, want ok", out)
	}
	if err := c.Ping(ctx); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	// Close on a normal server returns the subprocess wait result (nil on exit 0).
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestCloseKillsHangingSubprocess(t *testing.T) {
	if testing.Short() {
		t.Skip("kill-after-timeout path waits closeWaitTimeout")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := Open(ctx, "hang", helperServerConfig("hang"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	start := time.Now()
	// The hang server ignores stdin close, so Close must escalate to Kill after
	// closeWaitTimeout and drain the wait; the returned error is the kill signal.
	_ = c.Close()
	if elapsed := time.Since(start); elapsed < closeWaitTimeout {
		t.Fatalf("Close returned in %v, expected to wait ~%v before kill", elapsed, closeWaitTimeout)
	}
}

func TestCloseNilCmdIsNoop(t *testing.T) {
	c := newClientForTest("noop", nopWriteCloser{}, strings.NewReader(""))
	if err := c.Close(); err != nil {
		t.Fatalf("Close on test client = %v, want nil", err)
	}
}

type nopWriteCloser struct{}

func (nopWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriteCloser) Close() error                { return nil }
