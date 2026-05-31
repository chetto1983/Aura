// Package config is the thin root composite read by every cmd/aura subcommand.
// Per CONTEXT.md D-row "Composition": per-subsystem configs (db, knowledge, llm)
// live in their owning packages; this file only wires the top-level fields and
// composes credentials from POSTGRES_* primitives.
//
// Slice 0.5 form: DB only. Slice 0.7 added `Neo4j knowledge.Config`; Phase 3
// (Slice 1) adds `LLM llm.Config` + the AURA_OTEL_* tracing knobs. No new fields
// land here without an owning slice plan.
package config

import (
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/joho/godotenv"
)

// OTel exporter knob defaults (D-05/D-06). The default OTLP target silent-drops
// without a collector; "none" is a true no-op provider.
const (
	defaultOtelExporter = "otlp"
	defaultOtelEndpoint = "localhost:4317"
)

// Config is the root composite. Subsystem configs live in their packages.
type Config struct {
	DB             db.Config
	Neo4j          knowledge.Config // Slice 0.7 — graph + vector + embed sidecar wiring
	LLM            llm.Config       // Slice 1 — OpenAI-compat client + load-order chain (D-22)
	RunDir         string
	ToolPreviewCap int
	OtelExporter   string // AURA_OTEL_EXPORTER ∈ {stdout,otlp,none} (D-06)
	OtelEndpoint   string // AURA_OTEL_ENDPOINT — OTLP/gRPC target (D-06)

	// Phase 4 (Slice 1.8) conversation + context-management tuning knobs.
	// Non-fatal envIntDefault fallbacks (an ad-hoc tweak typo falls back, not boots-fatal).
	ConversationTurnCapBytes   int // AURA_CONVERSATION_TURN_CAP_BYTES — content > this spills to a sidecar file
	ContextToolEvictAfterTurns int // AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS — L1 microcompact eviction age
	HistoryHardCapTurns        int // AURA_HISTORY_HARD_CAP_TURNS — L2.5 picobot hard rolling buffer cap
	RunDirWarnThresholdBytes   int // AURA_RUN_DIR_WARN_THRESHOLD_BYTES — boot du WARN threshold (audit-only)
}

// Load reads .env (best-effort) then populates a Config from environment
// variables. A missing .env file is not an error — production deployments
// rely on real environment variables, not on the .env shim.
//
// Postgres DSN composition: AURA_DB_URL / AURA_DB_MIGRATE_URL overrides take
// precedence (production managed Postgres). Otherwise URLs are composed from
// POSTGRES_* primitives + AURA_DB_*_ROLE role names. Single POSTGRES_PASSWORD
// fans out to both runtime + DDL roles for local-dev ergonomics.
func Load() (*Config, error) {
	cfg := loadBase()
	// LLM config owns its own 4-tier load order + fail-fast empty-key (D-22);
	// its error (e.g. ErrMissingAPIKey) propagates through this composite.
	llmCfg, err := llm.Load()
	if err != nil {
		return nil, fmt.Errorf("config: load llm: %w", err)
	}
	cfg.LLM = *llmCfg
	return cfg, nil
}

// LoadDB loads the non-LLM configuration only. DB-admin commands
// (aura db migrate/ping/status/reset) must NOT require an LLM API key — migration
// is a pure DB operation, and Load's fail-fast empty-key (D-22) would otherwise
// block `aura db migrate` wherever OPENROUTER_API_KEY is unset (notably CI's
// migrate step). LLM is left zero-value; DB commands never read it.
func LoadDB() *Config {
	return loadBase()
}

