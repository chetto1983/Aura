// MCP subprocess client for mcp-neo4j-cypher (Pattern 3). Aura never links a
// native Go Neo4j driver (CLAUDE.md §Project scope discipline ban); every Cypher
// call rides a stdio JSON-RPC 2.0 channel to the subprocess, which holds the
// only bolt connection. Requests are serialized through mu since a single stdio
// pipe pair cannot interleave. D-06: a subprocess crash is unrecoverable — we
// surface a wrapped error and let the caller fail the Aura process; no restart,
// no graceful degrade.
package knowledge

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chetto1983/aura/internal/boundedbuffer"
)

const (
	toolRead  = "read_neo4j_cypher"
	toolWrite = "write_neo4j_cypher"
	// crashHint is the D-06 load-bearing literal asserted by TestMCPCrash_FailsAura.
	crashHint = "MCP may have crashed — D-06 policy: fail Aura process"
	// cypherCallTimeout bounds a single Cypher read so a hung mcp-neo4j-cypher (one that
	// never writes a response line) cannot block ReadBytes — and every other graph op
	// serialized behind mu — forever (Wave 0.3). It is deliberately generous so a healthy
	// but slow query never trips it; only a genuine hang exceeds it, on which we kill the
	// subprocess (D-06 fail-closed). Caller-ctx CANCELLATION is intentionally NOT a kill
	// trigger (see Cypher): a client disconnect must never take down the shared subprocess
	// mid-flight for every other caller — only this hang ceiling can.
	cypherCallTimeout = 120 * time.Second
)

// pwAssignRE masks `password=...` / `pass: ...` assignments in captured stderr
// (T-1.07-04). The literal cfg.Password is masked separately in redactSecrets.
var pwAssignRE = regexp.MustCompile(`(?i)(password|pass)(\s*[=:]\s*)\S+`)

// Client wraps the mcp-neo4j-cypher subprocess. The zero value is unusable; use Open.
type Client struct {
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      *bufio.Reader
	stderr      *boundedbuffer.Buffer
	password    string
	callTimeout time.Duration // per-call read hang ceiling; 0 falls back to cypherCallTimeout
	mu          sync.Mutex
	nextID      atomic.Int64
}

// Open spawns mcp-neo4j-cypher in stdio transport mode against cfg's bolt
// endpoint. On spawn failure the error carries the literal install hint so an
// operator missing the binary on PATH gets an actionable message.
func Open(ctx context.Context, cfg *Config) (*Client, error) {
	args := []string{
		"--db-url", cfg.BoltURL,
		"--username", cfg.User,
		"--password", cfg.Password,
		"--database", cfg.Database,
		"--transport", "stdio",
	}
	// G204: MCPBinary is an operator-configured path (AURA_MCP_NEO4J_CYPHER_BIN),
	// resolved from .env / config — not attacker-controlled user input.
	cmd := exec.Command(cfg.MCPBinary, args...) //nolint:gosec
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr := boundedbuffer.New(0)
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("spawn %s: %w (PATH check: pip install mcp-neo4j-cypher==0.6.0)", cfg.MCPBinary, err)
	}
	c := &Client{
		cmd:         cmd,
		stdin:       stdin,
		stdout:      bufio.NewReader(stdoutPipe),
		stderr:      stderr,
		password:    cfg.Password,
		callTimeout: cypherCallTimeout,
	}
	// mcp-neo4j-cypher (FastMCP) rejects tools/call until the MCP lifecycle
	// handshake completes. Verified by the Wave 0 probe (Assumption A10).
	initCtx, cancel := connectContext(ctx, cfg)
	defer cancel()
	if err := c.initializeWithContext(initCtx); err != nil {
		_ = c.Close()
		return nil, err
	}
	return c, nil
}

func (c *Client) initializeWithContext(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- c.initialize()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		select {
		case err := <-done:
			if err != nil {
				return fmt.Errorf("initialize timeout: %w; after kill: %v", ctx.Err(), err)
			}
		case <-time.After(time.Second):
		}
		return fmt.Errorf("initialize timeout: %w", ctx.Err())
	}
}

