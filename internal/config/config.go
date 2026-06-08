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
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	mcpmanager "github.com/chetto1983/aura/internal/mcp/manager"
	"github.com/chetto1983/aura/internal/sandboxagent"
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
	MCPServers     map[string]mcp.ServerConfig
	MCPPolicies    map[string]mcp.ManagedServer
	MCPServersErr  error
	SandboxAgent   sandboxagent.Config
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

	// Sandbox execution is provided by a local sandbox-agent container on loopback.
	// Aura does not download sandbox binaries at boot and does not call an online
	// sandbox service.

	// Phase 7 (Slice 5) web_search/web_fetch knobs. SearxngURL is the upstream-
	// canonical name (NO AURA_ prefix); an empty value is NOT boot-fatal — it is
	// surfaced as web_search_unavailable{searxng_not_configured} at call time
	// (D-05/D-06) so `aura db migrate` and every non-web subcommand keep working.
	// No allowlist/loopback escape hatch lands here (D-30).
	SearxngURL           string // SEARXNG_URL — local SearXNG /search endpoint; empty default, fail-closed at call time
	WebDNSPinTTLSec      int    // AURA_WEB_DNS_PIN_TTL_SEC — per-conversation DNS pin TTL (D-25)
	WebFetchMaxBodyBytes int    // AURA_WEB_FETCH_MAX_BODY_BYTES — raw HTTP response body download ceiling (DoS guard); the LLM-facing markdown preview/spillover is governed by the agent tool-result preview cap (tools.NewResult), NOT this field
	WebCachePersistent   bool   // AURA_WEB_CACHE_PERSISTENT — opt-in disk cache; default false = in-memory (D-32)
	WebSearchTimeoutSec  int    // AURA_WEB_SEARCH_TIMEOUT_SEC — search wall-clock deadline (D-14)
	WebFetchTimeoutSec   int    // AURA_WEB_FETCH_TIMEOUT_SEC — fetch wall-clock deadline (D-23)
	WebUserAgent         string // AURA_WEB_USER_AGENT — Aura-specific UA, no browser spoof (D-34/D-35)

	// Phase 9 (Slice 3) swarm knobs (D-11/D-12/D-13). All non-fatal
	// envIntDefault fallbacks — a typo falls back rather than booting fatal.
	MaxSwarmGoals        int // AURA_SWARM_MAX_GOALS — hard cap on goals per swarm_spawn call (D-13)
	SwarmChildTimeoutSec int // AURA_SWARM_CHILD_TIMEOUT_SEC — per-child wall-clock deadline (D-11)
	MaxSwarmConcurrent   int // AURA_SWARM_MAX_CONCURRENT — wave width; goals beyond this run in sequential waves (D-12)

	// Scheduler agent_job wall-clock (#53/D-42). The 120s swarm-child analog starved
	// real artifact jobs (a North-Star-class xlsx run measures 150-360s live); the
	// serve composition root passes this into AgentDeps.MaxDuration.
	AgentJobMaxDurationSec int // AURA_AGENT_JOB_MAX_DURATION_SEC — agent_job end-to-end wall-clock deadline

	// Phase 11 (Slice 7) skills knobs (D-34). SkillsDir is the active skill root the
	// loader scans + builtins materialize into; ExportDir is the ro `/skills` mount
	// source. Cap knobs bound the per-skill body and the model-visible manifest.
	SkillsDir             string // AURA_SKILLS_DIR — active skill root (loader scan + builtin materialization)
	SkillBodyCapBytes     int    // AURA_SKILL_BODY_CAP_BYTES — per-skill body size cap at load (DoS guard, D-34)
	SkillManifestCapBytes int    // AURA_SKILL_MANIFEST_CAP_BYTES — manifest-in-Description byte budget; overflow → BM25 list (D-09/D-34)
	SkillExportDir        string // AURA_SKILL_EXPORT_DIR — activation→host export dir (the ro /skills mount source, D-17)
	SkillSnippetTTLDays   int    // AURA_SKILL_SNIPPET_TTL_DAYS — TTL sweep archives snippets unused this long (D-16/D-34)

	// Write-boundary injection blocklist (D-27/D-34). The NFKC-normalize-then-
	// match literal sequence list the skills validator enforces at write time
	// (model-authored create/update/install), NEVER on load (D-28). Defaults to
	// the prd.md §Slice 7 builtin list; a comma-separated AURA_SKILL_INJECTION_BLOCKLIST
	// replaces it wholesale.
	SkillInjectionBlocklist []string // AURA_SKILL_INJECTION_BLOCKLIST — prompt-injection literal blocklist (D-27/D-34)

	// Phase 12 (Slice 8) AG-UI gateway knobs. AGUIBind is hardcoded loopback this
	// phase (auth deferred; the loopback bind IS the compensating control, Pitfall 6
	// / amendment #35 — no --bind flag, no non-loopback escape). AGUICORSPermissive
	// gates the `Access-Control-Allow-Origin: *` header (default restrictive, dev-only).
	// AGUIBufferCap caps the per-connection SSE pump channel (drop-on-full, never
	// blocks the Loop). All non-fatal envDefault fallbacks.
	AGUIBind           string // AURA_AGUI_BIND — loopback-only HTTP bind (Pitfall 6)
	AGUICORSPermissive bool   // AURA_AGUI_CORS_PERMISSIVE — dev-only permissive CORS (default restrictive)
	AGUIBufferCap      int    // AURA_AGUI_BUFFER_CAP — SSE/fanout subscriber buffer cap (default 64)

	// Phase 13 (Slice 9) channels + setup-wizard + multimodal knobs. Aura-native
	// knobs use the AURA_<DOMAIN>_<UNIT> convention; third-party sidecars keep
	// upstream naming (MULTIMODAL_*/STT_*/TTS_*) per the CLAUDE.md exception. All
	// non-fatal silent-fallback loads — a typo falls back, never boots fatal.
	//
	// SetupBind is a loopback default on a port DISTINCT from the AG-UI :9080
	// (T-13-04-SetupExposure: the loopback bind IS the compensating control; the
	// override exists only for a deliberate remote-QR scan, token gate is plan
	// 13-07). SetupToken empty default → a random UUIDv4 is generated and printed
	// to stdout on first boot (plan 13-07), never read from disk here.
	SetupBind  string // AURA_SETUP_BIND — loopback setup-wizard HTTP bind, distinct from :9080
	SetupToken string // AURA_SETUP_TOKEN — setup-API gate; empty → generated at boot (13-07)

	// VisionCloud routes image understanding: false (default) → local aura-ocr-vl
	// sidecar; true → OpenRouter/minimax-m3 cloud (no GPU). One env branch, zero
	// code dup (#60 / Pitfall 6).
	VisionCloud bool // AURA_VISION_CLOUD — false=local GLM-OCR sidecar, true=cloud vision

	// Multimodal sidecar URLs/models (upstream naming, CLAUDE.md third-party
	// exception). MultimodalFallbackModel is the cloud vision model used when
	// VisionCloud is true and the primary model lacks SupportsVision.
	MultimodalBaseURL       string // MULTIMODAL_BASE_URL — aura-ocr-vl OpenAI-compat base
	MultimodalModel         string // MULTIMODAL_MODEL — local vision model id
	MultimodalFallbackModel string // MULTIMODAL_FALLBACK_MODEL — cloud vision fallback (default minimax/minimax-m3)
	STTBaseURL              string // STT_BASE_URL — aura-stt OpenAI-compat base
	STTModel                string // STT_MODEL — speech-to-text model id
	STTLanguage             string // STT_LANGUAGE — transcription language hint (default "it"; "" = whisper auto-detect, unreliable on short clips — spike-027)
	TTSBaseURL              string // TTS_BASE_URL — aura-tts OpenAI-compat base
	TTSVoice                string // TTS_VOICE — Kokoro voice id (default if_sara)
	TTSFormat               string // TTS_FORMAT — voice-note audio format (default opus)
	DocumentsBaseURL        string // DOCUMENTS_BASE_URL — markitdown /convert base (UX-04 documents leg)
	MultimodalTimeoutSec    int    // MULTIMODAL_TIMEOUT_SEC — per-request sidecar ceiling (default 120s; CPU OCR on a downscaled photo is well under, but vision needs more headroom than STT/TTS)
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
	mcpServers, mcpPolicies, mcpServersErr := loadMCPServers()

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
		MCPServers:    mcpServers,
		MCPPolicies:   mcpPolicies,
		MCPServersErr: mcpServersErr,
		SandboxAgent: sandboxagent.Config{
			BaseURL:    envDefault("AURA_SANDBOX_AGENT_URL", sandboxagent.DefaultBaseURL),
			TimeoutSec: envIntDefault("AURA_SANDBOX_AGENT_TIMEOUT_SEC", sandboxagent.DefaultTimeoutSec),
		},
		RunDir:         envDefault("AURA_RUN_DIR", defaultRunDir()),
		ToolPreviewCap: envIntDefault("AURA_CONTEXT_PREVIEW_CAP_BYTES", 2048),
		OtelExporter:   envDefault("AURA_OTEL_EXPORTER", defaultOtelExporter),
		OtelEndpoint:   envDefault("AURA_OTEL_ENDPOINT", defaultOtelEndpoint),

		ConversationTurnCapBytes:   envIntDefault("AURA_CONVERSATION_TURN_CAP_BYTES", 65536),
		ContextToolEvictAfterTurns: envIntDefault("AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS", 10),
		HistoryHardCapTurns:        envIntDefault("AURA_HISTORY_HARD_CAP_TURNS", 50),
		RunDirWarnThresholdBytes:   envIntDefault("AURA_RUN_DIR_WARN_THRESHOLD_BYTES", 1073741824),

		// Phase 7 web knobs. SEARXNG_URL has an empty default on purpose (D-05):
		// missing is fail-closed at call time, never a boot error.
		SearxngURL:           os.Getenv("SEARXNG_URL"),
		WebDNSPinTTLSec:      envIntDefault("AURA_WEB_DNS_PIN_TTL_SEC", 60),
		WebFetchMaxBodyBytes: envIntDefault("AURA_WEB_FETCH_MAX_BODY_BYTES", 5_000_000),
		WebCachePersistent:   envBoolDefault("AURA_WEB_CACHE_PERSISTENT", false),
		WebSearchTimeoutSec:  envIntDefault("AURA_WEB_SEARCH_TIMEOUT_SEC", 20),
		WebFetchTimeoutSec:   envIntDefault("AURA_WEB_FETCH_TIMEOUT_SEC", 30),
		WebUserAgent:         envDefault("AURA_WEB_USER_AGENT", "Aura/0.x web_fetch"),

		MaxSwarmGoals:        envIntDefault("AURA_SWARM_MAX_GOALS", 8),
		SwarmChildTimeoutSec: envIntDefault("AURA_SWARM_CHILD_TIMEOUT_SEC", 120),
		MaxSwarmConcurrent:   envIntDefault("AURA_SWARM_MAX_CONCURRENT", 4),

		AgentJobMaxDurationSec: envIntDefault("AURA_AGENT_JOB_MAX_DURATION_SEC", 600),

		// Phase 11 skills knobs (D-34). Defaults derive from the per-user ~/.aura tree.
		SkillsDir:             envDefault("AURA_SKILLS_DIR", defaultSkillsDir()),
		SkillBodyCapBytes:     envIntDefault("AURA_SKILL_BODY_CAP_BYTES", 32768),
		SkillManifestCapBytes: envIntDefault("AURA_SKILL_MANIFEST_CAP_BYTES", 8192),
		SkillExportDir:        envDefault("AURA_SKILL_EXPORT_DIR", defaultSkillExportDir()),
		SkillSnippetTTLDays:   envIntDefault("AURA_SKILL_SNIPPET_TTL_DAYS", 90),

		SkillInjectionBlocklist: envSliceDefault("AURA_SKILL_INJECTION_BLOCKLIST", defaultSkillInjectionBlocklist()),

		// Phase 12 AG-UI gateway. Loopback default is the compensating control for the
		// auth-deferred posture (no --bind flag this phase, amendment #35).
		AGUIBind:           envDefault("AURA_AGUI_BIND", "127.0.0.1:9080"),
		AGUICORSPermissive: envBoolDefault("AURA_AGUI_CORS_PERMISSIVE", false),
		AGUIBufferCap:      envIntDefault("AURA_AGUI_BUFFER_CAP", 64),

		// Phase 13 channels + setup + multimodal. Setup bind defaults to :9081 —
		// a loopback port DISTINCT from the AG-UI :9080 (separate-port requirement).
		SetupBind:   envDefault("AURA_SETUP_BIND", "127.0.0.1:9081"),
		SetupToken:  os.Getenv("AURA_SETUP_TOKEN"),
		VisionCloud: envBoolDefault("AURA_VISION_CLOUD", false),

		MultimodalBaseURL:       os.Getenv("MULTIMODAL_BASE_URL"),
		MultimodalModel:         os.Getenv("MULTIMODAL_MODEL"),
		MultimodalFallbackModel: envDefault("MULTIMODAL_FALLBACK_MODEL", "minimax/minimax-m3"),
		STTBaseURL:              os.Getenv("STT_BASE_URL"),
		STTModel:                os.Getenv("STT_MODEL"),
		STTLanguage:             envDefault("STT_LANGUAGE", "it"),
		TTSBaseURL:              os.Getenv("TTS_BASE_URL"),
		TTSVoice:                envDefault("TTS_VOICE", "if_sara"),
		TTSFormat:               envDefault("TTS_FORMAT", "opus"),
		DocumentsBaseURL:        os.Getenv("DOCUMENTS_BASE_URL"),
		MultimodalTimeoutSec:    envIntDefault("MULTIMODAL_TIMEOUT_SEC", 120),
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

