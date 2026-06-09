//go:build neo4j_integration && db_integration

// Live-container integration tests (B1 split). Require a running Neo4j +
// mcp-neo4j-cypher on PATH + the aura-llama-embed sidecar + Postgres with the
// aura.knowledge_migrations table. Run via:
//
//	make neo4j-migrate && go test -race -tags 'db_integration neo4j_integration' ./internal/knowledge/...
//
// goleak.VerifyTestMain leak-detection is provided by the package's sole
// TestMain in client_unit_test.go (the no-build-tag file is compiled into this
// build too); a second TestMain here would be a duplicate-symbol link error.
package knowledge

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func testConfig(t *testing.T) *Config {
	t.Helper()
	pw := envOrSkipCI(t, "NEO4J_PASSWORD", "live Neo4j integration test")
	return &Config{
		BoltURL:           envOr("AURA_NEO4J_BOLT_URL", "bolt://127.0.0.1:7687"),
		User:              envOr("NEO4J_USER", "neo4j"),
		Password:          pw,
		Database:          envOr("AURA_NEO4J_DATABASE", "neo4j"),
		MCPBinary:         envOr("AURA_MCP_NEO4J_CYPHER_BIN", "mcp-neo4j-cypher"),
		ConnectTimeoutSec: 10,
		EmbedURL:          envOr("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081"),
		EmbedDimensions:   envIntOr(t, "AURA_EMBED_DIMENSIONS", DefaultEmbedDimensions),
	}
}

func testPool(ctx context.Context, t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := envOrSkipCI(t, "AURA_DB_URL", "audit-row integration test")
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatalf("open pg pool: %v", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		t.Fatalf("ping pg: %v", err)
	}
	return pool
}

func openTestMCP(ctx context.Context, t *testing.T) *Client {
	t.Helper()
	mcp, err := Open(ctx, testConfig(t))
	if err != nil {
		t.Fatalf("open mcp: %v", err)
	}
	return mcp
}

func openTestSchema(ctx context.Context, t *testing.T) *SchemaExecutor {
	t.Helper()
	schema, err := OpenSchema(ctx, testConfig(t))
	if err != nil {
		t.Fatalf("open schema executor: %v", err)
	}
	return schema
}

func TestPing_ReturnsServerVersion(t *testing.T) {
	ctx := context.Background()
	mcp := openTestMCP(ctx, t)
	defer mcp.Close()

	version, err := pingMCP(ctx, mcp)
	if err != nil {
		t.Fatalf("pingMCP: %v", err)
	}
	if !strings.HasPrefix(version, "5.26") {
		t.Errorf("server version: want 5.26.x, got %q", version)
	}
}

// TestPing_Full exercises the composed Ping (MCP version check + embed dim probe).
func TestPing_Full(t *testing.T) {
	ctx := context.Background()
	mcp := openTestMCP(ctx, t)
	defer mcp.Close()
	res, err := Ping(ctx, mcp, testConfig(t))
	if err != nil {
		t.Fatalf("Ping: %v", err)
	}
	wantDim := testConfig(t).EmbedDimensions
	if !strings.HasPrefix(res.ServerVersion, "5.26") || res.EmbedDim != wantDim {
		t.Errorf("Ping result: %+v", res)
	}
}

// TestReset_ReappliesSchema exercises the apply path end-to-end: Reset drops the
// schema, clears the audit, and re-Migrates (covering Exec, splitCypherStatements,
// and the Migrate apply branch). After it, version 1 is freshly recorded.
func TestReset_ReappliesSchema(t *testing.T) {
	ctx := context.Background()
	schema := openTestSchema(ctx, t)
	defer schema.Close(ctx)
	pool := testPool(ctx, t)
	defer pool.Close()

	if err := Reset(ctx, schema, pool); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	rows, err := Status(ctx, pool)
	if err != nil {
		t.Fatalf("Status after reset: %v", err)
	}
	if len(rows) != 1 || rows[0].Version != 1 {
		t.Fatalf("after reset want exactly version 1 applied, got %+v", rows)
	}
	// Schema must be back: a second migrate is a no-op.
	n, err := Migrate(ctx, schema, pool)
	if err != nil || n != 0 {
		t.Fatalf("re-migrate after reset: n=%d err=%v (want 0,nil)", n, err)
	}
}