// initialize performs the MCP handshake: an `initialize` request/response then
// an `initialized` notification. Runs once at Open before any Cypher call, so it
// needs no lock (the client is not yet shared).
func (c *Client) initialize() error {
	req := rpcReq{
		JSONRPC: "2.0",
		ID:      c.nextID.Add(1),
		Method:  "initialize",
		Params: map[string]any{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]any{},
			"clientInfo":      map[string]any{"name": "aura", "version": "0.7"},
		},
	}
	enc, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("encode initialize: %w", err)
	}
	if _, err := fmt.Fprintln(c.stdin, string(enc)); err != nil {
		return fmt.Errorf("send initialize: %w (%s)%s", err, crashHint, c.stderrTail())
	}
	line, err := c.stdout.ReadBytes('\n')
	if err != nil {
		return fmt.Errorf("recv initialize: %w (%s)%s", err, crashHint, c.stderrTail())
	}
	var resp rpcResp
	if err := json.Unmarshal(line, &resp); err != nil {
		return fmt.Errorf("decode initialize response: %w", err)
	}
	if resp.Error != nil {
		return fmt.Errorf("initialize error %d: %s", resp.Error.Code, c.redactSecrets(resp.Error.Message))
	}
	if _, err := fmt.Fprintln(c.stdin, `{"jsonrpc":"2.0","method":"notifications/initialized"}`); err != nil {
		return fmt.Errorf("send initialized notification: %w (%s)", err, crashHint)
	}
	return nil
}

