// Package arcadedb is a typed client for ArcadeDB's HTTP API.
//
// It talks to the database directly rather than through ArcadeDB's own MCP
// server: that server's `query` tool accepts only (database, language, query,
// limit) with no bind parameters, which makes it unusable for EmbeddingGemma's
// 768-float vectors and unsafe for user text. `/api/v1/query` and
// `/api/v1/command` take `params`, so they are the programmatic seam.
package arcadedb

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
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

// MemoryLimits are enforced in this package, below every MCP or CLI caller.
// Positive values override the defaults; zero values inherit them.
type MemoryLimits struct {
	QueryRunes           int
	EntityRunes          int
	StatementRunes       int
	PredicateRunes       int
	SourceRunIDRunes     int
	SourceMemoryIDRunes  int
	SourceMemoryIDs      int
	Results              int
	DigestFactsPerEntity int
	MaintenanceBatch     int
	DigestScan           int
	HybridCandidates     int
	DenseMaxDistance     float64
	LexicalMinScore      float64
	MinRelevance         float64
}

var defaultMemoryLimits = MemoryLimits{
	QueryRunes: 2048, EntityRunes: 512, StatementRunes: 4096,
	PredicateRunes: 100, SourceRunIDRunes: 100, SourceMemoryIDRunes: 100,
	SourceMemoryIDs: 64, Results: 100, DigestFactsPerEntity: 20,
	MaintenanceBatch: 100, DigestScan: 2000, HybridCandidates: 400,
	// 0.72 is the midpoint of a measured separation band, not a guess. On a live
	// 102-fact memory (2026-09-02) the nearest neighbour for a question the memory
	// COULD answer sat at 0.514 and 0.667; for one it could not ("ricetta della
	// pizza napoletana", "chi ha vinto il mondiale 1982") at 0.777 and 0.890. The
	// previous 0.55 fell INSIDE the true-match band, so it discarded correct facts
	// while admitting nothing useful -- the dense leg came back empty and "hybrid"
	// retrieval ran on its lexical leg alone. The bound stays because it is what
	// lets retrieval ABSTAIN: vector.neighbors always returns k neighbours however
	// far away, so without it nothing is ever "no qualified candidates".
	// Not established by that measure: conversation turns, other identities'
	// corpora, or a memory much larger than 102 facts.
	DenseMaxDistance: 0.72, LexicalMinScore: 2, MinRelevance: 0.28,
}

func (limits MemoryLimits) normalized() MemoryLimits {
	limits.QueryRunes = defaultLimit(limits.QueryRunes, defaultMemoryLimits.QueryRunes)
	limits.EntityRunes = defaultLimit(limits.EntityRunes, defaultMemoryLimits.EntityRunes)
	limits.StatementRunes = defaultLimit(limits.StatementRunes, defaultMemoryLimits.StatementRunes)
	limits.PredicateRunes = defaultLimit(limits.PredicateRunes, defaultMemoryLimits.PredicateRunes)
	limits.SourceRunIDRunes = defaultLimit(limits.SourceRunIDRunes, defaultMemoryLimits.SourceRunIDRunes)
	limits.SourceMemoryIDRunes = defaultLimit(limits.SourceMemoryIDRunes, defaultMemoryLimits.SourceMemoryIDRunes)
	limits.SourceMemoryIDs = defaultLimit(limits.SourceMemoryIDs, defaultMemoryLimits.SourceMemoryIDs)
	limits.Results = defaultLimit(limits.Results, defaultMemoryLimits.Results)
	limits.DigestFactsPerEntity = defaultLimit(limits.DigestFactsPerEntity, defaultMemoryLimits.DigestFactsPerEntity)
	limits.MaintenanceBatch = defaultLimit(limits.MaintenanceBatch, defaultMemoryLimits.MaintenanceBatch)
	limits.DigestScan = defaultLimit(limits.DigestScan, defaultMemoryLimits.DigestScan)
	limits.HybridCandidates = defaultLimit(limits.HybridCandidates, defaultMemoryLimits.HybridCandidates)
	limits.DenseMaxDistance = defaultFloatLimit(limits.DenseMaxDistance, defaultMemoryLimits.DenseMaxDistance)
	limits.LexicalMinScore = defaultFloatLimit(limits.LexicalMinScore, defaultMemoryLimits.LexicalMinScore)
	limits.MinRelevance = defaultFloatLimit(limits.MinRelevance, defaultMemoryLimits.MinRelevance)
	return limits
}

func defaultLimit(value, fallback int) int {
	if value == 0 {
		return fallback
	}
	return value
}

func defaultFloatLimit(value, fallback float64) float64 {
	if value == 0 {
		return fallback
	}
	return value
}

func (limits MemoryLimits) validate() error {
	integers := []struct {
		name  string
		value int
	}{
		{"query runes", limits.QueryRunes}, {"entity runes", limits.EntityRunes},
		{"statement runes", limits.StatementRunes}, {"predicate runes", limits.PredicateRunes},
		{"source run id runes", limits.SourceRunIDRunes}, {"source memory id runes", limits.SourceMemoryIDRunes},
		{"source memory ids", limits.SourceMemoryIDs}, {"results", limits.Results},
		{"digest facts per entity", limits.DigestFactsPerEntity}, {"maintenance batch", limits.MaintenanceBatch},
		{"digest scan", limits.DigestScan}, {"hybrid candidates", limits.HybridCandidates},
	}
	for _, limit := range integers {
		if limit.value < 0 {
			return fmt.Errorf("arcadedb: memory limit %s must be positive", limit.name)
		}
	}
	floats := []struct {
		name  string
		value float64
	}{
		{"dense max distance", limits.DenseMaxDistance},
		{"lexical min score", limits.LexicalMinScore}, {"min relevance", limits.MinRelevance},
	}
	for _, limit := range floats {
		if limit.value < 0 || math.IsNaN(limit.value) || math.IsInf(limit.value, 0) {
			return fmt.Errorf("arcadedb: memory limit %s must be finite and positive", limit.name)
		}
	}
	return nil
}

