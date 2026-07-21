package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
	if mode == "grandchild-child" {
		// A plain, indefinitely-alive descendant of the mounted helper, with no
		// stdio wiring of its own — the process-tree-kill regression target
		// (D-10/F-035): it must not survive its ancestor's Close(). It proves its
		// own liveness by appending a heartbeat line to a file every tick; a dead
		// process obviously cannot keep doing that.
		heartbeat := os.Getenv("AURA_MCP_HELPER_HEARTBEAT")
		for {
			if heartbeat != "" {
				if f, err := os.OpenFile(heartbeat, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600); err == nil {
					_, _ = f.WriteString("beat\n")
					_ = f.Close()
				}
			}
			time.Sleep(30 * time.Millisecond)
		}
	}
	if mode == "grandchild" {
		// Spawn a genuine OS-level grandchild (relative to the test process) by
		// re-execing this same test binary in "grandchild-child" mode, then behave
		// like "hang" below so Close() must escalate through its kill-after-timeout
		// path — exercising the actual process-tree-kill call, not just a graceful
		// parent exit that would leave the grandchild untouched.
		child := exec.Command(os.Args[0], "-test.run=TestHelperProcess")
		child.Env = append(os.Environ(), "AURA_MCP_HELPER=1", "AURA_MCP_HELPER_MODE=grandchild-child",
			"AURA_MCP_HELPER_HEARTBEAT="+os.Getenv("AURA_MCP_HELPER_HEARTBEAT"))
		if err := child.Start(); err != nil {
			fmt.Fprintln(os.Stderr, "boom: grandchild spawn failed:", err)
			os.Exit(4)
		}
		mode = "hang"
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

func TestOpenRejectsUnsafeCommandNameBeforeSpawn(t *testing.T) {
	t.Parallel()
	for _, command := range []string{
		"sh",
		"bash",
		"cmd.exe",
		"powershell",
		"foo bar",
		"../evil",
	} {
		t.Run(command, func(t *testing.T) {
			if _, err := Open(context.Background(), "unsafe", ServerConfig{Command: command}); err == nil || !strings.Contains(err.Error(), "unsafe command") {
				t.Fatalf("Open(%q) err = %v, want unsafe-command rejection", command, err)
			}
		})
	}
}

func TestCommandNameRegexAllowsWindowsRunnerShortPath(t *testing.T) {
	t.Parallel()
	command := `C:\Users\RUNNER~1\AppData\Local\Temp\go-build123\b001\aura.test.exe`
	if !mcpCommandNameRe.MatchString(command) {
		t.Fatalf("mcpCommandNameRe rejected Windows short path %q", command)
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

// TestCloseKillsGrandchildProcessTree is the D-10/F-035 regression guard: Close
// must terminate the WHOLE spawned process tree, not just the tracked PID. A
// single-PID kill (the pre-fix cmd.Process.Kill()) leaves the grandchild running
// indefinitely — proven here via a heartbeat file the grandchild can only keep
// growing while it is actually alive.
func TestCloseKillsGrandchildProcessTree(t *testing.T) {
	if testing.Short() {
		t.Skip("kill-after-timeout path waits closeWaitTimeout")
	}
	heartbeat := filepath.Join(t.TempDir(), "grandchild-heartbeat.txt")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cfg := helperServerConfig("grandchild")
	cfg.Env = append(cfg.Env, "AURA_MCP_HELPER_HEARTBEAT="+heartbeat)
	c, err := Open(ctx, "grandchild", cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	waitForHeartbeats(t, heartbeat, 2)

	// The parent ignores stdin close (mode was switched to "hang"), so Close must
	// escalate through closeWaitTimeout to the actual process-tree kill.
	_ = c.Close()

	sizeAfterClose := heartbeatSize(t, heartbeat)
	time.Sleep(300 * time.Millisecond) // several heartbeat ticks, if anything survived
	sizeAfterSettle := heartbeatSize(t, heartbeat)
	if sizeAfterSettle != sizeAfterClose {
		t.Fatalf("grandchild heartbeat grew after Close (%d -> %d bytes): process-tree kill failed, grandchild still running",
			sizeAfterClose, sizeAfterSettle)
	}
}

func waitForHeartbeats(t *testing.T, path string, minLines int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		data, err := os.ReadFile(path)
		if err == nil && strings.Count(string(data), "\n") >= minLines {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("heartbeat file %s did not reach %d lines in time (err=%v)", path, minLines, err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func heartbeatSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat heartbeat file: %v", err)
	}
	return info.Size()
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
