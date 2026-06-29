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
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/mcp"
	"github.com/chetto1983/aura/internal/profile"
	"github.com/joho/godotenv"
)

// OTel exporter knob defaults (D-05/D-06). The default OTLP target silent-drops
// without a collector; "none" is a true no-op provider.
const (
	defaultOtelExporter = "otlp"
	defaultOtelEndpoint = "localhost:4317"
)

const (
	defaultObjectStoreAccessKey = "GK000000000000000000000000"
	defaultObjectStoreSecretKey = "0000000000000000000000000000000000000000000000000000000000000000"
)

// Config is the root composite. Subsystem configs live in their packages.
type Config struct {
	DB             db.Config
	Neo4j          knowledge.Config // Slice 0.7 — graph + vector + embed sidecar wiring
	LLM            llm.Config       // Slice 1 — OpenAI-compat client + load-order chain (D-22)
	MCPServers     map[string]mcp.ServerConfig
	MCPPolicies    map[string]mcp.ManagedServer
	MCPServersErr  error
	RunDir         string // absolute — a relative AURA_RUN_DIR is normalized to absolute at load (F-041) so sidecars are not cwd-dependent
	RunDirErr      error  // non-nil only if filepath.Abs failed (cwd unobtainable); surfaced by Validate so boot fails loudly
	ToolPreviewCap int
	OtelExporter   string // AURA_OTEL_EXPORTER ∈ {stdout,otlp,none} (D-06)
	OtelEndpoint   string // AURA_OTEL_ENDPOINT — OTLP/gRPC target (D-06)

	// Phase 4 (Slice 1.8) conversation + context-management tuning knobs.
	// Non-fatal envIntDefault fallbacks (an ad-hoc tweak typo falls back, not boots-fatal).
	ConversationTurnCapBytes   int // AURA_CONVERSATION_TURN_CAP_BYTES — content > this spills to a sidecar file
	ContextToolEvictAfterTurns int // AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS — L1 microcompact eviction age
	HistoryHardCapTurns        int // AURA_HISTORY_HARD_CAP_TURNS — L2.5 picobot hard rolling buffer cap
	RunDirWarnThresholdBytes   int // AURA_RUN_DIR_WARN_THRESHOLD_BYTES — boot du WARN threshold (audit-only)
	RunDirSweepIntervalSec     int // AURA_RUN_DIR_SWEEP_INTERVAL_SEC — periodic sidecar-sweep cadence in `serve` (M-06); <=0 disables the worker (boot sweep still runs)

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

	// SchedulerPreferOriginChannel is the default-on kill-switch (Phase 20 R5/D-03):
	// when on, a scheduled notification with an owning identity prefers the origin
	// channel (a Telegram reminder lands back in that DM) over the per-task route. The
	// composition root injects it as cron.DispatchDeps.PreferOriginChannel; false →
	// byte-identical legacy route-only behavior. Unset/malformed → on.
	SchedulerPreferOriginChannel bool // AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL — prefer the origin channel over the route (default true)

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

	// Phase 12 (Slice 8) AG-UI gateway knobs. AGUIBind is the cockpit HTTP bind: it
	// defaults to loopback but may be ANY address — WEB-02/D-06 lifted the historical
	// hardcoded-loopback restriction (amendment #35) and replaced it with the boot-time
	// GuardWebBind policy (loopback always boots; a non-loopback bind requires a web-auth
	// credential). AGUICORSPermissive gates the `Access-Control-Allow-Origin: *` header
	// (default restrictive, dev-only). AGUIBufferCap caps the per-connection SSE pump
	// channel (drop-on-full, never blocks the Loop). All non-fatal envDefault fallbacks.
	AGUIBind           string // AURA_AGUI_BIND — cockpit HTTP bind (any address; non-loopback gated by GuardWebBind)
	AGUICORSPermissive bool   // AURA_AGUI_CORS_PERMISSIVE — dev-only permissive CORS (default restrictive)
	AGUIBufferCap      int    // AURA_AGUI_BUFFER_CAP — SSE/fanout subscriber buffer cap (default 64)

	// Industrial asset object-store foundation. The backend is selected by the
	// later asset service; config is intentionally non-fatal so DB/migration paths
	// do not depend on Garage being reachable.
	ObjectStoreBackend        string // AURA_OBJECTSTORE_BACKEND — garage|filesystem-dev|fake
	ObjectStoreEndpoint       string // AURA_OBJECTSTORE_ENDPOINT — S3-compatible internal endpoint
	ObjectStorePublicEndpoint string // AURA_OBJECTSTORE_PUBLIC_ENDPOINT — optional presign URL host rewrite
	ObjectStoreRegion         string // AURA_OBJECTSTORE_REGION — Garage/S3 region
	ObjectStoreBucket         string // AURA_OBJECTSTORE_BUCKET — asset bucket
	ObjectStoreAccessKey      string // AURA_OBJECTSTORE_ACCESS_KEY — S3 access key
	ObjectStoreSecretKey      string // AURA_OBJECTSTORE_SECRET_KEY — S3 secret key
	ObjectStorePathStyle      bool   // AURA_OBJECTSTORE_PATH_STYLE — Garage requires path-style by default
	AssetMaxDocumentBytes     int    // AURA_ASSET_MAX_DOCUMENT_BYTES — document upload ceiling
	AssetMaxImageBytes        int    // AURA_ASSET_MAX_IMAGE_BYTES — image upload ceiling
	AssetMaxAudioBytes        int    // AURA_ASSET_MAX_AUDIO_BYTES — audio upload ceiling
	AssetPresignTTLSec        int    // AURA_ASSET_PRESIGN_TTL_SEC — upload URL lifetime
	AssetProcessingConcurrent int    // AURA_ASSET_PROCESSING_CONCURRENCY — future asset worker width
	TelegramAPIBaseURL        string // TELEGRAM_API_BASE_URL — optional local Bot API base
	TelegramFileBaseURL       string // TELEGRAM_FILE_BASE_URL — optional local Bot API file base
	TelegramLocalBotAPI       bool   // AURA_TELEGRAM_LOCAL_BOT_API — local Bot API toggle

	// Web-auth knobs. GuardWebBind decides at boot whether a non-loopback AGUIBind
	// may start. Authula is the active provider; WebAuthSecret is retained only so
	// old env files/tests can be loaded without making it a product path.
	WebAuthSecret string // AURA_WEB_AUTH_SECRET - deprecated legacy passphrase secret; not mounted by aura serve
	WebTrustProxy bool   // AURA_WEB_TRUST_PROXY — operator vouches a reverse proxy terminates auth (D-05)

	// Authula web-auth provider knobs (docs/cockpit-overhaul/05-authula-auth-SPEC.md).
	// WebAuthProvider defaults to authula. Legacy values are accepted for backward
	// compatibility, but aura serve still builds the Authula-only auth boundary.
	WebAuthProvider         string // AURA_WEB_AUTH_PROVIDER (default authula)
	AuthulaDatabaseURL      string // AURA_AUTHULA_DATABASE_URL — Postgres DSN for the authula schema; empty default → derived from AURA_DB_URL with ?search_path=authula
	AuthulaSecret           string // AURA_AUTHULA_SECRET — 32-byte hex secret Authula derives its HMAC/token keys from (required when provider=authula)
	AuthulaOperatorIdentity string // AURA_AUTHULA_OPERATOR_IDENTITY — optional legacy Aura identity fallback for Authula operator linking
	AuthulaRateLimitMax     int    // AURA_AUTHULA_RATE_LIMIT_MAX — credential attempts per minute before Authula throttles (default 30)

	// ServeShutdownGraceSec bounds the in-flight turn drain on a SIGTERM/SIGINT
	// (audit O-06 / AP-17): on the signal the daemon stops accepting new work, then
	// gives in-flight turns up to this window to reach a terminal frame before the
	// work ctx is hard-cancelled as the final backstop. Default 25s — a real turn
	// rarely exceeds it; a misconfigured non-positive value degrades to immediate
	// hard-cancel (no wedge).
	ServeShutdownGraceSec int // AURA_SERVE_SHUTDOWN_GRACE_SEC — bounded in-flight turn drain window on shutdown (default 25)

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
	STTModel                string // STT_MODEL — LOCAL faster-whisper model id (NOT the cloud switch)
	STTCloudModel           string // AURA_STT_CLOUD_MODEL — set to a cloud STT model (e.g. openai/whisper-large-v3) to swap STT to OpenRouter; empty = local faster-whisper sidecar
	STTLanguage             string // STT_LANGUAGE — transcription language hint (default "it"; "" = whisper auto-detect, unreliable on short clips — spike-027)
	TTSBaseURL              string // TTS_BASE_URL — aura-tts OpenAI-compat base
	TTSModel                string // AURA_TTS_MODEL — set to a cloud TTS model (e.g. hexgrad/kokoro-82m) to swap TTS to OpenRouter; empty = local Kokoro sidecar
	TTSVoice                string // TTS_VOICE — Kokoro voice id (default if_sara)
	TTSFormat               string // TTS_FORMAT — voice-note audio format (default opus)
	DocumentsBaseURL        string // DOCUMENTS_BASE_URL — markitdown /convert base (UX-04 documents leg)
	MultimodalTimeoutSec    int    // MULTIMODAL_TIMEOUT_SEC — per-request sidecar ceiling (default 120s; CPU OCR on a downscaled photo is well under, but vision needs more headroom than STT/TTS)

	// Phase 14 (Slice 10) Agent.md profile knobs.
	ProfileDir        string // AURA_PROFILE_DIR — per-identity Agent.md root, default ~/.aura/agents
	ProfileCertaintyN int    // AURA_PROFILE_CERTAINTY_N — observation threshold for auto-add, default 3

	// Cockpit "Connect" device-linking knob. WhatsAppBridgeURL is the aura-whatsapp bridge
	// management REST base URL the /api/connect/whatsapp/* proxy forwards to; the in-compose
	// default points at the sibling sidecar. An unset/empty value is NOT boot-fatal — the
	// connect routes answer 503 at call time so a stack without the sidecar boots fine.
	WhatsAppBridgeURL string // AURA_WHATSAPP_BRIDGE_URL — aura-whatsapp bridge mgmt REST base, default http://whatsapp:8081

	// Cockpit "Connect Google Calendar" knobs. CalendarMCPURL is the aura-pim-mcp sidecar's
	// base URL the /api/connect/pim/* admin-proxy forwards to; CalendarMCPAdminToken is the
	// /admin Bearer token injected server-side (never returned to the client). An unset URL is
	// NOT boot-fatal — the connect routes answer 503 at call time so a stack without the sidecar
	// boots fine.
	CalendarMCPURL        string // AURA_PIM_MCP_URL — aura-pim-mcp /admin REST base, default http://aura-pim-mcp:8080
	CalendarMCPAdminToken string // AURA_PIM_MCP_ADMIN_TOKEN — /admin Bearer token, default changeme-aura-pim-local

	// Phase 30 (RET-01) retrieval rerank knobs. RerankBaseURL is the optional
	// aura-rerank sidecar (/v1/rerank) base; an unset/empty value is NOT
	// boot-fatal — the rerank client fails soft to the RRF/vector order, so a
	// GPU-absent deployment runs with rerank off (spike 070). Convention
	// AURA_<DOMAIN>_<UNIT>. The local↔cloud swap is ONE knob: set AURA_RERANK_MODEL
	// to a cloud model (e.g. cohere/rerank-4-fast) and rerank routes to the shared
	// OpenRouter endpoint authenticated with the SINGLE OPENROUTER_API_KEY every
	// cloud backend uses — no per-backend key (see RerankRoute, D-28 vision parity).
	RerankBaseURL string // AURA_RERANK_BASE_URL — local rerank sidecar base, default http://127.0.0.1:8085
	RerankModel   string // AURA_RERANK_MODEL — set to a cloud model (cohere/rerank-4-fast) to swap to OpenRouter; empty = local sidecar
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

// LoadServe reads the full daemon config but permits an empty LLM API key so the
// setup wizard and channels can boot. LLM turns still fail closed at call time.
func LoadServe() (*Config, error) {
	cfg := loadBase()
	llmCfg, err := llm.LoadAllowEmptyKey()
	if err != nil {
		return nil, fmt.Errorf("config: load llm: %w", err)
	}
	cfg.LLM = *llmCfg
	return cfg, nil
}

// Validate fails fast on an empty REQUIRED infrastructure secret so a misconfigured
// deploy errors at boot with a named cause instead of a late, cryptic DB auth
// failure or a silently degraded graph (O-04). The LLM API key has its own
// fail-fast in llm.Load (D-22); this covers the composed DB DSN and the Neo4j
// password. The daemon/REPL boot wires it in; the DB-only commands (LoadDB) skip it.
func (c *Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.DB.URL) == "" {
		missing = append(missing, "POSTGRES_PASSWORD (or AURA_DB_URL)")
	}
	if strings.TrimSpace(c.Neo4j.Password) == "" {
		missing = append(missing, "NEO4J_PASSWORD")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: required secret(s) unset: %s", strings.Join(missing, ", "))
	}
	if c.RunDirErr != nil {
		return fmt.Errorf("config: %w", c.RunDirErr)
	}
	return nil
}

