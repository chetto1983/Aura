// Package knowledge adapts Aura's generic hardened stdio MCP transport to the
// two Cypher tools exposed by mcp-neo4j-cypher. It owns no process, framing,
// buffering, serialization, cancellation, or shutdown implementation.
package knowledge

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/mcp"
)

const (
	toolRead  = "read_neo4j_cypher"
	toolWrite = "write_neo4j_cypher"

	crashHint         = "MCP may have crashed — D-06 policy: fail Aura process"
	cypherCallTimeout = 120 * time.Second
)

var pwAssignRE = regexp.MustCompile(`(?i)(password|pass)(\s*[=:]\s*)\S+`)

type cypherTransport interface {
	CallTool(context.Context, string, map[string]any) (string, error)
	Close() error
}

// Client is a narrow Cypher adapter over the common MCP transport.
type Client struct {
	transport cypherTransport
	password  string
}

// Open launches mcp-neo4j-cypher through the common hardened stdio client. Neo4j
// credentials are delivered only through the upstream server's native NEO4J_*
// environment contract; argv contains transport selection and no secret.
func Open(ctx context.Context, cfg *Config) (*Client, error) {
	if cfg == nil {
		return nil, fmt.Errorf("open mcp-neo4j-cypher: nil config")
	}
	handshakeCtx, cancel := connectContext(ctx, cfg)
	defer cancel()
	transport, err := mcp.OpenWithHandshakeContext(
		context.WithoutCancel(ctx),
		handshakeCtx,
		"neo4j-cypher",
		mcp.ServerConfig{
			Command: cfg.MCPBinary,
			Args:    []string{"--transport", "stdio"},
			Env: []string{
				"NEO4J_URL=" + cfg.BoltURL,
				"NEO4J_USERNAME=" + cfg.User,
				"NEO4J_PASSWORD=" + cfg.Password,
				"NEO4J_DATABASE=" + cfg.Database,
				"NEO4J_TRANSPORT=stdio",
			},
			CallTimeout: cypherCallTimeout,
		},
	)
	if err != nil {
		return nil, fmt.Errorf(
			"open mcp-neo4j-cypher: %w (PATH check: pip install mcp-neo4j-cypher==0.6.0)",
			err,
		)
	}
	return &Client{transport: transport, password: cfg.Password}, nil
}

// Cypher invokes the read or write tool with query and params as separate
// structured arguments. It preserves the historical raw MCP content envelope so
// existing callers of Cypher and decodeRows remain wire-compatible.
func (c *Client) Cypher(
	ctx context.Context,
	query string,
	params map[string]any,
	write bool,
) (json.RawMessage, error) {
	if c == nil || c.transport == nil {
		return nil, fmt.Errorf("cypher on closed knowledge client")
	}
	if params == nil {
		params = map[string]any{}
	}
	tool := toolRead
	if write {
		tool = toolWrite
	}
	text, err := c.transport.CallTool(ctx, tool, map[string]any{
		"query":  query,
		"params": params,
	})
	if err != nil {
		message := "cypher call: " + err.Error()
		if mcp.IsTransportError(err) {
			message += " (" + crashHint + ")"
		}
		return nil, &redactedClientError{message: c.redactSecrets(message), cause: err}
	}
	envelope, err := json.Marshal(map[string]any{
		"content": []map[string]string{{"type": "text", "text": text}},
		"isError": false,
	})
	if err != nil {
		return nil, fmt.Errorf("encode cypher result: %w", err)
	}
	return envelope, nil
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

// Close delegates bounded, idempotent shutdown to the common MCP transport.
func (c *Client) Close() error {
	if c == nil || c.transport == nil {
		return nil
	}
	return c.transport.Close()
}

func connectContext(parent context.Context, cfg *Config) (context.Context, context.CancelFunc) {
	if cfg != nil && cfg.ConnectTimeoutSec > 0 {
		return context.WithTimeout(parent, time.Duration(cfg.ConnectTimeoutSec)*time.Second)
	}
	return context.WithCancel(parent)
}

func (c *Client) redactSecrets(s string) string {
	s = mcp.RedactSecrets(s)
	if c != nil && c.password != "" {
		s = strings.ReplaceAll(s, c.password, "***")
	}
	return pwAssignRE.ReplaceAllString(s, "$1$2***")
}

type redactedClientError struct {
	message string
	cause   error
}

func (e *redactedClientError) Error() string { return e.message }
func (e *redactedClientError) Unwrap() error { return e.cause }

// decodeRows extracts row objects from the preserved MCP content envelope.
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
