// Package arcadedb is a typed client for ArcadeDB's HTTP API.
//
// It talks to the database directly rather than through ArcadeDB's own MCP
// server: that server's `query` tool accepts only (database, language, query,
// limit) with no bind parameters, which makes it unusable for a 1024-float
// vector and unsafe for user text. `/api/v1/query` and `/api/v1/command` take
// `params`, so they are the programmatic seam.
package arcadedb

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DefaultTimeout bounds a single statement. ArcadeDB answers a vector or
// full-text query in tens of milliseconds; anything approaching this is a
// symptom, not a slow query.
const DefaultTimeout = 30 * time.Second

// maxErrorBody caps how much of a failed response is read before wrapping, so a
// misconfigured endpoint returning a large body cannot be echoed into a log.
const maxErrorBody = 4 << 10

// ErrEmptyStatement is returned instead of letting the server reject a blank
// statement, which it reports as a parse error and is confusing to read.
var ErrEmptyStatement = errors.New("arcadedb: empty statement")

// Config addresses one database. Every field is required except Timeout.
type Config struct {
	BaseURL  string
	Database string
	User     string
	Password string
	Timeout  time.Duration
}

// Client issues statements against a single ArcadeDB database.
type Client struct {
	queryURL   string
	commandURL string
	authHeader string
	http       *http.Client
	// embedder is optional: with none, memory retrieval is the lexical leg alone,
	// which is the behaviour that shipped and must not regress when it is absent.
	embedder Embedder
}

// ServerError is a statement the database refused. Detail carries ArcadeDB's
// own message, which names the offending column for a parse error -- keep it,
// it is the difference between "syntax error" and "column 247 is a reserved
// word".
type ServerError struct {
	Status    int
	Kind      string
	Detail    string
	Exception string
}

func (e *ServerError) Error() string {
	detail := e.Detail
	if detail == "" {
		detail = e.Kind
	}
	return fmt.Sprintf("arcadedb: http %d: %s", e.Status, detail)
}

// New validates cfg and builds a Client. It performs no I/O.
func New(cfg Config) (*Client, error) {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		return nil, fmt.Errorf("arcadedb: base URL must be non-empty")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: parse base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, fmt.Errorf("arcadedb: base URL scheme must be http or https, got %q", parsed.Scheme)
	}
	database := strings.TrimSpace(cfg.Database)
	if database == "" {
		return nil, fmt.Errorf("arcadedb: database must be non-empty")
	}
	if strings.TrimSpace(cfg.User) == "" {
		return nil, fmt.Errorf("arcadedb: user must be non-empty")
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	credentials := base64.StdEncoding.EncodeToString([]byte(cfg.User + ":" + cfg.Password))
	return &Client{
		queryURL:   base + "/api/v1/query/" + url.PathEscape(database),
		commandURL: base + "/api/v1/command/" + url.PathEscape(database),
		authHeader: "Basic " + credentials,
		http:       &http.Client{Timeout: timeout},
	}, nil
}

// Query runs a read-only statement.
func (c *Client) Query(ctx context.Context, statement string, params map[string]any) ([]map[string]any, error) {
	return c.execute(ctx, c.queryURL, "sql", statement, params)
}

// Command runs a statement that writes. ArcadeDB rejects a mutation sent to the
// query endpoint, so the split is the server's, not a convention of ours.
func (c *Client) Command(ctx context.Context, statement string, params map[string]any) ([]map[string]any, error) {
	return c.execute(ctx, c.commandURL, "sql", statement, params)
}

// Read and Write run Cypher. They exist with these names and this signature
// because that is what Aura's packages already depend on
// (internal/documents.KnowledgeClient, internal/knowledge.GraphReader): a
// *Client satisfies those interfaces, so the existing Cypher runs against
// ArcadeDB unchanged rather than being rewritten in SQL or Gremlin.
//
// The read/write split mirrors the endpoints: ArcadeDB refuses a mutation sent
// to /query, which is a useful guard rather than an inconvenience.
func (c *Client) Read(ctx context.Context, query string, params map[string]any) ([]map[string]any, error) {
	return c.execute(ctx, c.queryURL, "cypher", query, params)
}

// Write runs Cypher that mutates.
func (c *Client) Write(ctx context.Context, query string, params map[string]any) ([]map[string]any, error) {
	return c.execute(ctx, c.commandURL, "cypher", query, params)
}

// Script runs several SQL statements in one transaction, delimited by `;` and
// wrapped in BEGIN/COMMIT.
func (c *Client) Script(ctx context.Context, script string, params map[string]any) ([]map[string]any, error) {
	return c.execute(ctx, c.commandURL, "sqlscript", script, params)
}

func (c *Client) execute(
	ctx context.Context,
	endpoint string,
	language string,
	statement string,
	params map[string]any,
) ([]map[string]any, error) {
	if strings.TrimSpace(statement) == "" {
		return nil, ErrEmptyStatement
	}
	// limit: -1 disables ArcadeDB's HTTP result cap, which is 20000 rows and
	// SILENT -- a query matching more comes back short with no error, no flag and
	// no truncation marker. Measured: a co-occurrence query over 48561 entity
	// pairs returned exactly 20000, and the community detection built on it
	// reported a clean result for 41% of the graph.
	//
	// Every statement this package sends carries its own LIMIT where a bound is
	// wanted, so the transport should not be quietly adding a second one.
	payload := map[string]any{"language": language, "command": statement, "limit": -1}
	if len(params) > 0 {
		payload["params"] = params
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("arcadedb: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, decodeServerError(resp)
	}
	var decoded struct {
		Result []map[string]any `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("arcadedb: decode response: %w", err)
	}
	return decoded.Result, nil
}

func decodeServerError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	failure := &ServerError{Status: resp.StatusCode}
	var decoded struct {
		Error     string `json:"error"`
		Detail    string `json:"detail"`
		Exception string `json:"exception"`
	}
	if json.Unmarshal(raw, &decoded) == nil {
		failure.Kind = decoded.Error
		failure.Detail = decoded.Detail
		failure.Exception = decoded.Exception
	}
	if failure.Kind == "" && failure.Detail == "" {
		failure.Detail = strings.TrimSpace(string(raw))
	}
	return failure
}

// WithEmbedder attaches the dense leg. A nil embedder is legal and leaves
// retrieval lexical, so a caller can pass whatever configuration produced
// without branching.
func (c *Client) WithEmbedder(e Embedder) *Client {
	if c != nil {
		c.embedder = e
	}
	return c
}