func loadMCPServers() (map[string]mcp.ServerConfig, map[string]mcp.ManagedServer, error) {
	path, err := mcp.ManagedConfigPath()
	if err != nil {
		return nil, nil, err
	}
	managed, err := mcp.LoadManagedConfig(path)
	if err != nil {
		return nil, nil, err
	}
	managedServers, err := mcpmanager.RuntimeServers(managed)
	if err != nil {
		return nil, nil, err
	}
	runnableManaged, err := mcpmanager.RunnableManagedServers(managed)
	if err != nil {
		return nil, nil, err
	}
	policies := make(map[string]mcp.ManagedServer, len(runnableManaged))
	for name, server := range runnableManaged {
		policies[name] = server
	}
	envServers, err := parseMCPServersJSON(os.Getenv("AURA_MCP_SERVERS_JSON"))
	if err != nil {
		return nil, nil, err
	}
	out := make(map[string]mcp.ServerConfig, len(managedServers)+len(envServers))
	for name, cfg := range managedServers {
		out[name] = cfg
	}
	for name, cfg := range envServers {
		out[name] = cfg
		delete(policies, name)
	}
	if len(out) == 0 && len(policies) == 0 {
		return nil, nil, nil
	}
	return out, policies, nil
}

func parseMCPServersJSON(raw string) (map[string]mcp.ServerConfig, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var wrapped struct {
		MCPServers map[string]mcp.ServerConfig `json:"mcpServers"`
	}
	if err := json.Unmarshal([]byte(raw), &wrapped); err != nil {
		return nil, fmt.Errorf("AURA_MCP_SERVERS_JSON: %w", err)
	}
	if wrapped.MCPServers != nil {
		return validateMCPServers(wrapped.MCPServers)
	}
	var direct map[string]mcp.ServerConfig
	if err := json.Unmarshal([]byte(raw), &direct); err != nil {
		return nil, fmt.Errorf("AURA_MCP_SERVERS_JSON: %w", err)
	}
	return validateMCPServers(direct)
}

