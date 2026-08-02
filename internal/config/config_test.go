package config

import (
	"net/url"
	"os"
	"strings"
	"testing"
)

// clearPostgresEnv zeroes every Postgres-related env var so each test runs from
// a known baseline regardless of what the host shell sets. It also clears the
// AURA_LLM_*/AURA_OTEL_* knobs and sets a placeholder OPENROUTER_API_KEY: since
// Slice 1, config.Load composes llm.Load (which fail-fasts on an empty key), so
// every Load() needs a non-empty key to reach the DB/embed assertions.
func clearPostgresEnv(t *testing.T) {
	t.Helper()
	keys := []string{
		"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB",
		"POSTGRES_HOST", "POSTGRES_PORT", "POSTGRES_SSLMODE",
		"AURA_DB_APP_ROLE", "AURA_DB_MIGRATE_ROLE",
		"AURA_DB_URL", "AURA_DB_MIGRATE_URL", "AURA_DB_BOOTSTRAP_URL",
		"AURA_RUN_DIR", "AURA_CONTEXT_PREVIEW_CAP_BYTES", "AURA_RUN_DIR_SWEEP_INTERVAL_SEC",
		"AURA_EMBED_BASE_URL", "AURA_EMBED_DIMENSIONS",
		"AURA_LLM_MODEL", "AURA_LLM_BASE_URL", "AURA_LLM_TEMPERATURE",
		"AURA_LLM_MAX_TOKENS", "AURA_LLM_TOTAL_TIMEOUT_SEC", "AURA_LLM_CONNECT_TIMEOUT_SEC",
		"AURA_LLM_STREAM_IDLE_TIMEOUT_SEC", "AURA_MODEL_CONTEXT_WINDOW", "AURA_MODEL_MAX_OUTPUT_TOKENS",
		"AURA_LLM_ADAPTIVE_REASONING", "AURA_SHOW_REASONING", "AURA_COMPLETION_GATE",
		"AURA_COMPLETION_CRITIC_MODEL",
		"AURA_OTEL_EXPORTER", "AURA_OTEL_ENDPOINT", "AURA_METRICS_BIND",
		"AURA_SANDBOX_AGENT_URL", "AURA_SANDBOX_AGENT_TIMEOUT_SEC", "AURA_SANDBOX_AGENT_TOKEN",
		"SEARXNG_URL", "AURA_WEB_DNS_PIN_TTL_SEC", "AURA_WEB_FETCH_MAX_BODY_BYTES",
		"AURA_WEB_CACHE_PERSISTENT", "AURA_WEB_SEARCH_TIMEOUT_SEC",
		"AURA_WEB_FETCH_TIMEOUT_SEC", "AURA_WEB_USER_AGENT",
		"AURA_MCP_SERVERS_JSON", "AURA_MCP_CONFIG",
		"AURA_SWARM_MAX_GOALS", "AURA_SWARM_CHILD_TIMEOUT_SEC", "AURA_SWARM_MAX_CONCURRENT",
		"AURA_AGUI_BIND", "AURA_AGUI_BUFFER_CAP", "AURA_AGUI_SSE_HEARTBEAT_SEC",
		"AURA_AGUI_RUN_DETACH", "AURA_AGUI_RUN_BUFFER_EVENTS", "AURA_AGUI_RUN_LINGER_SEC",
		"AURA_AGUI_RUN_MAX_WALLCLOCK_SEC", "AURA_AGUI_RUN_MAX_LIVE",
		"AURA_OBJECTSTORE_BACKEND", "AURA_OBJECTSTORE_ENDPOINT", "AURA_OBJECTSTORE_PUBLIC_ENDPOINT",
		"AURA_OBJECTSTORE_REGION", "AURA_OBJECTSTORE_BUCKET", "AURA_OBJECTSTORE_ACCESS_KEY",
		"AURA_OBJECTSTORE_SECRET_KEY", "AURA_OBJECTSTORE_PATH_STYLE",
		"AURA_ASSET_MAX_DOCUMENT_BYTES", "AURA_ASSET_MAX_IMAGE_BYTES", "AURA_ASSET_MAX_AUDIO_BYTES",
		"AURA_ASSET_PRESIGN_TTL_SEC", "AURA_ASSET_PROCESSING_CONCURRENCY",
		"TELEGRAM_API_BASE_URL", "TELEGRAM_FILE_BASE_URL", "AURA_TELEGRAM_LOCAL_BOT_API",
		"AURA_SETUP_BIND", "AURA_SETUP_TOKEN", "AURA_VISION_CLOUD",
		"MULTIMODAL_BASE_URL", "MULTIMODAL_MODEL", "MULTIMODAL_FALLBACK_MODEL",
		"STT_BASE_URL", "STT_MODEL",
		"TTS_BASE_URL", "TTS_VOICE", "TTS_FORMAT", "AURA_TTS_MAX_CHARS",
		"AURA_PROFILE_DIR", "AURA_PROFILE_CERTAINTY_N",
		"AURA_WHATSAPP_BRIDGE_URL",
		"AURA_RERANK_BASE_URL",
		"AURA_PROFILE", "AURA_OBJECTSTORE_REPLICATION_FACTOR", "GARAGE_RPC_SECRET",
		"AURA_SHELL_DESTRUCTIVE_PATTERNS", "AURA_MUSR_ISOLATION",
		"AURA_REASONING_PERSIST_MAX_RUNES",
	}
	for _, k := range keys {
		t.Setenv(k, "")
	}
	// Non-empty key so the composed llm.Load() succeeds; the LLM-specific
	// load-order is unit-tested in internal/llm/config_test.go.
	t.Setenv("OPENROUTER_API_KEY", "sk-test-config")
}