type rpcReq struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int64  `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// buildRequest constructs the tools/call envelope. The query string and the
// params map are kept structurally separate — there is no code path that
// concatenates caller params into the query body (T-1.07-01). Unit-tested by
// TestCypherClient_RejectsConcatenatedInjection.
func (c *Client) buildRequest(query string, params map[string]any, write bool) rpcReq {
	tool := toolRead
	if write {
		tool = toolWrite
	}
	if params == nil {
		params = map[string]any{}
	}
	return rpcReq{
		JSONRPC: "2.0",
		ID:      c.nextID.Add(1),
		Method:  "tools/call",
		Params: map[string]any{
			"name":      tool,
			"arguments": map[string]any{"query": query, "params": params},
		},
	}
}

// readLineWithContext reads one newline-delimited JSON-RPC frame with ctx as a hang
// ceiling, mirroring initializeWithContext. A hung mcp-neo4j-cypher (no response line
// ever written) otherwise blocks ReadBytes forever under mu, wedging every graph op
// through this Client and leaking the reader goroutine (Wave 0.3). On ctx expiry it
// kills the subprocess — which unblocks the read (closing the pipe) and enforces the
// D-06 fail-closed policy — then returns ctx.Err(). The buffered channel lets the reader
// goroutine send its result and exit even after the kill, so nothing leaks. mu stays
// held by the caller: the single stdio pipe pair cannot interleave, so the read is not
// moved off the lock; killing is what frees the blocked read.
func (c *Client) readLineWithContext(ctx context.Context) ([]byte, error) {
	type readResult struct {
		line []byte
		err  error
	}
	done := make(chan readResult, 1)
	go func() {
		line, err := c.stdout.ReadBytes('\n')
		done <- readResult{line: line, err: err}
	}()
	select {
	case r := <-done:
		return r.line, r.err
	case <-ctx.Done():
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		// Reap the now-unblocked reader (the kill closes the pipe, so ReadBytes returns
		// promptly) so its goroutine cannot outlive the call.
		select {
		case <-done:
		case <-time.After(time.Second):
		}
		return nil, ctx.Err()
	}
}

// Cypher sends one read or write Cypher call and returns the raw MCP result.
// Serialized via mu. Send/recv failures wrap crashHint (D-06) with redacted
// stderr so a dead subprocess fails Aura with an actionable, secret-free error.
func (c *Client) Cypher(ctx context.Context, query string, params map[string]any, write bool) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin == nil {
		return nil, fmt.Errorf("cypher on closed knowledge client")
	}

	enc, err := json.Marshal(c.buildRequest(query, params, write))
	if err != nil {
		return nil, fmt.Errorf("encode cypher request: %w", err)
	}
	if _, err := fmt.Fprintln(c.stdin, string(enc)); err != nil {
		return nil, fmt.Errorf("send cypher: %w (%s)%s", err, crashHint, c.stderrTail())
	}
	// Bound the response read by a hang ceiling, NOT the caller ctx: caller cancellation
	// (a client disconnect) must not kill the shared subprocess mid-flight, so we strip
	// it with WithoutCancel and only the timeout can fire. A single stdio pipe pair cannot
	// interleave, so mu stays held across the read (0.3).
	timeout := c.callTimeout
	if timeout <= 0 {
		timeout = cypherCallTimeout
	}
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), timeout)
	defer cancel()
	line, err := c.readLineWithContext(readCtx)
	if err != nil {
		return nil, fmt.Errorf("recv cypher: %w (%s)%s", err, crashHint, c.stderrTail())
	}
	var resp rpcResp
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("decode cypher response: %w", err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("cypher error %d: %s", resp.Error.Code, c.redactSecrets(resp.Error.Message))
	}
	return resp.Result, nil
}

func (c *Client) Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error) {
	result, err := c.Cypher(ctx, query, params, false)
	if err != nil {
		return nil, err
	}
	return decodeRows(result)
}

func (c *Client) Write(ctx context.Context, query string, params map[string]any) ([]map[string]any, error) {
	result, err := c.Cypher(ctx, query, params, true)
	if err != nil {
		return nil, err
	}
	return decodeRows(result)
}

// Close shuts the subprocess down by closing its stdin and waiting for exit.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stdin != nil {
		_ = c.stdin.Close()
		c.stdin = nil
	}
	cmd := c.cmd
	c.cmd = nil
	if cmd == nil {
		return nil
	}
	return cmd.Wait()
}

func connectContext(parent context.Context, cfg *Config) (context.Context, context.CancelFunc) {
	if cfg != nil && cfg.ConnectTimeoutSec > 0 {
		return context.WithTimeout(parent, time.Duration(cfg.ConnectTimeoutSec)*time.Second)
	}
	return context.WithCancel(parent)
}

// stderrTail returns a redacted, length-capped suffix of captured stderr for
// error context (empty string when nothing was captured).
func (c *Client) stderrTail() string {
	s := strings.TrimSpace(c.stderr.String())
	if s == "" {
		return ""
	}
	if len(s) > 200 {
		s = s[len(s)-200:]
	}
	return ": " + c.redactSecrets(s)
}

// redactSecrets masks the literal Neo4j password plus any password=/pass:
// assignment so credentials never reach a log line (T-1.07-04).
func (c *Client) redactSecrets(s string) string {
	if c.password != "" {
		s = strings.ReplaceAll(s, c.password, "***")
	}
	return pwAssignRE.ReplaceAllString(s, "$1$2***")
}

// decodeRows extracts row objects from an MCP tools/call result. mcp-neo4j-cypher
// wraps query output as {"content":[{"type":"text","text":"<json-array>"}]}.
// VERIFY at Wave 0 probe (GATE B / Assumption A10): if the live envelope differs
// from this shape, adjust here — this is the single decode chokepoint.
func decodeRows(result json.RawMessage) ([]map[string]any, error) {
	if len(result) == 0 {
		return nil, nil
	}
	var env struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &env); err != nil {
		return nil, fmt.Errorf("decode mcp content envelope: %w", err)
	}
	if env.IsError {
		detail := ""
		if len(env.Content) > 0 {
			detail = strings.TrimSpace(env.Content[0].Text)
		}
		return nil, fmt.Errorf("mcp tool reported isError=true: %s", detail)
	}
	if len(env.Content) == 0 || strings.TrimSpace(env.Content[0].Text) == "" {
		return nil, nil
	}
	payload := []byte(env.Content[0].Text)
	var rows []map[string]any
	if err := json.Unmarshal(payload, &rows); err == nil {
		return rows, nil
	} else {
		var row map[string]any
		if objectErr := json.Unmarshal(payload, &row); objectErr == nil {
			return []map[string]any{row}, nil
		}
		return nil, fmt.Errorf("decode cypher rows: %w", err)
	}
}