// Config addresses one database. Every field is required except Timeout and
// MemoryLimits.
type Config struct {
	BaseURL      string
	Database     string
	User         string
	Password     string
	Timeout      time.Duration
	MemoryLimits MemoryLimits
}

// Client issues statements against a single ArcadeDB database.
type Client struct {
	// baseURL is the server root. The per-database endpoints below are derived
	// from it once, but the existence and readiness probes are NOT under
	// /api/v1/server, so the root has to be kept rather than reconstructed by
	// trimming a suffix off one of them.
	baseURL    string
	queryURL   string
	commandURL string
	// serverURL is the database-lifecycle endpoint: create and drop live at the
	// SERVER, not under a database that may not exist yet.
	serverURL string
	// beginURL/commitURL/rollbackURL are the explicit-transaction endpoints
	// (transaction.go). Every other statement in this package auto-commits;
	// these three exist ONLY for the one write this package cannot make safe
	// any other way -- see attachFactSourceOnce's doc comment.
	beginURL    string
	commitURL   string
	rollbackURL string
	authHeader  string
	http        *http.Client
	limits      MemoryLimits
	// embedder is optional: with none, memory retrieval is the lexical leg alone,
	// which is the behaviour that shipped and must not regress when it is absent.
	embedder Embedder
	// facts serializes UpsertFact's attach-or-create sequence per fact_key
	// (fact_lock.go) -- see that file's doc comment for why an in-process
	// lock, not just the transactional write, is what actually closes the
	// concurrent-write race this package's own stress tests measured.
	facts factLocks
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
	base, err := normalizeBaseURL(cfg.BaseURL)
	if err != nil {
		return nil, err
	}
	database := strings.TrimSpace(cfg.Database)
	if database == "" {
		return nil, fmt.Errorf("arcadedb: database must be non-empty")
	}
	if strings.TrimSpace(cfg.User) == "" {
		return nil, fmt.Errorf("arcadedb: user must be non-empty")
	}
	if err := cfg.MemoryLimits.validate(); err != nil {
		return nil, err
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	credentials := base64.StdEncoding.EncodeToString([]byte(cfg.User + ":" + cfg.Password))
	return &Client{
		baseURL:     base,
		queryURL:    base + "/api/v1/query/" + url.PathEscape(database),
		commandURL:  base + "/api/v1/command/" + url.PathEscape(database),
		serverURL:   base + "/api/v1/server",
		beginURL:    base + "/api/v1/begin/" + url.PathEscape(database),
		commitURL:   base + "/api/v1/commit/" + url.PathEscape(database),
		rollbackURL: base + "/api/v1/rollback/" + url.PathEscape(database),
		authHeader:  "Basic " + credentials,
		http:        &http.Client{Timeout: timeout},
		limits:      cfg.MemoryLimits.normalized(),
	}, nil
}

func (c *Client) memoryLimits() MemoryLimits {
	if c == nil {
		return defaultMemoryLimits
	}
	return c.limits.normalized()
}

// ValidateBaseURL checks the shared server endpoint without requiring tenant credentials.
func ValidateBaseURL(raw string) error {
	_, err := normalizeBaseURL(raw)
	return err
}

func normalizeBaseURL(raw string) (string, error) {
	base := strings.TrimRight(strings.TrimSpace(raw), "/")
	if base == "" {
		return "", fmt.Errorf("arcadedb: base URL must be non-empty")
	}
	parsed, err := url.Parse(base)
	if err != nil {
		return "", fmt.Errorf("arcadedb: parse base URL: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("arcadedb: base URL scheme must be http or https, got %q", parsed.Scheme)
	}
	return base, nil
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
	return c.executeSession(ctx, endpoint, "", language, statement, params)
}

// executeSession is execute with an optional ArcadeDB transaction session id
// (transaction.go) attached. An empty sessionID is the ordinary auto-commit
// path every other statement in this package uses; a non-empty one scopes
// the statement to that open transaction so it sees the transaction's own
// prior writes and its effect is provisional until the transaction commits.
func (c *Client) executeSession(
	ctx context.Context,
	endpoint string,
	sessionID string,
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
	var decoded struct {
		Result []map[string]any `json:"result"`
	}
	if err := c.executeJSON(ctx, endpoint, sessionID, payload, &decoded); err != nil {
		return nil, err
	}
	return decoded.Result, nil
}

// executeJSON is the shared authenticated HTTP transport. ArcadeDB serializers
// change only the successful response shape; request construction, bounded error
// handling and the read/write endpoint split must stay identical.
func (c *Client) executeJSON(ctx context.Context, endpoint string, sessionID string, payload map[string]any, decoded any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("arcadedb: encode request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("arcadedb: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", c.authHeader)
	if sessionID != "" {
		req.Header.Set(sessionHeader, sessionID)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("arcadedb: request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return decodeServerError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(decoded); err != nil {
		return fmt.Errorf("arcadedb: decode response: %w", err)
	}
	return nil
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
