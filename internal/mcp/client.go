// Package mcp is a generic stdio MCP (Model Context Protocol) client: it spawns
// any MCP server that speaks JSON-RPC 2.0 over newline-delimited stdio, performs
// the initialize handshake, and exposes tools/list + tools/call. It is the reusable
// substrate behind Aura's MCP-to-agent-tool bridge for stdio MCP servers declared
// in the mcpServers config.
//
// Requests are serialized through a context-selectable session gate because a
// stdio pipe pair cannot interleave, and readResponse skips interleaved
// notifications (e.g. logging) so a chatty server cannot desync the
// request/response stream.
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
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chetto1983/aura/internal/boundedbuffer"
	"github.com/chetto1983/aura/internal/envutil"
	"github.com/chetto1983/aura/internal/procgroup"
)

// protocolVersion is the MCP revision Aura negotiates.
const protocolVersion = "2024-11-05"

// defaultStdioMaxFrame is the default cap, in bytes, on one stdio JSON-RPC frame's
// content — tunable via AURA_MCP_STDIO_MAX_FRAME (D-08). A hostile/misbehaving
// server that never terminates a line can no longer force unbounded memory growth
// (F-034): bufio.Scanner aborts deterministically at the cap instead.
const defaultStdioMaxFrame = 1 << 20 // 1 MiB

// ErrStdioFrameTooLarge marks a stdio frame that exceeded the configured cap. It
// wraps ErrTransport so IsTransportError is true and reconnectingServer
// (internal/agent/mcptools) tears the poisoned pipe down instead of resyncing it
// on the next call (D-09).
var ErrStdioFrameTooLarge = fmt.Errorf("%w: stdio frame exceeds cap", ErrTransport)

// newStdioScanner builds a bounded line scanner over r: maxFrame is the cap on one
// frame's CONTENT (the JSON payload, excluding the newline delimiter). The +1 gives
// bufio.Scanner room to additionally buffer the trailing delimiter byte itself —
// without it, a frame of exactly maxFrame content bytes would spuriously trip
// bufio.ErrTooLong one byte early (verified against go1.26.5's bufio/scan.go: the
// scanner's max token size bounds the whole buffered chunk needed to FIND the
// delimiter, which is one byte past the content).
func newStdioScanner(r io.Reader, maxFrame int) *bufio.Scanner {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 4096), maxFrame+1)
	return scanner
}

var mcpCommandNameRe = regexp.MustCompile(`^[A-Za-z0-9._:/\\~-]+$`)

// ToolAnnotations carries optional trust/action hints advertised by an MCP
// server for one tool.
type ToolAnnotations struct {
	ReadOnlyHint    bool  `json:"readOnlyHint"`
	DestructiveHint *bool `json:"destructiveHint"`
}

// ToolDef is one entry from tools/list: the LLM-facing name, description, raw
// JSON-Schema, and annotations the bridge translates into an Aura tool schema.
type ToolDef struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	InputSchema json.RawMessage `json:"inputSchema"`
	Annotations ToolAnnotations `json:"annotations"`
}

// Client wraps one MCP server subprocess. The zero value is unusable; use Open.
type Client struct {
	name            string
	cmd             *exec.Cmd
	stdin           io.WriteCloser
	stdout          *bufio.Scanner
	stdoutCloser    io.Closer
	stderr          *boundedbuffer.Buffer
	gate            sessionGate
	stdinCloseOnce  sync.Once
	stdoutCloseOnce sync.Once
	closeOnce       sync.Once
	closeErr        error
	callTimeout     time.Duration
	nextID          atomic.Int64
}

// Open spawns the server described by cfg and completes the initialize handshake.
// name is a short label used in error messages (the mcpServers key). On any
// failure the subprocess is reaped before returning. ctx bounds BOTH the
// subprocess's whole lifetime and the initialize handshake — callers that need to
// bound ONLY the handshake (so a slow-but-eventually-healthy server is not killed
// once an unrelated mount deadline elapses, Pitfall #2) must use
// OpenWithHandshakeContext instead.
func Open(ctx context.Context, name string, cfg ServerConfig) (*Client, error) {
	return OpenWithHandshakeContext(ctx, ctx, name, cfg)
}

