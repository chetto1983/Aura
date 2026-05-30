package config

import (
	"strings"
	"testing"
)

// clearPostgresEnv zeroes every Postgres-related env var so each test runs from
// a known baseline regardless of what the host shell sets. It also clears the
// AURA_LLM_*/AURA_OTEL_* knobs and sets a placeholder OPENROUTER_API_KEY: since
// Slice 1, config.Load composes llm.Load (which fail-fasts on an empty key), so
// every Load() needs a non-empty key to reach the DB/Neo4j assertions.
func clearPostgresEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB",
		"POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_SSLMODE",
		"AURA_DB_APP_ROLE", "AURA_DB_MIGRATE_ROLE",
		"AURA_DB_URL", "AURA_DB_MIGRATE_URL", "AURA_DB_BOOTSTRAP_URL",
		"AURA_RUN_DIR", "AURA_CONTEXT_PREVIEW_CAP_BYTES",
		"NEO4J_USER", "NEO4J_PASSWORD", "AURA_NEO4J_BOLT_URL", "AURA_NEO4J_DATABASE",
		"AURA_MCP_NEO4J_CYPHER_BIN", "AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC",
		"AURA_EMBED_BASE_URL", "AURA_EMBED_DIMENSIONS",
		"AURA_LLM_MODEL", "AURA_LLM_BASE_URL", "AURA_LLM_TEMPERATURE",
		"AURA_LLM_MAX_TOKENS", "AURA_LLM_TOTAL_TIMEOUT_SEC", "AURA_LLM_CONNECT_TIMEOUT_SEC",
		"AURA_OTEL_EXPORTER", "AURA_OTEL_ENDPOINT",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
	// Non-empty key so the composed llm.Load() succeeds; the LLM-specific
	// load-order is unit-tested in internal/llm/config_test.go.
	t.Setenv("OPENROUTER_API_KEY", "sk-test-config")
}

func TestLoad_DefaultsApplied(t *testing.T) {
	clearPostgresEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	// With no password and no override, all DSNs compose to empty.
	if cfg.DB.URL != "" {
		t.Errorf("DB.URL: want empty (no POSTGRES_PASSWORD), got %q", cfg.DB.URL)
	}
	if cfg.DB.MigrateURL != "" {
		t.Errorf("DB.MigrateURL: want empty (no POSTGRES_PASSWORD), got %q", cfg.DB.MigrateURL)
	}
	if cfg.DB.BootstrapURL != "" {
		t.Errorf("DB.BootstrapURL: want empty (no POSTGRES_PASSWORD), got %q", cfg.DB.BootstrapURL)
	}
	if cfg.DB.Password != "" {
		t.Errorf("DB.Password: want empty, got %q", cfg.DB.Password)
	}
	if cfg.RunDir == "" {
		t.Error("RunDir: want a non-empty default path, got empty")
	}
	if cfg.ToolPreviewCap != 2048 {
		t.Errorf("ToolPreviewCap: want 2048 default, got %d", cfg.ToolPreviewCap)
	}
}

// TestLoad_LLMAndOtelComposed asserts the Slice 1 additions: the LLM sub-struct
// is populated via llm.Load (with the placeholder key from clearPostgresEnv) and
// the AURA_OTEL_* knobs default to otlp/localhost:4317.
func TestLoad_LLMAndOtelComposed(t *testing.T) {
	clearPostgresEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.LLM.Model != "deepseek/deepseek-v4-flash:exacto" {
		t.Errorf("LLM.Model: want default, got %q", cfg.LLM.Model)
	}
	if cfg.LLM.BaseURL != "https://openrouter.ai/api/v1" {
		t.Errorf("LLM.BaseURL: want default, got %q", cfg.LLM.BaseURL)
	}
	if cfg.LLM.APIKey != "sk-test-config" {
		t.Errorf("LLM.APIKey: want the placeholder key, got %q", cfg.LLM.APIKey)
	}
	if cfg.OtelExporter != "otlp" {
		t.Errorf("OtelExporter: want default otlp, got %q", cfg.OtelExporter)
	}
	if cfg.OtelEndpoint != "localhost:4317" {
		t.Errorf("OtelEndpoint: want default localhost:4317, got %q", cfg.OtelEndpoint)
	}
}

