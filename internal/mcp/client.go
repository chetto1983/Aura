// Package mcp is a generic stdio MCP (Model Context Protocol) client: it spawns
// any MCP server that speaks JSON-RPC 2.0 over newline-delimited stdio, performs
// the initialize handshake, and exposes tools/list + tools/call. It is the reusable
// substrate behind Aura's MCP-to-agent-tool bridge for stdio MCP servers declared
// in the mcpServers config.
//
// It generalizes the single-purpose mcp-neo4j-cypher client (internal/knowledge):
// the framing (one JSON object per line, serialized via mu since a stdio pipe pair
// cannot interleave) is identical, but the server, tool set, and arguments are not
// hard-coded. Unlike that client, readResponse skips interleaved notifications
// (e.g. logging) so a chatty server cannot desync the request/response stream.
//
// A subprocess crash is surfaced as a wrapped error; the bridge decides policy.
package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// protocolVersion is the MCP revision Aura negotiates.
const protocolVersion = "2024-11-05"

// ServerConfig declares how to launch one stdio MCP server (Claude-Desktop shape).
// Env entries ("KEY=value") are appended to the inherited environment.
type ServerConfig struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Env     []string `json:"env,omitempty"`
}

// ToolDef is one entry from tools/list: the LLM-facing name, description, and the
// raw JSON-Schema the bridge translates into an Aura tool schema.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
}

// Client wraps one MCP server subprocess. The zero value is unusable; use Open.
type Client struct {
	name   string
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *safeBuffer
	mu     sync.Mutex
	nextID atomic.Int64
}

// Open spawns the server described by cfg and completes the initialize handshake.
// name is a short label used in error messages (the mcpServers key). On any
// failure the subprocess is reaped before returning.
func Open(ctx context.Context, name string, cfg ServerConfig) (*Client, error) {
	if strings.TrimSpace(cfg.Command) == "" {
		return nil, fmt.Errorf("mcp %q: empty command", name)
	}
	// G204: Command/Args/Env come from the operator-controlled mcpServers config
	// (.env / config file), not from untrusted model output.
	cmd := exec.CommandContext(ctx, cfg.Command, cfg.Args...) //nolint:gosec
	cmd.Env = append(os.Environ(), cfg.Env...)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %q: stdin pipe: %w", name, err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %q: stdout pipe: %w", name, err)
	}
	stderr := &safeBuffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp %q: spawn %s: %w", name, cfg.Command, err)
	}
	c := &Client{
		name:   name,
		cmd:    cmd,
		stdin:  stdin,
		stdout: bufio.NewReader(stdoutPipe),
		stderr: stderr,
	}
	if err := c.initialize(); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

type rpcReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// initialize performs the MCP lifecycle handshake (initialize request/response
// then the initialized notification). Runs once at Open before the client is
// shared, so it needs no lock.
func (c *Client) initialize() error {
	res, err := c.roundtrip("initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "aura", "version": "0.7"},
	})
	if err != nil {
		return fmt.Errorf("mcp %q: initialize: %w", c.name, err)
	}
	_ = res
	if err := c.notify("notifications/initialized"); err != nil {
		return fmt.Errorf("mcp %q: initialized notification: %w", c.name, err)
	}
	return nil
}

// ListTools returns the server's advertised tools (tools/list).
func (c *Client) ListTools(ctx context.Context) ([]ToolDef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.roundtrip("tools/list", map[string]any{})
	if err != nil {
		return nil, fmt.Errorf("mcp %q: tools/list: %w", c.name, err)
	}
	var env struct {
		Tools []ToolDef `json:"tools"`
	}
	if err := json.Unmarshal(res, &env); err != nil {
		return nil, fmt.Errorf("mcp %q: decode tools/list: %w", c.name, err)
	}
	return env.Tools, nil
}