func TestLoad_MCPServersJSON(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("AURA_MCP_SERVERS_JSON", `{
		"mcpServers": {
			"calculator": {
				"command": "uvx",
				"args": [
					"--from",
					"calculator-mcp-server@git+https://github.com/chetto1983/calculator-mcp-server.git",
					"--",
					"calculator-mcp-server",
					"--stdio"
				],
				"env": ["PYTHONUNBUFFERED=1"]
			}
		}
	}`)

	cfg := LoadDB()
	got, ok := cfg.MCPServers["calculator"]
	if !ok {
		t.Fatal("calculator MCP server was not loaded")
	}
	if got.Command != "uvx" {
		t.Fatalf("calculator command = %q, want uvx", got.Command)
	}
	if len(got.Args) != 5 || got.Args[0] != "--from" || got.Args[3] != "calculator-mcp-server" {
		t.Fatalf("calculator args not preserved: %#v", got.Args)
	}
	if len(got.Env) != 1 || got.Env[0] != "PYTHONUNBUFFERED=1" {
		t.Fatalf("calculator env not preserved: %#v", got.Env)
	}
}

func TestLoad_MCPManagedConfigAndEnvOverride(t *testing.T) {
	clearPostgresEnv(t)
	dir := t.TempDir()
	path := dir + "/servers.json"
	t.Setenv("AURA_MCP_CONFIG", path)
	if err := os.WriteFile(path, []byte(`{
		"mcpServers": {
			"calculator": {"command": "uvx", "args": ["calculator-mcp-server"]},
			"disabled": {"command": "ignored", "enabled": false}
		}
	}`), 0o600); err != nil {
		t.Fatalf("write managed config: %v", err)
	}
	t.Setenv("AURA_MCP_SERVERS_JSON", `{
		"calculator": {"command": "override-calc"},
		"adhoc": {"command": "node", "args": ["server.js"]}
	}`)

	cfg := LoadDB()
	if _, ok := cfg.MCPServers["disabled"]; ok {
		t.Fatal("disabled managed MCP server should not be loaded")
	}
	if cfg.MCPServers["calculator"].Command != "override-calc" {
		t.Fatalf("env config should override managed config, got %#v", cfg.MCPServers["calculator"])
	}
	if cfg.MCPServers["adhoc"].Command != "node" {
		t.Fatalf("adhoc env server missing: %#v", cfg.MCPServers)
	}
}