func TestPingEmbed_Live(t *testing.T) {
	ctx := context.Background()
	cfg := testConfig(t)
	if err := pingEmbed(ctx, cfg.EmbedURL, cfg.EmbedDimensions); err != nil {
		t.Fatalf("pingEmbed live: %v", err)
	}
}

func TestCypherMigrate_Idempotent(t *testing.T) {
	ctx := context.Background()
	schema := openTestSchema(ctx, t)
	defer schema.Close(ctx)
	pool := testPool(ctx, t)
	defer pool.Close()

	if _, err := Migrate(ctx, schema, pool); err != nil {
		t.Fatalf("first migrate: %v", err)
	}
	n, err := Migrate(ctx, schema, pool)
	if err != nil {
		t.Fatalf("second migrate: %v", err)
	}
	if n != 0 {
		t.Errorf("idempotency: second migrate applied %d, want 0", n)
	}
}

func TestCypherMigrate_WritesAuditRow(t *testing.T) {
	ctx := context.Background()
	schema := openTestSchema(ctx, t)
	defer schema.Close(ctx)
	pool := testPool(ctx, t)
	defer pool.Close()

	if _, err := Migrate(ctx, schema, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	rows, err := Status(ctx, pool)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	found := false
	for _, r := range rows {
		if r.Version == 1 && r.Name == "0001_init" {
			found = true
		}
	}
	if !found {
		t.Errorf("audit row for version 1 (0001_init) not found in %+v", rows)
	}
}

func TestMCPCrash_FailsAura(t *testing.T) {
	ctx := context.Background()
	mcp := openTestMCP(ctx, t)
	defer mcp.Close()

	// Warm the channel, then kill the subprocess mid-session (D-06 scenario).
	if _, err := mcp.Cypher(ctx, "RETURN 1 AS one", nil, false); err != nil {
		t.Fatalf("warm-up cypher: %v", err)
	}
	if err := mcp.cmd.Process.Kill(); err != nil {
		t.Fatalf("kill mcp subprocess: %v", err)
	}
	// Give the OS a moment to tear down the pipe.
	time.Sleep(200 * time.Millisecond)

	_, err := mcp.Cypher(ctx, "RETURN 1 AS one", nil, false)
	if err == nil {
		t.Fatal("expected error after MCP crash, got nil (D-06 violated)")
	}
	if !strings.Contains(err.Error(), crashHint) {
		t.Errorf("crash error missing D-06 literal %q\n  got: %v", crashHint, err)
	}
}

// TestMigrate_ChecksumMismatch inserts an audit row for version 1 with a wrong
// checksum, then asserts Migrate refuses with a history-corruption error.
func TestMigrate_ChecksumMismatch(t *testing.T) {
	ctx := context.Background()
	schema := openTestSchema(ctx, t)
	defer schema.Close(ctx)
	pool := testPool(ctx, t)
	defer pool.Close()

	// Clean slate, then poison the audit row.
	if err := Reset(ctx, schema, pool); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := pool.Exec(ctx, "UPDATE aura.knowledge_migrations SET checksum='deadbeef' WHERE version=1"); err != nil {
		t.Fatalf("poison checksum: %v", err)
	}
	if _, err := Migrate(ctx, schema, pool); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("want checksum mismatch error, got %v", err)
	}
	// Restore a clean state for downstream tests.
	if err := Reset(ctx, schema, pool); err != nil {
		t.Fatalf("restore reset: %v", err)
	}
}

// TestMigrate_ClosedPool covers the ListApplied error branch in Migrate.
func TestMigrate_ClosedPool(t *testing.T) {
	ctx := context.Background()
	schema := openTestSchema(ctx, t)
	defer schema.Close(ctx)
	pool := testPool(ctx, t)
	pool.Close()
	if _, err := Migrate(ctx, schema, pool); err == nil {
		t.Fatal("want error from Migrate on a closed pool")
	}
}

