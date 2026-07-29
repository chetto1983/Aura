package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/chetto1983/aura/internal/idempotency"
)

const httpProtocolVersion = "2025-06-18"
const httpSSEMaxLineBytes = 1024 * 1024
const httpRPCMaxBodyBytes = 8 << 20

// httpCloseTimeout bounds the session-DELETE round-trip on Close so an
// unresponsive remote endpoint cannot hang shutdown indefinitely; it mirrors the
// stdio client's closeWaitTimeout budget.
const httpCloseTimeout = 5 * time.Second

var errHTTPSessionExpired = errors.New("mcp http session expired")

// HTTPConfig declares how to reach a Streamable-HTTP MCP server: its endpoint URL
// plus optional static headers, bearer token, and an override http.Client.
//
// EgressPolicy is resolved from the runtime profile plus typed managed-server
// source/trust. Its zero value is the dev-permissive posture, while the
// scheme/cloud-metadata barrier remains unconditional.
type HTTPConfig struct {
	URL                  string
	Headers              map[string]string
	BearerToken          string
	BearerTokenSource    func(context.Context) (string, error)
	ToolIdentityArgument string
	Client               *http.Client
	EgressPolicy         EgressPolicy
}

// HTTPClient speaks the Streamable-HTTP MCP transport to one remote server,
// tracking the negotiated protocol version and Mcp-Session-Id across requests.
// The zero value is unusable; use OpenHTTP.
type HTTPClient struct {
	name            string
	endpoint        string
	headers         map[string]string
	bearerToken     string
	bearerSource    func(context.Context) (string, error)
	identityArg     string
	client          *http.Client
	sessionID       string
	protocolVersion string
	gate            sessionGate
	closeOnce       sync.Once
	closeErr        error
	nextID          atomic.Int64
}

// OpenHTTP connects to the Streamable-HTTP MCP server at cfg.URL and completes the
// initialize handshake, capturing the session id and negotiated protocol version.
// name is a short label used in error messages (the mcpServers key).
func OpenHTTP(ctx context.Context, name string, cfg HTTPConfig) (*HTTPClient, error) {
	if strings.TrimSpace(cfg.URL) == "" {
		return nil, fmt.Errorf("mcp %q: empty HTTP URL", name)
	}
	// SSRF barrier (SEC-08 / T-31-SSRF): validate the endpoint BEFORE it becomes
	// c.endpoint so the c.client.Do(req) sink consumes a parsed+scheme+metadata-
	// checked value. The scheme/metadata block is unconditional; private-range
	// handling comes from the resolved profile/source egress policy.
	validated, err := guardEndpointWithPolicy(ctx, cfg.URL, cfg.EgressPolicy, net.DefaultResolver)
	if err != nil {
		return nil, fmt.Errorf("mcp %q: %w", name, err)
	}
	httpClient := cfg.Client
	if httpClient == nil {
		httpClient = http.DefaultClient
		if cfg.EgressPolicy.Enforced() {
			// Hardened Layer-2 pins strict-policy dials. Lenient policy keeps the
			// default transport; both paths still receive redirect validation below.
			httpClient = newHardenedHTTPClient(net.DefaultResolver, cfg.EgressPolicy)
		}
	}
	httpClient = withMCPRedirectGuard(httpClient, cfg.EgressPolicy, net.DefaultResolver)
	c := &HTTPClient{
		name:            name,
		endpoint:        validated.String(),
		headers:         cfg.Headers,
		bearerToken:     cfg.BearerToken,
		bearerSource:    cfg.BearerTokenSource,
		identityArg:     strings.TrimSpace(cfg.ToolIdentityArgument),
		client:          httpClient,
		protocolVersion: httpProtocolVersion,
	}
	if err := c.initialize(ctx); err != nil {
		return nil, err
	}
	return c, nil
}

func withMCPRedirectGuard(base *http.Client, policy EgressPolicy, res resolver) *http.Client {
	cloned := *base
	previous := base.CheckRedirect
	cloned.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		if _, err := guardEndpointWithPolicy(req.Context(), req.URL.String(), policy, res); err != nil {
			return fmt.Errorf("mcp redirect blocked: %w", err)
		}
		if previous != nil {
			return previous(req, via)
		}
		if len(via) >= 10 {
			return fmt.Errorf("mcp redirect stopped after %d hops", len(via))
		}
		return nil
	}
	return &cloned
}

func (c *HTTPClient) initialize(ctx context.Context) (err error) {
	ctx, end := httpInitializeBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	callCtx, release, err := c.gate.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	return c.initializeLocked(callCtx)
}

