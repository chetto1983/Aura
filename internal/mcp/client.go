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
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chetto1983/aura/internal/boundedbuffer"
)

// protocolVersion is the MCP revision Aura negotiates.
const protocolVersion = "2024-11-05"

// ErrTransport marks a broken MCP transport pipe/session. Callers use
// IsTransportError rather than matching opaque OS error strings.
var ErrTransport = errors.New("mcp transport error")

// IsTransportError reports whether err wraps ErrTransport.
func IsTransportError(err error) bool {
	return errors.Is(err, ErrTransport)
}

// ServerConfig declares how to launch one stdio MCP server (Claude-Desktop shape).
// Env entries ("KEY=value") are explicit operator-declared child environment.
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
	name            string
	cmd             *exec.Cmd
	stdin           io.WriteCloser
	stdout          *bufio.Reader
	stdoutCloser    io.Closer
	stderr          *boundedbuffer.Buffer
	mu              sync.Mutex
	stdinCloseOnce  sync.Once
	stdoutCloseOnce sync.Once
	closeOnce       sync.Once
	closeErr        error
	nextID          atomic.Int64
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
	cmd.Env = processEnvForMCP(cfg.Env)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %q: stdin pipe: %w", name, err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("mcp %q: stdout pipe: %w", name, err)
	}
	stderr := boundedbuffer.New(0)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp %q: spawn %s: %w", name, cfg.Command, err)
	}
	c := &Client{
		name:         name,
		cmd:          cmd,
		stdin:        stdin,
		stdout:       bufio.NewReader(stdoutPipe),
		stdoutCloser: stdoutPipe,
		stderr:       stderr,
	}
	if err := c.initializeContext(ctx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func processEnvForMCP(configured []string) []string {
	env := make([]string, 0, len(configured)+8)
	seen := map[string]struct{}{}
	for _, kv := range os.Environ() {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || !mcpInheritedEnvKey(k) || secretEnvKey(k) {
			continue
		}
		upper := strings.ToUpper(k)
		if _, dup := seen[upper]; dup {
			continue
		}
		seen[upper] = struct{}{}
		env = append(env, kv)
	}
	for _, kv := range configured {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || strings.TrimSpace(k) == "" {
			continue
		}
		upper := strings.ToUpper(k)
		if _, dup := seen[upper]; dup {
			env = replaceEnv(env, k, kv)
		} else {
			env = append(env, kv)
		}
		seen[upper] = struct{}{}
	}
	return env
}

func mcpInheritedEnvKey(key string) bool {
	switch strings.ToUpper(key) {
	case "PATH", "PATHEXT", "SYSTEMROOT", "WINDIR", "COMSPEC", "HOME", "USERPROFILE", "TMP", "TEMP", "TMPDIR", "LANG", "LC_ALL", "SSL_CERT_FILE", "SSL_CERT_DIR":
		return true
	default:
		return false
	}
}

func secretEnvKey(key string) bool {
	k := strings.ToLower(key)
	for _, marker := range []string{"token", "secret", "password", "passwd", "api_key", "apikey", "auth", "bearer", "credential", "key"} {
		if strings.Contains(k, marker) {
			return true
		}
	}
	return false
}

func replaceEnv(env []string, key, kv string) []string {
	want := strings.ToUpper(key)
	for i, existing := range env {
		k, _, ok := strings.Cut(existing, "=")
		if ok && strings.ToUpper(k) == want {
			env[i] = kv
			return env
		}
	}
	return append(env, kv)
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
	return c.initializeContext(context.Background())
}

func (c *Client) initializeContext(ctx context.Context) error {
	res, err := c.roundtripContext(ctx, "initialize", map[string]any{
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
	return listToolsWith(ctx, c.name, c.roundtripContext)
}

// CallTool invokes one tool and returns its concatenated text content. A tool that
// reports isError=true is returned as an error carrying that text, so the caller
// never mistakes a tool-level failure for success.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return callToolWith(ctx, c.name, name, args, c.roundtripContext)
}

// Ping issues an MCP ping round-trip to confirm the subprocess is alive and
// responsive; used by the bridge as a liveness probe.
func (c *Client) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	_, err := c.roundtripContext(ctx, "ping", map[string]any{})
	if err != nil {
		return fmt.Errorf("mcp %q: ping: %w", c.name, err)
	}
	return nil
}

// roundtrip writes one request and reads the matching response, skipping any
// interleaved notifications. Caller holds mu (except initialize, pre-share).
func (c *Client) roundtrip(method string, params any) (json.RawMessage, error) {
	return c.roundtripContext(context.Background(), method, params)
}

func (c *Client) roundtripContext(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: %s canceled before send: %w", ErrTransport, method, err)
	}
	id := c.nextID.Add(1)
	enc, err := json.Marshal(rpcReq{JSONRPC: "2.0", ID: id, Method: method, Params: params})
	if err != nil {
		return nil, fmt.Errorf("encode %s: %w", method, err)
	}
	if _, err := fmt.Fprintln(c.stdin, string(enc)); err != nil {
		return nil, fmt.Errorf("%w: send %s: %w%s", ErrTransport, method, err, c.stderrTail())
	}
	return c.readResponseContext(ctx, id)
}

// notify sends a fire-and-forget notification (no id, no response expected).
func (c *Client) notify(method string) error {
	enc, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": method})
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(c.stdin, string(enc))
	if err != nil {
		return fmt.Errorf("%w: notify %s: %w", ErrTransport, method, err)
	}
	return nil
}

// readResponse reads lines until it finds the response whose id matches want,
// discarding notifications (messages with no id) that the server may interleave.
func (c *Client) readResponse(want int64) (json.RawMessage, error) {
	return c.readResponseContext(context.Background(), want)
}

func (c *Client) readResponseContext(ctx context.Context, want int64) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("%w: recv: %w%s", ErrTransport, err, c.stderrTail())
	}
	type readResult struct {
		res json.RawMessage
		err error
	}
	done := make(chan readResult, 1)
	go func() {
		res, err := c.readResponseBlocking(want)
		done <- readResult{res: res, err: err}
	}()
	select {
	case rr := <-done:
		return rr.res, rr.err
	case <-ctx.Done():
		c.abortTransport()
		select {
		case <-done:
		case <-time.After(100 * time.Millisecond):
		}
		return nil, fmt.Errorf("%w: recv timeout: %w%s", ErrTransport, ctx.Err(), c.stderrTail())
	}
}

func (c *Client) readResponseBlocking(want int64) (json.RawMessage, error) {
	for {
		line, err := c.stdout.ReadBytes('\n')
		if err != nil {
			return nil, fmt.Errorf("%w: recv: %w%s", ErrTransport, err, c.stderrTail())
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
	c.closeOnce.Do(func() {
		c.closeErr = c.close()
	})
	return c.closeErr
}

func (c *Client) close() error {
	c.closeStdin()
	defer c.closeStdout()
	if c.cmd == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(closeWaitTimeout):
		c.killProcess()
		return <-done // Wait must still be drained after Kill to release resources
	}
}

func (c *Client) closePipes() {
	c.closeStdin()
	c.closeStdout()
}

func (c *Client) closeStdin() {
	c.stdinCloseOnce.Do(func() {
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
	})
}

func (c *Client) closeStdout() {
	c.stdoutCloseOnce.Do(func() {
		if c.stdoutCloser != nil {
			_ = c.stdoutCloser.Close()
		}
	})
}

func (c *Client) abortTransport() {
	c.closePipes()
	c.killProcess()
}

func (c *Client) killProcess() {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
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