// TestSchemaExec_Error asserts a bad statement surfaces a driver error.
func TestSchemaExec_Error(t *testing.T) {
	ctx := context.Background()
	schema := openTestSchema(ctx, t)
	defer schema.Close(ctx)
	if err := schema.Exec(ctx, "THIS IS NOT VALID CYPHER"); err == nil {
		t.Fatal("want error on invalid Cypher")
	}
}

// TestReset_ClosedPool covers Reset's audit-clear (pool.Exec) error branch: the
// schema DROPs succeed, then the DELETE against a closed pool fails.
func TestReset_ClosedPool(t *testing.T) {
	ctx := context.Background()
	schema := openTestSchema(ctx, t)
	defer schema.Close(ctx)
	pool := testPool(ctx, t)
	pool.Close()
	if err := Reset(ctx, schema, pool); err == nil {
		t.Fatal("want error from Reset when the audit pool is closed")
	}
	// Restore a clean state for downstream tests with a fresh pool.
	fresh := testPool(ctx, t)
	defer fresh.Close()
	if err := Reset(ctx, schema, fresh); err != nil {
		t.Fatalf("restore reset: %v", err)
	}
}

// TestMigrate_ExecError covers Migrate's apply-time Exec error branch: with a
// cleared audit (so version 1 is pending) and a closed driver, the CREATE Exec
// fails.
func TestMigrate_ExecError(t *testing.T) {
	ctx := context.Background()
	pool := testPool(ctx, t)
	defer pool.Close()
	if _, err := pool.Exec(ctx, "DELETE FROM aura.knowledge_migrations"); err != nil {
		t.Fatalf("clear audit: %v", err)
	}
	closed := openTestSchema(ctx, t)
	_ = closed.Close(ctx) // close the driver so Exec fails
	if _, err := Migrate(ctx, closed, pool); err == nil {
		t.Fatal("want Exec error from Migrate with a closed schema driver")
	}
	// Restore: re-migrate with a live executor.
	live := openTestSchema(ctx, t)
	defer live.Close(ctx)
	if _, err := Migrate(ctx, live, pool); err != nil {
		t.Fatalf("restore migrate: %v", err)
	}
}

// TestStatus_ClosedPool asserts Status surfaces the sqlc query error path.
func TestStatus_ClosedPool(t *testing.T) {
	ctx := context.Background()
	pool := testPool(ctx, t)
	pool.Close() // force subsequent queries to fail
	if _, err := Status(ctx, pool); err == nil {
		t.Fatal("want error querying a closed pool")
	}
}

func TestInitCypher_AllArtifactsPresent(t *testing.T) {
	ctx := context.Background()
	mcp := openTestMCP(ctx, t)
	defer mcp.Close()
	schema := openTestSchema(ctx, t)
	defer schema.Close(ctx)
	pool := testPool(ctx, t)
	defer pool.Close()
	if _, err := Migrate(ctx, schema, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	idxRaw, err := mcp.Cypher(ctx, "SHOW INDEXES YIELD name, type, options RETURN name, type, options", nil, false)
	if err != nil {
		t.Fatalf("show indexes: %v", err)
	}
	idx := string(idxRaw)
	for _, want := range []string{"chunk_embedding", "chunk_text"} {
		if !strings.Contains(idx, want) {
			t.Errorf("SHOW INDEXES missing %q\n  got: %s", want, idx)
		}
	}
	// HNSW vector index config (Amendment #18/#20): configured dim + cosine
	// (Neo4j reports
	// the similarity function uppercased as COSINE).
	wantDim := strconv.Itoa(testConfig(t).EmbedDimensions)
	if !strings.Contains(idx, wantDim) || !strings.Contains(strings.ToLower(idx), "cosine") {
		t.Errorf("chunk_embedding HNSW config missing %s/cosine\n  got: %s", wantDim, idx)
	}

	consRaw, err := mcp.Cypher(ctx, "SHOW CONSTRAINTS YIELD name RETURN name", nil, false)
	if err != nil {
		t.Fatalf("show constraints: %v", err)
	}
	if !strings.Contains(string(consRaw), "chunk_id") {
		t.Errorf("SHOW CONSTRAINTS missing chunk_id\n  got: %s", string(consRaw))
	}
}