// GuardWebBind is the WEB-02 fail-fast boot policy for the cockpit listener (D-05).
// A loopback bind always boots with no credential, exactly as before; a non-loopback
// bind boots ONLY when EITHER Authula web auth is configured OR trustProxy is true
// (the operator vouches a reverse proxy terminates auth).
// A non-loopback bind with neither credential returns an actionable error so the daemon
// refuses to silently expose an unauthenticated surface. It is a pure function — total
// (no panic path) and table-test-friendly — mirroring Validate's "config: …" posture.
//
// Wildcards (0.0.0.0, ::, [::]) are NOT special-cased: net.ParseIP(...).IsLoopback()
// returns false for them, so they fall through to the gated branch, which is correct.
func GuardWebBind(bind string, authConfigured bool, trustProxy bool) error {
	host, _, err := net.SplitHostPort(bind)
	if err != nil {
		host = bind // tolerate a bare host with no port
	}
	ip := net.ParseIP(host)
	isLoopback := host == "localhost" || (ip != nil && ip.IsLoopback())
	if isLoopback {
		return nil // loopback always bootable, exactly as before (D-05)
	}
	if authConfigured || trustProxy {
		return nil // unlocked by either credential (D-05)
	}
	return fmt.Errorf("config: AURA_AGUI_BIND=%q is non-loopback but web auth is not configured; "+
		"set AURA_AUTHULA_SECRET with AURA_AUTHULA_DATABASE_URL or AURA_DB_URL, set "+
		"AURA_WEB_TRUST_PROXY=true (a reverse proxy terminates auth), or bind a loopback address", bind)
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

	runDir, runDirErr := absRunDir(envDefault("AURA_RUN_DIR", defaultRunDir()))

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
			EmbedDimensions:   envIntDefault("AURA_EMBED_DIMENSIONS", knowledge.DefaultEmbedDimensions),
			EmbedModel:        os.Getenv("AURA_EMBED_MODEL"),
		},
		MCPServers:     mcpServers,
		MCPPolicies:    mcpPolicies,
		MCPServersErr:  mcpServersErr,
		RunDir:         runDir,
		RunDirErr:      runDirErr,
		ToolPreviewCap: envIntDefault("AURA_CONTEXT_PREVIEW_CAP_BYTES", 2048),
		OtelExporter:   envDefault("AURA_OTEL_EXPORTER", defaultOtelExporter),
		OtelEndpoint:   envDefault("AURA_OTEL_ENDPOINT", defaultOtelEndpoint),

		ConversationTurnCapBytes:   envIntDefault("AURA_CONVERSATION_TURN_CAP_BYTES", 65536),
		ContextToolEvictAfterTurns: envIntDefault("AURA_CONTEXT_TOOL_EVICT_AFTER_TURNS", 10),
		HistoryHardCapTurns:        envIntDefault("AURA_HISTORY_HARD_CAP_TURNS", 50),
		RunDirWarnThresholdBytes:   envIntDefault("AURA_RUN_DIR_WARN_THRESHOLD_BYTES", 1073741824),
		RunDirSweepIntervalSec:     envIntDefault("AURA_RUN_DIR_SWEEP_INTERVAL_SEC", 3600),

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

		SchedulerPreferOriginChannel: envBoolDefault("AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL", true),

		// Phase 11 skills knobs (D-34). Defaults derive from the per-user ~/.aura tree.
		SkillsDir:             envDefault("AURA_SKILLS_DIR", defaultSkillsDir()),
		SkillBodyCapBytes:     envIntDefault("AURA_SKILL_BODY_CAP_BYTES", 32768),
		SkillManifestCapBytes: envIntDefault("AURA_SKILL_MANIFEST_CAP_BYTES", 8192),
		SkillExportDir:        envDefault("AURA_SKILL_EXPORT_DIR", defaultSkillExportDir()),
		SkillSnippetTTLDays:   envIntDefault("AURA_SKILL_SNIPPET_TTL_DAYS", 90),

		SkillInjectionBlocklist: envSliceDefault("AURA_SKILL_INJECTION_BLOCKLIST", defaultSkillInjectionBlocklist()),

		// Phase 12 AG-UI gateway. The loopback default still boots with no web-auth
		// config; WEB-02/D-06 lifted the hardcoded-loopback restriction so AURA_AGUI_BIND
		// may now be any address, with GuardWebBind enforcing the non-loopback credential
		// policy at boot (D-05).
		AGUIBind:           envDefault("AURA_AGUI_BIND", "127.0.0.1:9080"),
		AGUICORSPermissive: envBoolDefault("AURA_AGUI_CORS_PERMISSIVE", false),
		AGUIBufferCap:      envIntDefault("AURA_AGUI_BUFFER_CAP", 64),

		ObjectStoreBackend:        envDefault("AURA_OBJECTSTORE_BACKEND", "garage"),
		ObjectStoreEndpoint:       envDefault("AURA_OBJECTSTORE_ENDPOINT", "http://127.0.0.1:3900"),
		ObjectStorePublicEndpoint: os.Getenv("AURA_OBJECTSTORE_PUBLIC_ENDPOINT"),
		ObjectStoreRegion:         envDefault("AURA_OBJECTSTORE_REGION", "garage"),
		ObjectStoreBucket:         envDefault("AURA_OBJECTSTORE_BUCKET", "aura-assets"),
		ObjectStoreAccessKey:      envDefault("AURA_OBJECTSTORE_ACCESS_KEY", defaultObjectStoreAccessKey),
		ObjectStoreSecretKey:      envDefault("AURA_OBJECTSTORE_SECRET_KEY", defaultObjectStoreSecretKey),
		ObjectStorePathStyle:      envBoolDefault("AURA_OBJECTSTORE_PATH_STYLE", true),
		AssetMaxDocumentBytes:     envIntDefault("AURA_ASSET_MAX_DOCUMENT_BYTES", 104857600),
		AssetMaxImageBytes:        envIntDefault("AURA_ASSET_MAX_IMAGE_BYTES", 26214400),
		AssetMaxAudioBytes:        envIntDefault("AURA_ASSET_MAX_AUDIO_BYTES", 104857600),
		AssetPresignTTLSec:        envIntDefault("AURA_ASSET_PRESIGN_TTL_SEC", 600),
		AssetProcessingConcurrent: envIntDefault("AURA_ASSET_PROCESSING_CONCURRENCY", 2),
		TelegramAPIBaseURL:        os.Getenv("TELEGRAM_API_BASE_URL"),
		TelegramFileBaseURL:       os.Getenv("TELEGRAM_FILE_BASE_URL"),
		TelegramLocalBotAPI:       envBoolDefault("AURA_TELEGRAM_LOCAL_BOT_API", false),

		// Web-auth knobs. The legacy secret is read raw for compatibility only; the
		// active cockpit login path is Authula.
		WebAuthSecret: os.Getenv("AURA_WEB_AUTH_SECRET"),
		WebTrustProxy: envBoolDefault("AURA_WEB_TRUST_PROXY", false),

		// Authula provider (default authula).
		WebAuthProvider:         envDefault("AURA_WEB_AUTH_PROVIDER", "authula"),
		AuthulaDatabaseURL:      os.Getenv("AURA_AUTHULA_DATABASE_URL"),
		AuthulaSecret:           os.Getenv("AURA_AUTHULA_SECRET"),
		AuthulaOperatorIdentity: os.Getenv("AURA_AUTHULA_OPERATOR_IDENTITY"),
		AuthulaRateLimitMax:     envIntDefault("AURA_AUTHULA_RATE_LIMIT_MAX", 30),

		ServeShutdownGraceSec: envIntDefault("AURA_SERVE_SHUTDOWN_GRACE_SEC", 25),

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
		STTCloudModel:           os.Getenv("AURA_STT_CLOUD_MODEL"),
		STTLanguage:             envDefault("STT_LANGUAGE", "it"),
		TTSBaseURL:              os.Getenv("TTS_BASE_URL"),
		TTSModel:                os.Getenv("AURA_TTS_MODEL"),
		TTSVoice:                envDefault("TTS_VOICE", "if_sara"),
		TTSFormat:               envDefault("TTS_FORMAT", "opus"),
		DocumentsBaseURL:        os.Getenv("DOCUMENTS_BASE_URL"),
		MultimodalTimeoutSec:    envIntDefault("MULTIMODAL_TIMEOUT_SEC", 120),

		ProfileDir:        envDefault("AURA_PROFILE_DIR", profile.DefaultRoot()),
		ProfileCertaintyN: envIntDefault("AURA_PROFILE_CERTAINTY_N", 3),

		WhatsAppBridgeURL: envDefault("AURA_WHATSAPP_BRIDGE_URL", "http://whatsapp:8081"),

		CalendarMCPURL:        envDefault("AURA_PIM_MCP_URL", "http://aura-pim-mcp:8080"),
		CalendarMCPAdminToken: envDefault("AURA_PIM_MCP_ADMIN_TOKEN", "changeme-aura-pim-local"),

		RerankBaseURL: envDefault("AURA_RERANK_BASE_URL", "http://127.0.0.1:8085"),
		RerankModel:   os.Getenv("AURA_RERANK_MODEL"),
	}
}

// composeDSN returns "" when password is empty so callers can detect an
// unconfigured DSN and fail-fast instead of dialing with a blank credential.
func composeDSN(role, password, host, port, dbname, sslmode string) string {
	if password == "" {
		return ""
	}
	u := url.URL{
		Scheme:  "postgres",
		User:    url.UserPassword(role, password),
		Host:    net.JoinHostPort(host, port),
		Path:    "/" + dbname,
		RawPath: "/" + url.PathEscape(dbname),
	}
	q := url.Values{}
	q.Set("sslmode", sslmode)
	u.RawQuery = q.Encode()
	return u.String()
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

// absRunDir normalizes an AURA_RUN_DIR value to an absolute path (F-041) so
// sidecars resolve against a stable root, not the process cwd — a relative value
// would make tool-result and conversation sidecars unreadable after a restart
// from a different directory, and read_tool_output hard-fails on a relative root.
// filepath.Abs is idempotent on an already-absolute path (defaultRunDir always is)
// and only errors when the cwd is unobtainable; that error is returned for Validate
// to surface at boot rather than silently keeping a relative path.
func absRunDir(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return dir, fmt.Errorf("AURA_RUN_DIR=%q could not be resolved to an absolute path: %w", dir, err)
	}
	return abs, nil
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
