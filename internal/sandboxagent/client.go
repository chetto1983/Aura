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
}

type Client struct {
	baseURL string
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
		http:    &http.Client{Timeout: time.Duration(timeout) * time.Second},
	}
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
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return out, fmt.Errorf("sandbox-agent run: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return out, fmt.Errorf("sandbox-agent run HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return out, fmt.Errorf("sandbox-agent run decode: %w", err)
	}
	return out, nil
}
