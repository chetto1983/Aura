package markitdown

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/aura/aura/internal/mcp"
)

const (
	// DefaultTimeout caps a single conversion. markitdown is CPU-bound on
	// large xlsx; 120s is enough for the worst case we expect at personal
	// scale (~50 MiB workbook) without letting a stuck converter pin the
	// agent loop.
	DefaultTimeout = 120 * time.Second

	// DefaultMaxBytes rejects inputs that would be expensive to base64 +
	// ship across JSON-RPC. Aura already enforces per-upload caps upstream,
	// but defending in depth means a tool bug can't blow past this.
	DefaultMaxBytes = 64 * 1024 * 1024 // 64 MiB

	// mcpPath is the Streamable HTTP path the markitdown-mcp server mounts
	// when started with --http (see markitdown_mcp/__main__.py).
	mcpPath = "/mcp"

	// convertToolName is the single tool advertised by markitdown-mcp.
	convertToolName = "convert_to_markdown"
)

// Client speaks JSON-RPC 2.0 over Streamable HTTP to a running markitdown-mcp
// sidecar. The MCP handshake (initialize + tools/list) is performed lazily on
// the first Convert so Aura can start cleanly when the sidecar is offline.
type Client struct {
	baseURL  string
	timeout  time.Duration
	maxBytes int

	mu    sync.Mutex
	inner *mcp.Client
}

// Config tunes the client. Zero values pick sensible defaults.
type Config struct {
	BaseURL  string        // e.g. http://aura-markitdown:3001
	Timeout  time.Duration // per-call timeout; defaults to DefaultTimeout
	MaxBytes int           // per-call input cap; defaults to DefaultMaxBytes
}

// New builds a Client. The sidecar is NOT contacted here; the first Convert
// call connects.
func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	maxBytes := cfg.MaxBytes
	if maxBytes <= 0 {
		maxBytes = DefaultMaxBytes
	}
	return &Client{
		baseURL:  strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		timeout:  timeout,
		maxBytes: maxBytes,
	}
}

// Convert posts a data: URI to markitdown-mcp and returns the resulting
// markdown. Errors are terse and never include the body bytes.
func (c *Client) Convert(ctx context.Context, in ConvertInput) (ConvertResult, error) {
	if c == nil {
		return ConvertResult{}, errors.New("markitdown: nil client")
	}
	if c.baseURL == "" {
		return ConvertResult{}, errors.New("markitdown: base url not configured")
	}
	if len(in.Bytes) == 0 {
		return ConvertResult{}, errors.New("markitdown: empty bytes")
	}
	if len(in.Bytes) > c.maxBytes {
		return ConvertResult{}, fmt.Errorf("markitdown: input %d bytes exceeds cap %d", len(in.Bytes), c.maxBytes)
	}

	inner, err := c.connect()
	if err != nil {
		return ConvertResult{}, err
	}

	mime := strings.TrimSpace(in.MimeType)
	if mime == "" {
		mime = "application/octet-stream"
	}
	uri := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString(in.Bytes)

	callCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	md, err := inner.CallTool(callCtx, convertToolName, map[string]any{"uri": uri})
	if err != nil {
		// Treat any failure as a reason to drop the cached connection so
		// the next call re-bootstraps against a possibly-restarted sidecar.
		c.reset()
		return ConvertResult{}, fmt.Errorf("markitdown: convert: %w", err)
	}
	return ConvertResult{Markdown: md}, nil
}

// Close releases the cached MCP connection. Safe to call multiple times.
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inner == nil {
		return nil
	}
	err := c.inner.Close()
	c.inner = nil
	return err
}

func (c *Client) connect() (*mcp.Client, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inner != nil {
		return c.inner, nil
	}
	inner, err := mcp.NewHTTPClient("markitdown", c.baseURL+mcpPath, nil)
	if err != nil {
		return nil, fmt.Errorf("markitdown: connect %s: %w", c.baseURL, err)
	}
	if !hasTool(inner.Tools(), convertToolName) {
		_ = inner.Close()
		return nil, fmt.Errorf("markitdown: sidecar at %s does not expose %s", c.baseURL, convertToolName)
	}
	c.inner = inner
	return inner, nil
}

func (c *Client) reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.inner != nil {
		_ = c.inner.Close()
		c.inner = nil
	}
}

func hasTool(tools []mcp.Tool, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}
