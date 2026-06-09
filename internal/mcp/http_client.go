package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

const httpProtocolVersion = "2025-06-18"
const httpSSEMaxLineBytes = 1024 * 1024

// HTTPConfig declares how to reach a Streamable-HTTP MCP server: its endpoint URL
// plus optional static headers, bearer token, and an override http.Client.
type HTTPConfig struct {
	URL         string
	Headers     map[string]string
	BearerToken string
	Client      *http.Client
}

// HTTPClient speaks the Streamable-HTTP MCP transport to one remote server,
// tracking the negotiated protocol version and Mcp-Session-Id across requests.
// The zero value is unusable; use OpenHTTP.
type HTTPClient struct {
	name            string
	endpoint        string
	headers         map[string]string
	bearerToken     string
	client          *http.Client
	sessionID       string
	protocolVersion string
	mu              sync.Mutex
	nextID          atomic.Int64
}

// OpenHTTP connects to the Streamable-HTTP MCP server at cfg.URL and completes the
// initialize handshake, capturing the session id and negotiated protocol version.
// name is a short label used in error messages (the mcpServers key).
func OpenHTTP(ctx context.Context, name string, cfg HTTPConfig) (*HTTPClient, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("mcp %q: empty HTTP URL", name)
	}
	httpClient := cfg.Client
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	c := &HTTPClient{
		name:            name,
		endpoint:        strings.TrimSpace(cfg.URL),
		headers:         cfg.Headers,
		bearerToken:     cfg.BearerToken,
		client:          httpClient,
		protocolVersion: httpProtocolVersion,
	}
	if err := c.initialize(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *HTTPClient) initialize(ctx context.Context) error {
	res, err := c.roundtrip(ctx, "initialize", map[string]any{
		"protocolVersion": httpProtocolVersion,
		"capabilities":    map[string]any{},
		"clientInfo":      map[string]any{"name": "aura", "version": "0.7"},
	})
	if err != nil {
		return fmt.Errorf("mcp %q: initialize: %w", c.name, err)
	}
	var initResult struct {
		ProtocolVersion string `json:"protocolVersion"`
	}
	if err := json.Unmarshal(res, &initResult); err == nil && strings.TrimSpace(initResult.ProtocolVersion) != "" {
		c.protocolVersion = initResult.ProtocolVersion
	}
	if err := c.notify(ctx, "notifications/initialized"); err != nil {
		return fmt.Errorf("mcp %q: initialized notification: %w", c.name, err)
	}
	return nil
}

// ListTools returns the server's advertised tools (tools/list).
func (c *HTTPClient) ListTools(ctx context.Context) ([]ToolDef, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.roundtripLocked(ctx, "tools/list", map[string]any{})
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

// CallTool invokes one tool (tools/call) and returns its concatenated text
// content; a result with isError=true is returned as an error carrying that text.
func (c *HTTPClient) CallTool(ctx context.Context, name string, args map[string]any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if args == nil {
		args = map[string]any{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	res, err := c.roundtripLocked(ctx, "tools/call", map[string]any{"name": name, "arguments": args})
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

// Ping issues an MCP ping round-trip to confirm the remote server is reachable and
// the session is still valid.
func (c *HTTPClient) Ping(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, err := c.roundtripLocked(ctx, "ping", map[string]any{}); err != nil {
		return fmt.Errorf("mcp %q: ping: %w", c.name, err)
	}
	return nil
}

// Close terminates the MCP session with an HTTP DELETE when one is open; a server
// that does not support session deletion (405/404) is treated as already closed.
func (c *HTTPClient) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sessionID == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodDelete, c.endpoint, nil)
	if err != nil {
		return err
	}
	c.decorate(req, true)
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusMethodNotAllowed || resp.StatusCode == http.StatusNotFound {
		c.sessionID = ""
		return nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("http %d", resp.StatusCode)
	}
	c.sessionID = ""
	return nil
}

func (c *HTTPClient) roundtrip(ctx context.Context, method string, params any) (json.RawMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.roundtripLocked(ctx, method, params)
}

func (c *HTTPClient) roundtripLocked(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	payload := rpcReq{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	resp, err := c.post(ctx, payload, method != "initialize")
	if err != nil {
		return nil, err
	}
	if method == "initialize" {
		if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
			c.sessionID = sid
		}
	}
	return decodeHTTPRPC(resp.Body, id, resp.Header.Get("Content-Type"))
}

func (c *HTTPClient) notify(ctx context.Context, method string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	resp, err := c.post(ctx, map[string]any{"jsonrpc": "2.0", "method": method}, true)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusAccepted || (resp.StatusCode >= 200 && resp.StatusCode < 300) {
		return nil
	}
	return fmt.Errorf("http %d", resp.StatusCode)
}

func (c *HTTPClient) post(ctx context.Context, payload any, includeProtocol bool) (*http.Response, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	c.decorate(req, includeProtocol)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		defer func() { _ = resp.Body.Close() }()
		return nil, fmt.Errorf("unauthorized (401)")
	}
	if resp.StatusCode == http.StatusNotFound && c.sessionID != "" {
		defer func() { _ = resp.Body.Close() }()
		c.sessionID = ""
		return nil, fmt.Errorf("session expired (404)")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return resp, nil
}

func (c *HTTPClient) decorate(req *http.Request, includeProtocol bool) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	if includeProtocol && c.protocolVersion != "" {
		req.Header.Set("MCP-Protocol-Version", c.protocolVersion)
	}
}

func decodeHTTPRPC(body io.ReadCloser, wantID int64, contentType string) (json.RawMessage, error) {
	defer func() { _ = body.Close() }()
	if strings.Contains(contentType, "text/event-stream") {
		return decodeHTTPSSE(body, wantID)
	}
	var resp rpcResp
	if err := json.NewDecoder(body).Decode(&resp); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return rpcResult(resp, wantID)
}

func decodeHTTPSSE(r io.Reader, wantID int64) (json.RawMessage, error) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), httpSSEMaxLineBytes)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var resp rpcResp
		if err := json.Unmarshal([]byte(strings.TrimSpace(strings.TrimPrefix(line, "data:"))), &resp); err != nil {
			return nil, fmt.Errorf("decode sse response: %w", err)
		}
		if resp.ID == nil || *resp.ID != wantID {
			continue
		}
		return rpcResult(resp, wantID)
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("missing response id %d in event stream", wantID)
}

func rpcResult(resp rpcResp, wantID int64) (json.RawMessage, error) {
	if resp.ID == nil || *resp.ID != wantID {
		return nil, fmt.Errorf("missing response id %d", wantID)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("rpc error %d: %s", resp.Error.Code, resp.Error.Message)
	}
	return resp.Result, nil
}