func TestLoad_ComposesFromPrimitives(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("POSTGRES_PASSWORD", "s3cret")
	// User/DB/Host/Port/SSLMode all fall back to defaults (aura/aura/127.0.0.1/5432/disable).

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	wantApp := "postgres://aura_app:s3cret@127.0.0.1:5432/aura?sslmode=disable"
	if cfg.DB.URL != wantApp {
		t.Errorf("DB.URL composed: want %q, got %q", wantApp, cfg.DB.URL)
	}
	wantMigrate := "postgres://aura_migrate:s3cret@127.0.0.1:5432/aura?sslmode=disable"
	if cfg.DB.MigrateURL != wantMigrate {
		t.Errorf("DB.MigrateURL composed: want %q, got %q", wantMigrate, cfg.DB.MigrateURL)
	}
	wantBootstrap := "postgres://aura:s3cret@127.0.0.1:5432/aura?sslmode=disable"
	if cfg.DB.BootstrapURL != wantBootstrap {
		t.Errorf("DB.BootstrapURL composed: want %q, got %q", wantBootstrap, cfg.DB.BootstrapURL)
	}
	if cfg.DB.Password != "s3cret" {
		t.Errorf("DB.Password: want %q, got %q", "s3cret", cfg.DB.Password)
	}
}

func TestLoad_FullDSNOverrides(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("POSTGRES_PASSWORD", "ignored-because-of-override")
	t.Setenv("AURA_DB_URL", "postgres://prod_app:prod_pwd@db.prod:5432/aura?sslmode=require")
	t.Setenv("AURA_DB_MIGRATE_URL", "postgres://prod_migrate:prod_pwd@db.prod:5432/aura?sslmode=require")
	t.Setenv("AURA_DB_BOOTSTRAP_URL", "postgres://prod_super:prod_pwd@db.prod:5432/aura?sslmode=require")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !strings.Contains(cfg.DB.URL, "prod_app") {
		t.Errorf("DB.URL: override not applied, got %q", cfg.DB.URL)
	}
	if !strings.Contains(cfg.DB.MigrateURL, "prod_migrate") {
		t.Errorf("DB.MigrateURL: override not applied, got %q", cfg.DB.MigrateURL)
	}
	if !strings.Contains(cfg.DB.BootstrapURL, "prod_super") {
		t.Errorf("DB.BootstrapURL: override not applied, got %q", cfg.DB.BootstrapURL)
	}
}

func TestLoad_PrimitivesAndRoleOverrides(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("POSTGRES_USER", "supr")
	t.Setenv("POSTGRES_PASSWORD", "p@ss/word") // includes URL-significant chars
	t.Setenv("POSTGRES_DB", "auradb")
	t.Setenv("POSTGRES_HOST", "db.internal")
	t.Setenv("POSTGRES_PORT", "6432")
	t.Setenv("POSTGRES_SSLMODE", "require")
	t.Setenv("AURA_DB_APP_ROLE", "myapp")
	t.Setenv("AURA_DB_MIGRATE_ROLE", "mymigrate")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	// Password chars must be URL-escaped (/ → %2F, @ stays raw since it's after colon).
	if !strings.Contains(cfg.DB.URL, "myapp:p%40ss%2Fword@db.internal:6432/auradb?sslmode=require") {
		t.Errorf("DB.URL composition mishandled special chars: %q", cfg.DB.URL)
	}
	if !strings.Contains(cfg.DB.MigrateURL, "mymigrate:") {
		t.Errorf("DB.MigrateURL did not use AURA_DB_MIGRATE_ROLE: %q", cfg.DB.MigrateURL)
	}
	if !strings.Contains(cfg.DB.BootstrapURL, "supr:") {
		t.Errorf("DB.BootstrapURL did not use POSTGRES_USER: %q", cfg.DB.BootstrapURL)
	}
}

// TestLoad_Neo4jDefaultsApplied asserts the Slice 0.7 Neo4j sub-struct composes
// to the documented defaults when no env is set. Password stays empty (operator
// secret) so callers fail-fast rather than dialing with a blank credential.
func TestLoad_Neo4jDefaultsApplied(t *testing.T) {
	clearPostgresEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Neo4j.BoltURL != "bolt://127.0.0.1:7687" {
		t.Errorf("Neo4j.BoltURL: want default bolt://127.0.0.1:7687, got %q", cfg.Neo4j.BoltURL)
	}
	if cfg.Neo4j.User != "neo4j" {
		t.Errorf("Neo4j.User: want default neo4j, got %q", cfg.Neo4j.User)
	}
	if cfg.Neo4j.Password != "" {
		t.Errorf("Neo4j.Password: want empty (operator secret), got %q", cfg.Neo4j.Password)
	}
	if cfg.Neo4j.Database != "neo4j" {
		t.Errorf("Neo4j.Database: want default neo4j (Community single-DB), got %q", cfg.Neo4j.Database)
	}
	if cfg.Neo4j.MCPBinary != "mcp-neo4j-cypher" {
		t.Errorf("Neo4j.MCPBinary: want default mcp-neo4j-cypher, got %q", cfg.Neo4j.MCPBinary)
	}
	if cfg.Neo4j.ConnectTimeoutSec != 10 {
		t.Errorf("Neo4j.ConnectTimeoutSec: want default 10, got %d", cfg.Neo4j.ConnectTimeoutSec)
	}
	if cfg.Neo4j.EmbedURL != "http://127.0.0.1:8081" {
		t.Errorf("Neo4j.EmbedURL: want default http://127.0.0.1:8081, got %q", cfg.Neo4j.EmbedURL)
	}
}