// OpenWithHandshakeContext spawns the server described by cfg like Open, but splits
// the single lifetime context into two: processCtx bounds exec.CommandContext (the
// subprocess's ENTIRE lifetime — callers must pass a long-lived, non-deferred-cancel
// context here, e.g. the daemon's boot context, never a short-lived per-attempt one)
// while handshakeCtx bounds ONLY the initialize round-trip. A handshake
// failure/timeout closes (reaps) the just-spawned subprocess and returns the error
// without touching processCtx, so no other server sharing it is affected (the
// load-bearing Pitfall #2 fix: a single bounded context doubling as both would
// silently kill every healthy server once its handshake deadline later elapsed).
func OpenWithHandshakeContext(processCtx, handshakeCtx context.Context, name string, cfg ServerConfig) (*Client, error) {
	command := strings.TrimSpace(cfg.Command)
	if command == "" {
		return nil, fmt.Errorf("mcp %q: empty command", name)
	}
	if !mcpCommandNameRe.MatchString(command) {
		return nil, fmt.Errorf("mcp %q: unsafe command %q: use a single executable name or absolute path without shell metacharacters", name, cfg.Command)
	}
	if commandHasPathSeparator(command) && !filepath.IsAbs(command) {
		return nil, fmt.Errorf("mcp %q: unsafe command %q: relative executable paths are not allowed", name, cfg.Command)
	}
	if isShellLauncher(command) {
		return nil, fmt.Errorf("mcp %q: unsafe command %q: shell interpreters are not allowed for stdio MCP launch", name, cfg.Command)
	}
	// G204: Command/Args/Env come from the operator-controlled mcpServers config
	// (.env / config file), not from untrusted model output.
	cmd := exec.CommandContext(processCtx, command, cfg.Args...) //nolint:gosec
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
	// D-10: cmd leads its own process group/tree before it starts, so a later
	// killProcess (via procgroup.KillProcessGroup) reaps the WHOLE spawned tree —
	// not just this tracked PID — instead of leaking grandchild processes (F-035).
	procgroup.SetProcessGroup(cmd)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("mcp %q: spawn %s: %w", name, cfg.Command, err)
	}
	maxFrame := envutil.IntDefault("AURA_MCP_STDIO_MAX_FRAME", defaultStdioMaxFrame)
	c := &Client{
		name:         name,
		cmd:          cmd,
		stdin:        stdin,
		stdout:       newStdioScanner(stdoutPipe, maxFrame),
		stdoutCloser: stdoutPipe,
		stderr:       stderr,
		callTimeout:  cfg.CallTimeout,
	}
	if err := c.initializeContext(handshakeCtx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func commandHasPathSeparator(command string) bool {
	return strings.ContainsAny(command, `/\`)
}

func isShellLauncher(command string) bool {
	base := filepath.Base(strings.ReplaceAll(command, `\`, string(filepath.Separator)))
	switch strings.ToLower(base) {
	case "sh", "bash", "dash", "zsh", "fish", "cmd", "cmd.exe", "powershell", "powershell.exe", "pwsh", "pwsh.exe":
		return true
	default:
		return false
	}
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

func (c *Client) initializeContext(ctx context.Context) (err error) {
	ctx, end := stdioInitializeBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	res, err := c.roundtripContext(ctx, "initialize", map[string]any{
		"protocolVersion": protocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "aura", "version": "0.7"},
	})
	if err != nil {
		return fmt.Errorf("mcp %q: initialize: %w", c.name, err)
	}
	if _, err := validateInitializeResult(res); err != nil {
		return fmt.Errorf("mcp %q: initialize contract: %w", c.name, err)
	}
	if err := c.notify("notifications/initialized"); err != nil {
		return fmt.Errorf("mcp %q: initialized notification: %w", c.name, err)
	}
	return nil
}

// ListTools returns the server's advertised tools (tools/list).
func (c *Client) ListTools(ctx context.Context) (defs []ToolDef, err error) {
	ctx, end := stdioListBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	ctx, cancel := c.boundCallContext(ctx)
	defer cancel()
	callCtx, release, err := c.gate.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return listToolsWith(callCtx, c.name, c.roundtripContext)
}

// CallTool invokes one tool and returns its concatenated text content. A tool that
// reports isError=true is returned as an error carrying that text, so the caller
// never mistakes a tool-level failure for success.
func (c *Client) CallTool(ctx context.Context, name string, args map[string]any) (text string, err error) {
	ctx, end := stdioCallBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	ctx, cancel := c.boundCallContext(ctx)
	defer cancel()
	callCtx, release, err := c.gate.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return callToolWith(callCtx, c.name, name, args, c.roundtripContext)
}

// Ping issues an MCP ping round-trip to confirm the subprocess is alive and
// responsive; used by the bridge as a liveness probe.
func (c *Client) Ping(ctx context.Context) (err error) {
	ctx, end := stdioPingBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	ctx, cancel := c.boundCallContext(ctx)
	defer cancel()
	callCtx, release, err := c.gate.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	_, err = c.roundtripContext(callCtx, "ping", map[string]any{})
	if err != nil {
		return fmt.Errorf("mcp %q: ping: %w", c.name, err)
	}
	return nil
}

func (c *Client) boundCallContext(ctx context.Context) (context.Context, context.CancelFunc) {
	if c.callTimeout > 0 {
		return context.WithTimeout(ctx, c.callTimeout)
	}
	return context.WithCancel(ctx)
}

// roundtrip writes one request and reads the matching response, skipping any
// interleaved notifications. Caller owns the session gate (except initialize,
// which runs before the client is shared).
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
		if !c.stdout.Scan() {
			err := c.stdout.Err()
			if errors.Is(err, bufio.ErrTooLong) {
				// D-09: an over-cap frame aborts the whole transport deterministically
				// (kill+close) — the request/response stream is desynced and must never
				// be trusted for a subsequent call.
				c.abortTransport()
				return nil, fmt.Errorf("%w: %w%s", ErrStdioFrameTooLarge, err, c.stderrTail())
			}
			if err != nil {
				return nil, fmt.Errorf("%w: recv: %w%s", ErrTransport, err, c.stderrTail())
			}
			// Scan()==false with Err()==nil is bufio.Scanner's "clean EOF" (it treats a
			// closed reader as a normal end of input); for this always-newline-delimited
			// protocol a closed stdout ALWAYS means the peer is gone, so synthesize a
			// transport error instead of silently returning (nil, nil) (Pitfall #4).
			return nil, fmt.Errorf("%w: recv: %w%s", ErrTransport, io.ErrUnexpectedEOF, c.stderrTail())
		}
		line := c.stdout.Bytes()
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
		StructuredContent json.RawMessage `json:"structuredContent"`
		IsError           bool            `json:"isError"`
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
	text = strings.TrimRight(b.String(), "\n")
	if !env.IsError && explicitDomainFailure(env.StructuredContent, text) {
		env.IsError = true
	}
	return text, env.IsError, nil
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
	closeCtx, cancel := context.WithTimeout(context.Background(), closeWaitTimeout)
	defer cancel()
	c.gate.beginClose()
	c.closeStdin()
	defer c.closeStdout()
	if err := c.gate.waitIdle(closeCtx); err != nil {
		c.killProcess()
		return fmt.Errorf("mcp %q: close active request: %w", c.name, err)
	}
	if c.cmd == nil {
		return nil
	}
	done := make(chan error, 1)
	go func() { done <- c.cmd.Wait() }()
	select {
	case err := <-done:
		return err
	case <-closeCtx.Done():
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

// killProcess terminates the whole spawned process tree (D-10), not just the
// tracked PID — see internal/procgroup for the per-OS mechanism.
func (c *Client) killProcess() {
	if c.cmd == nil {
		return
	}
	_ = procgroup.KillProcessGroup(c.cmd)
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
