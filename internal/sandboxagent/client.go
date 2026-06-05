// Package sandboxagent is Aura's local-container sandbox client. It talks to a
// sandbox-agent server listening on loopback (normally the aura-sandbox-agent
// Compose service) and never provisions or downloads anything at chat boot.
package sandboxagent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const (
	DefaultBaseURL    = "http://127.0.0.1:2468"
	DefaultTimeoutSec = 30
)

type Config struct {
	BaseURL    string
	TimeoutSec int
	// Token is the shared bearer the sandbox-agent enforces with --token (D-38, spike
	// 008). When non-empty the client sends `Authorization: Bearer <token>` on EVERY
	// request (exec + health); an empty token preserves the unauthenticated path for
	// a sandbox-agent still running --no-token (dev) or in tests.
	Token string
}

type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

type RunRequest struct {
	Command        string            `json:"command"`
	Args           []string          `json:"args,omitempty"`
	Cwd            string            `json:"cwd,omitempty"`
	Env            map[string]string `json:"env,omitempty"`
	MaxOutputBytes *int              `json:"maxOutputBytes,omitempty"`
	TimeoutMs      *int64            `json:"timeoutMs,omitempty"`
}

type RunResponse struct {
	TimedOut        bool   `json:"timedOut"`
	Stdout          string `json:"stdout"`
	Stderr          string `json:"stderr"`
	StdoutTruncated bool   `json:"stdoutTruncated"`
	StderrTruncated bool   `json:"stderrTruncated"`
	DurationMs      int64  `json:"durationMs"`
	ExitCode        *int   `json:"exitCode,omitempty"`
}

func New(cfg Config) *Client {
	base := strings.TrimRight(cfg.BaseURL, "/")
	if base == "" {
		base = DefaultBaseURL
	}
	timeout := cfg.TimeoutSec
	if timeout <= 0 {
		timeout = DefaultTimeoutSec
	}
	return &Client{
		baseURL: base,
		token:   cfg.Token,
		http:    &http.Client{Timeout: time.Duration(timeout) * time.Second},
	}
}

// authHeaders sets the JSON content-negotiation headers and, when a token is
// configured, the bearer the sandbox-agent's --token enforces on every route
// (D-38, spike 008 — including /v1/health). Centralizing it keeps the bearer
// from being forgotten on any future request path.
func (c *Client) authHeaders(req *http.Request) {
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
}

// authError classifies a 401 from the sandbox-agent as a clear auth failure so a
// misconfigured/absent AURA_SANDBOX_AGENT_TOKEN surfaces as "auth failed" rather
// than a generic HTTP error (spike 008). Any other non-2xx falls through to the
// caller's generic HTTP-status error.
func authError(op string, status int, body string) error {
	if status == http.StatusUnauthorized {
		return fmt.Errorf("sandbox-agent %s auth failed (HTTP 401) — check AURA_SANDBOX_AGENT_TOKEN matches the --token the container runs: %s", op, body)
	}
	return nil
}

func (c *Client) Run(ctx context.Context, req RunRequest) (RunResponse, error) {
	var out RunResponse
	body, err := json.Marshal(req)
	if err != nil {
		return out, fmt.Errorf("sandbox-agent run marshal: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/processes/run", bytes.NewReader(body))
	if err != nil {
		return out, fmt.Errorf("sandbox-agent run request: %w", err)
	}
	c.authHeaders(httpReq)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return out, fmt.Errorf("sandbox-agent run: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(mustRead(resp.Body)))
		if aerr := authError("run", resp.StatusCode, msg); aerr != nil {
			return out, aerr
		}
		return out, fmt.Errorf("sandbox-agent run HTTP %d: %s", resp.StatusCode, msg)
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("sandbox-agent run decode: %w", err)
	}
	return out, nil
}

// Health probes /v1/health, carrying the bearer (D-38, spike 008 — /v1/health
// itself enforces --token). A 200 means the sandbox-agent is reachable AND the
// configured token is accepted; a 401 surfaces a clear auth error so a token
// mismatch is diagnosed at the boot probe, not at the first exec.
func (c *Client) Health(ctx context.Context) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/v1/health", nil)
	if err != nil {
		return fmt.Errorf("sandbox-agent health request: %w", err)
	}
	c.authHeaders(httpReq)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sandbox-agent health: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg := strings.TrimSpace(string(mustRead(resp.Body)))
		if aerr := authError("health", resp.StatusCode, msg); aerr != nil {
			return aerr
		}
		return fmt.Errorf("sandbox-agent health HTTP %d: %s", resp.StatusCode, msg)
	}
	return nil
}

// mustRead drains up to 4 KiB of a response body for an error message (best-effort).
func mustRead(r io.Reader) []byte {
	b, _ := io.ReadAll(io.LimitReader(r, 4096))
	return b
}