// TestLoad_Neo4jEnvOverrides asserts every Neo4j field honors its env override.
func TestLoad_Neo4jEnvOverrides(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("AURA_NEO4J_BOLT_URL", "bolt://neo.internal:7000")
	t.Setenv("NEO4J_USER", "graphuser")
	t.Setenv("NEO4J_PASSWORD", "gr@ph-pw")
	t.Setenv("AURA_NEO4J_DATABASE", "neo4j")
	t.Setenv("AURA_MCP_NEO4J_CYPHER_BIN", "/opt/bin/mcp-neo4j-cypher")
	t.Setenv("AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC", "25")
	t.Setenv("AURA_EMBED_BASE_URL", "http://embed.internal:9000")
	t.Setenv("AURA_EMBED_DIMENSIONS", "1024")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Neo4j.BoltURL != "bolt://neo.internal:7000" {
		t.Errorf("Neo4j.BoltURL override not applied: %q", cfg.Neo4j.BoltURL)
	}
	if cfg.Neo4j.User != "graphuser" {
		t.Errorf("Neo4j.User override not applied: %q", cfg.Neo4j.User)
	}
	if cfg.Neo4j.Password != "gr@ph-pw" {
		t.Errorf("Neo4j.Password override not applied: %q", cfg.Neo4j.Password)
	}
	if cfg.Neo4j.MCPBinary != "/opt/bin/mcp-neo4j-cypher" {
		t.Errorf("Neo4j.MCPBinary override not applied: %q", cfg.Neo4j.MCPBinary)
	}
	if cfg.Neo4j.ConnectTimeoutSec != 25 {
		t.Errorf("Neo4j.ConnectTimeoutSec override not applied: %d", cfg.Neo4j.ConnectTimeoutSec)
	}
	if cfg.Neo4j.EmbedURL != "http://embed.internal:9000" {
		t.Errorf("Neo4j.EmbedURL override not applied: %q", cfg.Neo4j.EmbedURL)
	}
	if cfg.Neo4j.EmbedDimensions != 1024 {
		t.Errorf("Neo4j.EmbedDimensions override not applied: %d", cfg.Neo4j.EmbedDimensions)
	}
}

// TestEmbedDimensions_RequiredNonZero asserts EmbedDimensions defaults to the
// 768 contract (Amendment #18) when AURA_EMBED_DIMENSIONS is unset — a non-zero
// value is required for the Pattern 5 boot self-test to be meaningful. A literal
// "0" is a deliberate misconfiguration that envIntDefault passes through verbatim
// (Atoi succeeds); it is caught downstream by the ping dim self-test, not here.
func TestEmbedDimensions_RequiredNonZero(t *testing.T) {
	clearPostgresEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Neo4j.EmbedDimensions != 768 {
		t.Errorf("EmbedDimensions: want non-zero contract default 768, got %d", cfg.Neo4j.EmbedDimensions)
	}
}

func TestEnvIntDefault_ParsesValid_FallsBackOnGarbage(t *testing.T) {
	t.Setenv("AURA_TEST_INT_VALID", "42")
	if got := envIntDefault("AURA_TEST_INT_VALID", 7); got != 42 {
		t.Errorf("valid int: want 42, got %d", got)
	}

	t.Setenv("AURA_TEST_INT_GARBAGE", "not-a-number")
	if got := envIntDefault("AURA_TEST_INT_GARBAGE", 7); got != 7 {
		t.Errorf("garbage: want fallback 7, got %d", got)
	}

	t.Setenv("AURA_TEST_INT_EMPTY", "")
	if got := envIntDefault("AURA_TEST_INT_EMPTY", 13); got != 13 {
		t.Errorf("empty: want fallback 13, got %d", got)
	}
}

func TestEnvDefault_FallbackOnUnset(t *testing.T) {
	t.Setenv("AURA_TEST_STR_EMPTY", "")
	if got := envDefault("AURA_TEST_STR_EMPTY", "fallback"); got != "fallback" {
		t.Errorf("empty: want fallback, got %q", got)
	}

	t.Setenv("AURA_TEST_STR_SET", "real")
	if got := envDefault("AURA_TEST_STR_SET", "fallback"); got != "real" {
		t.Errorf("set: want 'real', got %q", got)
	}
}