func (c *HTTPClient) initializeLocked(ctx context.Context) error {
	res, err := c.roundtripLocked(ctx, "initialize", map[string]any{
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
	if err := c.notifyLocked(ctx, "notifications/initialized"); err != nil {
		return fmt.Errorf("mcp %q: initialized notification: %w", c.name, err)
	}
	return nil
}

// ListTools returns the server's advertised tools (tools/list).
func (c *HTTPClient) ListTools(ctx context.Context) (defs []ToolDef, err error) {
	ctx, end := httpListBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	callCtx, release, err := c.gate.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer release()
	return listToolsWith(callCtx, c.name, c.roundtripLocked)
}

// CallTool invokes one tool (tools/call) and returns its concatenated text
// content; a result with isError=true is returned as an error carrying that text.
func (c *HTTPClient) CallTool(ctx context.Context, name string, args map[string]any) (text string, err error) {
	ctx, end := httpCallBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if c.identityArg != "" {
		rawIdentity, ok := args[c.identityArg]
		identity, stringOK := rawIdentity.(string)
		identity = strings.TrimSpace(identity)
		if !ok || !stringOK || identity == "" {
			return "", fmt.Errorf(
				"mcp %q: tool %q requires non-empty %s",
				c.name,
				name,
				c.identityArg,
			)
		}
		ctx = withBearerSubject(ctx, identity)
	}
	callCtx, release, err := c.gate.acquire(ctx)
	if err != nil {
		return "", err
	}
	defer release()
	return callToolWith(callCtx, c.name, name, args, c.roundtripLocked)
}

// Ping issues an MCP ping round-trip to confirm the remote server is reachable and
// the session is still valid.
func (c *HTTPClient) Ping(ctx context.Context) (err error) {
	ctx, end := httpPingBoundary.Start(ctx)
	defer end.PanicSafe(&err)
	callCtx, release, err := c.gate.acquire(ctx)
	if err != nil {
		return err
	}
	defer release()
	if _, err := c.roundtripLocked(callCtx, "ping", map[string]any{}); err != nil {
		return fmt.Errorf("mcp %q: ping: %w", c.name, err)
	}
	return nil
}

// Close terminates the MCP session with an HTTP DELETE when one is open; a server
// that does not support session deletion (405/404) is treated as already closed.
func (c *HTTPClient) Close() error {
	c.closeOnce.Do(func() {
		c.closeErr = c.close()
	})
	return c.closeErr
}

func (c *HTTPClient) close() error {
	ctx, cancel := context.WithTimeout(context.Background(), httpCloseTimeout)
	defer cancel()
	c.gate.beginClose()
	if err := c.gate.waitIdle(ctx); err != nil {
		return fmt.Errorf("mcp %q: close active request: %w", c.name, err)
	}
	if c.sessionID == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint, nil)
	if err != nil {
		return err
	}
	if err := c.decorate(ctx, req, true); err != nil {
		return err
	}
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

func (c *HTTPClient) roundtripLocked(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := c.nextID.Add(1)
	payload := rpcReq{JSONRPC: "2.0", ID: id, Method: method, Params: params}
	resp, err := c.post(ctx, payload, method != "initialize")
	if errors.Is(err, errHTTPSessionExpired) && method != "initialize" {
		if operation, ok := idempotency.OperationFromContext(ctx); ok && operation.Key.Scope == idempotency.ScopeMCPTool {
			return nil, fmt.Errorf("%w: session expired after mutating send", ErrTransport)
		}
		if initErr := c.initializeLocked(ctx); initErr != nil {
			return nil, fmt.Errorf("session expired (404); reinitialize: %w", initErr)
		}
		id = c.nextID.Add(1)
		payload.ID = id
		resp, err = c.post(ctx, payload, true)
	}
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

func (c *HTTPClient) notifyLocked(ctx context.Context, method string) error {
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
	if err := c.decorate(ctx, req, includeProtocol); err != nil {
		return nil, err
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: http post: %w", ErrTransport, err)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		defer func() { _ = resp.Body.Close() }()
		return nil, fmt.Errorf("unauthorized (401)")
	}
	if resp.StatusCode == http.StatusNotFound && c.sessionID != "" {
		defer func() { _ = resp.Body.Close() }()
		c.sessionID = ""
		return nil, errHTTPSessionExpired
	}
	// A session-less call is the SAME condition as an expired one, and saying so here is
	// what makes the connection recoverable. The 404 branch above clears sessionID before
	// roundtripLocked decides whether to retry; when it declines (a mutating tool call must
	// not be replayed after send), the client is left with no session and no way back — the
	// only trigger for reinitialize was the session id that was just cleared. Every later
	// call, reads included, then posts bare and the server answers 400 "Missing session ID"
	// forever. Observed live 2026-07-25 after a sidecar rebuild: twenty consecutive memory
	// calls failed with http 400 and only restarting the daemon fixed it.
	//
	// Guarded on sessionID == "": with a session in hand a 400 is a real bad request and is
	// returned as one. After reinitialize succeeds the guard no longer holds, so a genuinely
	// malformed request costs one extra round trip and cannot loop. includeProtocol is false
	// for exactly one method, initialize — the one call that legitimately has no session yet,
	// and whose own 400 must surface as itself rather than as a session to go re-establish.
	if resp.StatusCode == http.StatusBadRequest && c.sessionID == "" && includeProtocol {
		defer func() { _ = resp.Body.Close() }()
		return nil, errHTTPSessionExpired
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer func() { _ = resp.Body.Close() }()
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	return resp, nil
}

func (c *HTTPClient) decorate(
	ctx context.Context,
	req *http.Request,
	includeProtocol bool,
) error {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for key, value := range c.headers {
		req.Header.Set(key, value)
	}
	if c.bearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.bearerToken)
	}
	if c.bearerSource != nil {
		token, err := c.bearerSource(ctx)
		if err != nil {
			return fmt.Errorf("create bearer token: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if c.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", c.sessionID)
	}
	if includeProtocol && c.protocolVersion != "" {
		req.Header.Set("MCP-Protocol-Version", c.protocolVersion)
	}
	return nil
}

func decodeHTTPRPC(body io.ReadCloser, wantID int64, contentType string) (json.RawMessage, error) {
	defer func() { _ = body.Close() }()
	data, err := io.ReadAll(io.LimitReader(body, httpRPCMaxBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}
	if len(data) > httpRPCMaxBodyBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", httpRPCMaxBodyBytes)
	}
	if strings.Contains(contentType, "text/event-stream") {
		return decodeHTTPSSE(bytes.NewReader(data), wantID)
	}
	var resp rpcResp
	if err := json.NewDecoder(bytes.NewReader(data)).Decode(&resp); err != nil {
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