// loadBase reads every non-LLM config source. Load layers the LLM config (with its
// fail-fast) on top; LoadDB returns this as-is so DB-only commands skip the key.
func loadBase() *Config {
	_ = godotenv.Load() // best-effort; missing .env is not fatal

	pgUser := envDefault("POSTGRES_USER", "aura")
	pgPassword := os.Getenv("POSTGRES_PASSWORD")
	pgHost := envDefault("POSTGRES_HOST", "127.0.0.1")
	pgPort := envDefault("POSTGRES_PORT", "5432")
	pgDB := envDefault("POSTGRES_DB", "aura")
	pgSSL := envDefault("POSTGRES_SSLMODE", "disable")
	appRole := envDefault("AURA_DB_APP_ROLE", "aura_app")
	migrateRole := envDefault("AURA_DB_MIGRATE_ROLE", "aura_migrate")

	appURL := os.Getenv("AURA_DB_URL")
	if appURL == "" {
		appURL = composeDSN(appRole, pgPassword, pgHost, pgPort, pgDB, pgSSL)
	}
	migrateURL := os.Getenv("AURA_DB_MIGRATE_URL")
	if migrateURL == "" {
		migrateURL = composeDSN(migrateRole, pgPassword, pgHost, pgPort, pgDB, pgSSL)
	}
	bootstrapURL := os.Getenv("AURA_DB_BOOTSTRAP_URL")
	if bootstrapURL == "" {
		bootstrapURL = composeDSN(pgUser, pgPassword, pgHost, pgPort, pgDB, pgSSL)
	}

	return &Config{
		DB: db.Config{
			URL:          appURL,
			MigrateURL:   migrateURL,
			BootstrapURL: bootstrapURL,
			Password:     pgPassword,
		},
		Neo4j: knowledge.Config{
			BoltURL:           envDefault("AURA_NEO4J_BOLT_URL", "bolt://127.0.0.1:7687"),
			User:              envDefault("NEO4J_USER", "neo4j"),
			Password:          os.Getenv("NEO4J_PASSWORD"),
			Database:          envDefault("AURA_NEO4J_DATABASE", "neo4j"),
			MCPBinary:         envDefault("AURA_MCP_NEO4J_CYPHER_BIN", "mcp-neo4j-cypher"),
			ConnectTimeoutSec: envIntDefault("AURA_MCP_NEO4J_CONNECT_TIMEOUT_SEC", 10),
			EmbedURL:          envDefault("AURA_EMBED_BASE_URL", "http://127.0.0.1:8081"),
			EmbedDimensions:   envIntDefault("AURA_EMBED_DIMENSIONS", 768),
		},
		RunDir:         envDefault("AURA_RUN_DIR", defaultRunDir()),
		ToolPreviewCap: envIntDefault("AURA_CONTEXT_PREVIEW_CAP_BYTES", 2048),
		OtelExporter:   envDefault("AURA_OTEL_EXPORTER", defaultOtelExporter),
		OtelEndpoint:   envDefault("AURA_OTEL_ENDPOINT", defaultOtelEndpoint),

		ConversationTurnCapBytes:   envIntDefault("AURA_CONVERSATION_TURN_CAP_BYTES", 65536),
		ContextToolEvictAfterTurns: envIntDefault("AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS", 10),
		HistoryHardCapTurns:        envIntDefault("AURA_HISTORY_HARD_CAP_TURNS", 50),
		RunDirWarnThresholdBytes:   envIntDefault("AURA_RUN_DIR_WARN_THRESHOLD_BYTES", 1073741824),
	}
}

// composeDSN returns "" when password is empty so callers can detect an
// unconfigured DSN and fail-fast instead of dialing with a blank credential.
func composeDSN(role, password, host, port, dbname, sslmode string) string {
	if password == "" {
		return ""
	}
	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=%s",
		url.QueryEscape(role),
		url.QueryEscape(password),
		host, port,
		url.QueryEscape(dbname),
		sslmode,
	)
}

// envDefault returns the value of `key` from the environment, falling back to
// `fallback` when the variable is unset or empty.
func envDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// envIntDefault returns the integer value of `key`, falling back to `fallback`
// when the variable is unset, empty, or fails to parse as an int. Parsing
// failures are silently absorbed — the fallback is preferable to a fatal boot
// error on a misformatted ad-hoc env tweak.
func envIntDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// defaultRunDir returns a sensible per-user run directory for sidecar tool
// outputs. Falls back to a tmp-based path if user cache is unavailable.
func defaultRunDir() string {
	if cache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cache, "aura")
	}
	return filepath.Join(os.TempDir(), "aura")
}