// TestSwarmConfigDefaultsAndOverrides locks the Phase 9 swarm knobs (D-11/D-12/D-13):
// unset → builtin defaults; set → overrides.
func TestSwarmConfigDefaultsAndOverrides(t *testing.T) {
	clearPostgresEnv(t)

	cfg := LoadDB()
	if cfg.MaxSwarmGoals != 8 {
		t.Errorf("MaxSwarmGoals default = %d, want 8", cfg.MaxSwarmGoals)
	}
	if cfg.SwarmChildTimeoutSec != 120 {
		t.Errorf("SwarmChildTimeoutSec default = %d, want 120", cfg.SwarmChildTimeoutSec)
	}
	if cfg.MaxSwarmConcurrent != 4 {
		t.Errorf("MaxSwarmConcurrent default = %d, want 4", cfg.MaxSwarmConcurrent)
	}

	t.Setenv("AURA_SWARM_MAX_GOALS", "5")
	t.Setenv("AURA_SWARM_CHILD_TIMEOUT_SEC", "60")
	t.Setenv("AURA_SWARM_MAX_CONCURRENT", "2")
	cfg = LoadDB()
	if cfg.MaxSwarmGoals != 5 {
		t.Errorf("MaxSwarmGoals override = %d, want 5", cfg.MaxSwarmGoals)
	}
	if cfg.SwarmChildTimeoutSec != 60 {
		t.Errorf("SwarmChildTimeoutSec override = %d, want 60", cfg.SwarmChildTimeoutSec)
	}
	if cfg.MaxSwarmConcurrent != 2 {
		t.Errorf("MaxSwarmConcurrent override = %d, want 2", cfg.MaxSwarmConcurrent)
	}
}