// CallTool invokes one tool and returns its concatenated text content. A tool that
// reports isError=true is returned as an error carrying that text, so the caller
// never mistakes a tool-level failure for success.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if args == nil {
		args = map[string]any{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.roundtrip("tools/call", map[string]any{"name": name, "arguments": args})
	if err != nil {
		return "", fmt.Errorf("mcp %q: call %s: %w", c.name, name, err)
	}
	text, isErr, derr := decodeToolResult(res)
	if derr != nil {
		return "", fmt.Errorf("mcp %q: call %s: %w", c.name, name, derr)
	}
	if isErr {
		return "", fmt.Errorf("mcp %q: tool %s reported error: %s", c.name, name, text)
	}
	return text, nil
}

// Ping issues an MCP ping round-trip to confirm the subprocess is alive and
// responsive; used by the bridge as a liveness probe.
func (c *Client) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.roundtrip("ping", map[string]any{})
	if err != nil {
		return fmt.Errorf("mcp %q: ping: %w", c.name, err)
	}
	return nil
}

// roundtrip writes one request and reads the matching response, skipping any
// interleaved notifications. Caller holds mu (except initialize, pre-share).
func (c *Client) roundtrip(method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	enc, err := json.Marshal(rpcReq{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", method, err)
	}
	if _, err := fmt.Fprintln(c.stdin, string(enc)); err != nil {
		return nil, fmt.Errorf("send %s: %w%s", method, err, c.stderrTail())
	}
	return c.readResponse(id)
}

// notify sends a fire-and-forget notification (no id, no response expected).
func (c *Client) notify(method string) error {
	enc, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(c.stdin, string(enc))
	return err
}

// readResponse reads lines until it finds the response whose id matches want,
// discarding notifications (messages with no id) that the server may interleave.
func (c *Client) readResponse(want int64) (json.RawMessage, error) {
	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("recv: %w%s", err, c.stderrTail())
		}
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var resp rpcResp
		if err := json.Unmarshal(line, &resp); err != nil {
			return nil, fmt.Errorf("decode response: %w", err)
		}
		if resp.ID == nil || *resp.ID != want {
			continue // a notification or an out-of-band/earlier id — skip
		}
		if resp.Error != nil {
			return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
		}
		return resp.Result, nil
	}
}

// decodeToolResult extracts the text content from a tools/call result envelope:
// {"content":[{"type":"text","text":"..."}], "isError": bool}. All text parts are
// concatenated; isError surfaces as the second return.
func decodeToolResult(result json.RawMessage) (text string, isError bool, err error) {
	if len(result) == 0 {
		return "", false, nil
	}
	var env struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &env); err != nil {
		return "", false, fmt.Errorf("decode tool result envelope: %w", err)
	}
	var b strings.Builder
	for _, part := range env.Content {
		if part.Type == "text" {
			b.WriteString(part.Text)
		}
	}
	return strings.TrimRight(b.String(), "\n"), env.IsError, nil
}

// closeWaitTimeout bounds how long Close waits for a server to exit after its
// stdin closes before escalating to Kill. Servers that ignore stdin-close exist
// in the wild (the WSL-spawned whatsapp-mcp `wsl.exe` chain blocked a live run
// for 13 minutes until the test framework panicked, 2026-06-04) — an unbounded
// cmd.Wait turns one misbehaving sidecar into a process-wide hang.
const closeWaitTimeout = 5 * time.Second

// Close shuts the subprocess down by closing stdin and waiting for exit; if the
// server has not exited within closeWaitTimeout it is killed and the wait result
// is drained. Safe to call on a test client (no cmd) and idempotent enough for
// defer.
func (c *Client) Close() error {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(closeWaitTimeout):
		if c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		return <-done // Wait must still be drained after Kill to release resources
	}
}

// stderrTail returns a length-capped suffix of captured stderr for error context.
func (c *Client) stderrTail() string {
	s := strings.TrimSpace(c.stderr.String())
	if s == "" {
		return ""
	}
	if len(s) > 200 {
		s = s[len(s)-200:]
	}
	s = RedactSecrets(s)
	return ": " + s
}

// safeBuffer is a mutex-guarded bytes.Buffer usable as cmd.Stderr while the error
// path reads it concurrently.
type safeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *safeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *safeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}