func validateMCPServers(in map[string]mcp.ServerConfig) (map[string]mcp.ServerConfig, error) {
	if len(in) == 0 {
		return nil, nil
	}
	out := make(map[string]mcp.ServerConfig, len(in))
	for name, cfg := range in {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("AURA_MCP_SERVERS_JSON: server name cannot be empty")
		}
		if strings.TrimSpace(cfg.Command) == "" {
			return nil, fmt.Errorf("AURA_MCP_SERVERS_JSON: server %q command cannot be empty", name)
		}
		cfg.Command = strings.TrimSpace(cfg.Command)
		out[name] = cfg
	}
	return out, nil
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

// envBoolDefault returns the boolean value of `key`, falling back to `fallback`
// when the variable is unset, empty, or fails to parse. Like envIntDefault, a
// malformed value is silently absorbed to the fallback rather than booting fatal
// — a typo in an opt-in toggle should not block startup.
func envBoolDefault(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return b
}

// envSliceDefault returns the comma-separated value of `key` split into a
// trimmed, empty-dropped slice, falling back to `fallback` when the variable is
// unset or empty. A set-but-all-empty value (e.g. ",,") yields an empty slice —
// an operator can deliberately clear the blocklist that way.
func envSliceDefault(key string, fallback []string) []string {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// defaultSkillInjectionBlocklist is the prd.md §Slice 7 builtin prompt-injection
// blocklist (D-27/D-34): chat-template control tokens across the model families
// Aura speaks. The validator NFKC-normalizes a skill body THEN literal-matches
// each entry (write-boundary only, D-28).
func defaultSkillInjectionBlocklist() []string {
	return []string{
		// OpenAI ChatML
		"<|im_start|>", "<|im_end|>", "<|endoftext|>",
		// Anthropic
		"</system>", "</human>", "</assistant>", "\n\nHuman:", "\n\nAssistant:",
		// Llama / Mistral
		"[INST]", "[/INST]", "<<SYS>>", "<</SYS>>",
		// Meta / Llama 3
		"<|begin_of_text|>", "<|start_header_id|>", "<|end_header_id|>", "<|eot_id|>",
		// DeepSeek / Gemma / Qwen
		"<|fim_begin|>", "<|fim_hole|>", "<start_of_turn>", "<end_of_turn>",
	}
}

// defaultRunDir returns a sensible per-user run directory for sidecar tool
// outputs. Falls back to a tmp-based path if user cache is unavailable.
func defaultRunDir() string {
	if cache, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cache, "aura")
	}
	return filepath.Join(os.TempDir(), "aura")
}

// auraHomeDir returns the per-user ~/.aura base the skills tree lives under,
// falling back to a tmp-based path when the home dir is unavailable.
func auraHomeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".aura")
	}
	return filepath.Join(os.TempDir(), "aura")
}

// defaultSkillsDir is the active skill root (AURA_SKILLS_DIR default): ~/.aura/skills.
func defaultSkillsDir() string { return filepath.Join(auraHomeDir(), "skills") }

// defaultSkillExportDir is the activation export dir (AURA_SKILL_EXPORT_DIR
// default): ~/.aura/skills/export — the ro /skills mount source (D-17).
func defaultSkillExportDir() string { return filepath.Join(defaultSkillsDir(), "export") }