// TestLoadDB_NoLLMKeyRequired is the CI regression for the db-migrate coupling:
// `aura db migrate` (and ping/status/reset) go through config, but they are pure
// DB operations and must NOT require OPENROUTER_API_KEY. Load() fail-fasts on an
// empty key (correct for the chat/agent path); LoadDB() must succeed without it so
// CI's migrate step (no LLM key) is not blocked.
func TestLoadDB_NoLLMKeyRequired(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("OPENROUTER_API_KEY", "") // the CI migrate-step condition: no LLM key
	t.Setenv("POSTGRES_PASSWORD", "s3cret")

	// Load() must still fail-fast (the LLM path needs the key).
	if _, err := Load(); err == nil {
		t.Fatal("Load() must fail with an empty OPENROUTER_API_KEY, got nil error")
	}

	// LoadDB() must succeed and compose the migrate DSN — no LLM key needed.
	cfg := LoadDB()
	if cfg.DB.MigrateURL == "" {
		t.Error("LoadDB(): DB.MigrateURL must compose from POSTGRES_PASSWORD, got empty")
	}
	if cfg.DB.Password != "s3cret" {
		t.Errorf("LoadDB(): DB.Password = %q, want s3cret", cfg.DB.Password)
	}
	if cfg.LLM.APIKey != "" {
		t.Error("LoadDB(): LLM must be left zero-value (DB commands never read it)")
	}
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
	if cfg.ToolPreviewCap != defaultToolPreviewCapBytes {
		t.Errorf("ToolPreviewCap: want the %d default, got %d", defaultToolPreviewCapBytes, cfg.ToolPreviewCap)
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
	if cfg.LLM.Model != "deepseek/deepseek-v4-flash:nitro" {
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
	if cfg.MetricsBind != "127.0.0.1:9464" {
		t.Errorf("MetricsBind: want default 127.0.0.1:9464, got %q", cfg.MetricsBind)
	}

	t.Setenv("AURA_METRICS_BIND", "127.0.0.1:19464")
	if got := LoadDB().MetricsBind; got != "127.0.0.1:19464" {
		t.Errorf("MetricsBind override = %q, want 127.0.0.1:19464", got)
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

func TestComposeDSNEscapesComponents(t *testing.T) {
	got := composeDSN("role", "p@ss/word", "db.internal", "5432", "aura/db", "disable&connect_timeout=0")
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse composed DSN %q: %v", got, err)
	}
	q := u.Query()
	if q.Get("connect_timeout") != "" {
		t.Fatalf("sslmode escaped incorrectly; injected connect_timeout in %q", got)
	}
	if q.Get("sslmode") != "disable&connect_timeout=0" {
		t.Fatalf("sslmode query = %q, want literal value escaped in DSN %q", q.Get("sslmode"), got)
	}
	hostInjected := composeDSN("role", "pw", "db.internal/evil", "5432", "aura", "disable")
	if strings.Contains(hostInjected, "/evil") {
		t.Fatalf("host path injection was not escaped in %q", hostInjected)
	}
}

// TestLoad_EmbedDefaultsApplied asserts the embed sub-struct composes to the
// documented defaults when no env is set.
func TestLoad_EmbedDefaultsApplied(t *testing.T) {
	clearPostgresEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Embed.BaseURL != "http://127.0.0.1:8081" {
		t.Errorf("Embed.BaseURL: want default http://127.0.0.1:8081, got %q", cfg.Embed.BaseURL)
	}
	if cfg.Embed.Model != "" {
		t.Errorf("Embed.Model: want empty (local sidecar route), got %q", cfg.Embed.Model)
	}
}

// TestLoad_EmbedEnvOverrides asserts every embed field honors its env override.
func TestLoad_EmbedEnvOverrides(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("AURA_EMBED_BASE_URL", "http://embed.internal:9000")
	t.Setenv("AURA_EMBED_DIMENSIONS", "1024")
	t.Setenv("AURA_EMBED_MODEL", "qwen/qwen3-embedding-8b")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.Embed.BaseURL != "http://embed.internal:9000" {
		t.Errorf("Embed.BaseURL override not applied: %q", cfg.Embed.BaseURL)
	}
	if cfg.Embed.Dimensions != 1024 {
		t.Errorf("Embed.Dimensions override not applied: %d", cfg.Embed.Dimensions)
	}
	if cfg.Embed.Model != "qwen/qwen3-embedding-8b" {
		t.Errorf("Embed.Model override not applied: %q", cfg.Embed.Model)
	}
}

// TestEmbedDimensions_RequiredNonZero asserts Dimensions defaults to the
// sidecar contract when AURA_EMBED_DIMENSIONS is unset — a non-zero
// value is required for the Pattern 5 boot self-test to be meaningful. A literal
// "0" is a deliberate misconfiguration that envutil.IntDefault passes through verbatim
// (Atoi succeeds); it is caught downstream by the ping dim self-test, not here.
func TestEmbedDimensions_RequiredNonZero(t *testing.T) {
	clearPostgresEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	// Anchored to the constant, not to a literal: the width has moved 384 -> 768 -> 1024
	// and each move broke this assertion without saying anything about the behaviour —
	// which is that an unset env must not leave the contract dim at zero.
	if cfg.Embed.Dimensions != DefaultEmbedDimensions {
		t.Errorf("Embed.Dimensions: want the contract default %d, got %d",
			DefaultEmbedDimensions, cfg.Embed.Dimensions)
	}
	if cfg.Embed.Dimensions == 0 {
		t.Error("EmbedDimensions must never default to zero — a zero width disables the boot self-test")
	}
}

// TestReasoningPersistMaxRunes_DefaultAndOverride asserts the amendment #91
// (fix-plan 1.12) display-only CoT persistence cap: default 65536, an override is
// honored verbatim, and <=0 (the documented off switch the runner reads) passes
// through — envutil.IntDefault semantics, non-fatal on a typo.
func TestReasoningPersistMaxRunes_DefaultAndOverride(t *testing.T) {
	clearPostgresEnv(t)

	if cfg := LoadDB(); cfg.ReasoningPersistMaxRunes != 65536 {
		t.Errorf("ReasoningPersistMaxRunes: want default 65536, got %d", cfg.ReasoningPersistMaxRunes)
	}
	t.Setenv("AURA_REASONING_PERSIST_MAX_RUNES", "1024")
	if cfg := LoadDB(); cfg.ReasoningPersistMaxRunes != 1024 {
		t.Errorf("ReasoningPersistMaxRunes: override not applied, got %d", cfg.ReasoningPersistMaxRunes)
	}
	t.Setenv("AURA_REASONING_PERSIST_MAX_RUNES", "-1")
	if cfg := LoadDB(); cfg.ReasoningPersistMaxRunes != -1 {
		t.Errorf("ReasoningPersistMaxRunes: <=0 off switch must pass through, got %d", cfg.ReasoningPersistMaxRunes)
	}
	t.Setenv("AURA_REASONING_PERSIST_MAX_RUNES", "not-a-number")
	if cfg := LoadDB(); cfg.ReasoningPersistMaxRunes != 65536 {
		t.Errorf("ReasoningPersistMaxRunes: malformed value must fall back to the default, got %d", cfg.ReasoningPersistMaxRunes)
	}
}

// TestWebDefaults_AppliedAndNotFatal asserts the Phase 7 (Slice 5) web knobs:
// the AURA_WEB_* defaults land, and — critically (D-05/D-06) — an unset
// SEARXNG_URL is NOT a boot error. Web tools are optional; `aura db migrate` and
// every non-web subcommand must load config without SEARXNG configured. Missing
// SEARXNG_URL is surfaced as web_search_unavailable{searxng_not_configured} at
// call time, not here.
func TestWebDefaults_AppliedAndNotFatal(t *testing.T) {
	clearPostgresEnv(t) // also clears every SEARXNG_URL / AURA_WEB_* knob

	// LoadDB() must succeed with SEARXNG_URL unset — no panic, no error path.
	cfg := LoadDB()
	if cfg == nil { //nolint:staticcheck // SA5011 false positive: t.Fatal below halts execution via runtime.Goexit
		t.Fatal("LoadDB() returned nil with SEARXNG_URL unset")
	}
	if cfg.SearxngURL != "" { //nolint:staticcheck // SA5011 false positive: t.Fatal (line above) halts execution via runtime.Goexit
		t.Errorf("SearxngURL: want empty default (fail-closed at call time), got %q", cfg.SearxngURL)
	}
	if cfg.WebDNSPinTTLSec != 60 {
		t.Errorf("WebDNSPinTTLSec: want default 60, got %d", cfg.WebDNSPinTTLSec)
	}
	if cfg.WebFetchMaxBodyBytes != 5_000_000 {
		t.Errorf("WebFetchMaxBodyBytes: want default 5_000_000, got %d", cfg.WebFetchMaxBodyBytes)
	}
	if cfg.WebCachePersistent {
		t.Error("WebCachePersistent: want default false (in-memory), got true")
	}
	if cfg.WebSearchTimeoutSec != 20 {
		t.Errorf("WebSearchTimeoutSec: want default 20, got %d", cfg.WebSearchTimeoutSec)
	}
	if cfg.WebFetchTimeoutSec != 10 {
		t.Errorf("WebFetchTimeoutSec: want default 10, got %d", cfg.WebFetchTimeoutSec)
	}
	// The default UA must present as a browser. A self-declaring bot UA is refused
	// at the connection layer by manufacturer/news sites, which production saw as a
	// fetch timeout on every br-automation.com call (see defaultWebUserAgent).
	if cfg.WebUserAgent != defaultWebUserAgent {
		t.Errorf("WebUserAgent: want default %q, got %q", defaultWebUserAgent, cfg.WebUserAgent)
	}
	if !strings.Contains(cfg.WebUserAgent, "Mozilla/") {
		t.Errorf("WebUserAgent default must present as a browser, got %q", cfg.WebUserAgent)
	}

	// Load() with a placeholder LLM key must also load the web fields without error.
	full, err := Load()
	if err != nil {
		t.Fatalf("Load() errored with SEARXNG_URL unset: %v", err)
	}
	if full.SearxngURL != "" {
		t.Errorf("Load(): SearxngURL must stay empty when unset, got %q", full.SearxngURL)
	}
}

// TestWebEnvOverrides asserts each AURA_WEB_* / SEARXNG_URL field honors its env.
func TestWebEnvOverrides(t *testing.T) {
	clearPostgresEnv(t)
	t.Setenv("SEARXNG_URL", "http://searxng:8080/search")
	t.Setenv("AURA_WEB_DNS_PIN_TTL_SEC", "120")
	t.Setenv("AURA_WEB_FETCH_MAX_BODY_BYTES", "48000")
	t.Setenv("AURA_WEB_CACHE_PERSISTENT", "true")
	t.Setenv("AURA_WEB_SEARCH_TIMEOUT_SEC", "15")
	t.Setenv("AURA_WEB_FETCH_TIMEOUT_SEC", "45")
	t.Setenv("AURA_WEB_USER_AGENT", "Aura/1.0 custom")

	cfg := LoadDB()
	if cfg.SearxngURL != "http://searxng:8080/search" {
		t.Errorf("SearxngURL override not applied: %q", cfg.SearxngURL)
	}
	if cfg.WebDNSPinTTLSec != 120 {
		t.Errorf("WebDNSPinTTLSec override not applied: %d", cfg.WebDNSPinTTLSec)
	}
	if cfg.WebFetchMaxBodyBytes != 48000 {
		t.Errorf("WebFetchMaxBodyBytes override not applied: %d", cfg.WebFetchMaxBodyBytes)
	}
	if !cfg.WebCachePersistent {
		t.Error("WebCachePersistent override not applied: want true")
	}
	if cfg.WebSearchTimeoutSec != 15 {
		t.Errorf("WebSearchTimeoutSec override not applied: %d", cfg.WebSearchTimeoutSec)
	}
	if cfg.WebFetchTimeoutSec != 45 {
		t.Errorf("WebFetchTimeoutSec override not applied: %d", cfg.WebFetchTimeoutSec)
	}
	if cfg.WebUserAgent != "Aura/1.0 custom" {
		t.Errorf("WebUserAgent override not applied: %q", cfg.WebUserAgent)
	}
}

// TestMUSRIsolationDefaultOff locks the D-13 rollout switch: unset ⇒ false so plan
// 12's "deploy flag-off" step is safe (the documents-retrieval scoped-vs-unscoped path
// selector defaults to the unscoped fallback, no enforcement); set-true ⇒ true so plan
// 12 can flip it after the backfill. Read straight from AURA_MUSR_ISOLATION as a
// dedicated config field (never through the internal/settings OverlayEnv allowlist).
func TestMUSRIsolationDefaultOff(t *testing.T) {
	clearPostgresEnv(t)

	if cfg := LoadDB(); cfg.MUSRIsolation {
		t.Error("MUSRIsolation default = true, want false (D-13 deploy flag-off is load-bearing)")
	}

	t.Setenv("AURA_MUSR_ISOLATION", "true")
	if cfg := LoadDB(); !cfg.MUSRIsolation {
		t.Error("MUSRIsolation override = false, want true when AURA_MUSR_ISOLATION=true")
	}
}

// TestTTSMaxChars_DefaultAndOverride locks the 37C-02 web-voice knob (D-05):
// unset ⇒ the 4096 provider ceiling (OpenAI-compatible /audio/speech, which
// OpenRouter proxies, hard-400s a longer input), set ⇒ the override is parsed.
// The soft cap bounds per-character synth cost + latency on POST /api/tts.
func TestTTSMaxChars_DefaultAndOverride(t *testing.T) {
	clearPostgresEnv(t)

	if cfg := LoadDB(); cfg.TTSMaxChars != 4096 {
		t.Errorf("TTSMaxChars default = %d, want 4096 (the /audio/speech provider ceiling)", cfg.TTSMaxChars)
	}

	t.Setenv("AURA_TTS_MAX_CHARS", "1200")
	if cfg := LoadDB(); cfg.TTSMaxChars != 1200 {
		t.Errorf("TTSMaxChars override = %d, want 1200", cfg.TTSMaxChars)
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
