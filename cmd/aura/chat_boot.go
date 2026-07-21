// chat_boot.go: the `aura chat` / `aura serve` composition root (D-A2-05). Split
// out of chat.go (refactor-on-touch, 600-LOC cap): this file owns the boot/assembly
// path — config.Load -> db.Open -> Stores + ScanOrphans + MCP mount -> Runner — while
// chat.go keeps the subcommand dispatch + REPL entry. The boot NEVER calls os.Exit
// (serve reuses it via bootServeChatEnv and shuts down gracefully, Pitfall 6);
// bootChatNamed is the CLI os.Exit translator.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/reasoningstore"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/chetto1983/aura/internal/share"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/chetto1983/aura/internal/toolselectstore"
	"github.com/jackc/pgx/v5/pgxpool"
)

// chatEnv is the booted composition root shared by every chat subcommand: the
// config, the open pool, the three Stores, and the Runner that drives the REPL.
type chatEnv struct {
	cfg             *config.Config
	pool            *pgxpool.Pool
	conv            *conversations.Store
	pause           *askuser.Store
	identity        *identity.Store
	run             *runner.Runner
	client          llm.Client
	reg             *tools.Registry
	gateway         *gateway.Gateway
	toolInvocations *toolinvocations.Store // the append-only ledger the gateway reserves + the reconciler sweeps
	assets          *assets.Service
	// shareSvc is the WEBSHARE-02/03 share lifecycle (buildShareService, share_service_wiring.go,
	// serve-only — nil under `aura chat`). Backs three composition-root seams at once: the HTTP
	// surface (via SetShareService's adapter), the D-15 delete cascade (chat.run.SetShareRevoker),
	// and buildDispatch's share_expiry_sweep handler.
	shareSvc    *share.Service
	toolHandles runtimeToolHandles
	mcpClosers  []func() error
	// sandboxRouter is the per-identity box routing seam (Phase 37, plan 37-05). It is nil
	// under a non-strict profile or a Docker-unavailable host — a safe host-direct no-op
	// everywhere (Route/Strict/SuspendIdle all nil-guard). buildDispatch registers it as the
	// KindSandboxReap reaper; plan 37-07 wires it onto the box-capable tools.
	sandboxRouter *usersandbox.SandboxRouter
}

// close releases the pool (the OTel TracerProvider is owned by the REPL path).
func (e *chatEnv) close() {
	if e.toolHandles.BackgroundShells != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.toolHandles.BackgroundShells.Shutdown(ctx)
	}
	// Drain the async reasoning learner BEFORE the graph client it writes through
	// is closed (mcpClosers below holds gclient.Close).
	if e.run != nil {
		e.run.CloseLearner()
	}
	_ = closeMCPServers(e.mcpClosers)
	if e.pool != nil {
		e.pool.Close()
	}
}

// bootChat builds the composition root (D-A2-05) for the `aura chat` callsites. It
// wraps the error-returning bootChatEnv (D-15) and translates a boot failure into the
// CLI's os.Exit convention (a missing API key is the one fail-fast with a friendly
// line, never a panic). The shared boot itself NEVER calls os.Exit, so `aura serve`
// can reuse it and handle a transient boot error gracefully (Pitfall 6).
func bootChat(ctx context.Context) *chatEnv {
	return bootChatNamed(ctx, "aura chat")
}

func bootChatNamed(ctx context.Context, label string) *chatEnv {
	env, err := bootChatEnv(ctx)
	if err != nil {
		if errors.Is(err, llm.ErrMissingAPIKey) || isMissingAPIKey(err) {
			fmt.Fprintln(os.Stderr, label+": "+llm.ErrMissingAPIKey.Error())
			os.Exit(1)
		}
		fmt.Fprintln(os.Stderr, label+":", err)
		os.Exit(1)
	}
	return env
}

// bootChatEnv is the error-returning composition root for `aura chat` (D-15). It
// uses fail-fast config.Load; serve shares the lower helper through bootServeChatEnv.
// It loads config, opens the pool, constructs the Stores, runs
// the boot orphan scan (Req#12) BEFORE serving, initializes the tiktoken encoder
// once, mounts MCP, and constructs the Runner. It NEVER calls os.Exit — every failure
// is returned so serve can shut down its already-booted resources cleanly (Pitfall 6:
// an os.Exit in the shared boot would skip a daemon's graceful shutdown).
func bootChatEnv(ctx context.Context) (*chatEnv, error) {
	return bootChatEnvWithConfig(ctx, config.Load)
}

// bootServeChatEnv shares the chat composition root with serve's keyless LLM
// config loader. Runtime LLM calls still fail closed if no key is configured.
func bootServeChatEnv(ctx context.Context) (*chatEnv, error) {
	return bootChatEnvWithConfig(ctx, config.LoadServe)
}

func bootChatEnvWithConfig(ctx context.Context, loadConfig func() (*config.Config, error)) (*chatEnv, error) {
	cfg, pool, err := resolveConfigAndPool(ctx, loadConfig, db.Open)
	if err != nil {
		return nil, err
	}
	return assembleChatEnv(ctx, cfg, pool)
}

// dbOpener opens a pgx pool from a DB config; it matches db.Open. resolveConfigAndPool
// takes it as a parameter so its pool lifecycle (close-on-reload-failure) is
// unit-testable without a live Postgres — db.Open eagerly Pings, so the real opener
// cannot be driven past pool-open in a test.
type dbOpener func(ctx context.Context, cfg *db.Config) (*pgxpool.Pool, error)

// resolveConfigAndPool loads the config and opens the DB pool, handing BOTH to
// assembleChatEnv. It runs the config → (optional settings-overlay) → reload sequence
// and owns the pool ONLY across a post-overlay reload failure: if the reload fails it
// closes the freshly-opened pool before returning (no leak). On success it returns the
// OPEN pool and the caller (assembleChatEnv) takes over its lifecycle. Returning the
// pool from here also fixes a latent shadow bug: the previous inline form declared the
// overlay pool with `:=` inside the keyless branch, shadowing the outer var, so an
// overlay-success boot proceeded on a nil pool.
func resolveConfigAndPool(ctx context.Context, loadConfig func() (*config.Config, error), open dbOpener) (*config.Config, *pgxpool.Pool, error) {
	cfg, err := loadConfig()
	if err != nil {
		if !errors.Is(err, llm.ErrMissingAPIKey) && !isMissingAPIKey(err) {
			return nil, nil, err
		}
		// Keyless first load: the required infra secrets may live in the DB settings
		// overlay. openSettingsOverlayPool returns a nil pool whenever !ok/err, so a
		// failed overlay path never leaks a live pool.
		pool, ok, overlayErr := openSettingsOverlayPool(ctx)
		if overlayErr != nil || !ok {
			return nil, nil, err
		}
		if err := settings.OverlayEnv(ctx, settings.NewStore(pool)); err != nil {
			fmt.Fprintln(os.Stderr, "warn: settings overlay:", err)
		}
		cfg, err = loadConfig()
		if err != nil {
			pool.Close()
			return nil, nil, err
		}
		return cfg, pool, nil
	}
	// Fail fast on an empty required infra secret (O-04) BEFORE opening any connection,
	// so a misconfigured deploy errors at boot with a named cause instead of a late DB
	// auth failure or a silently degraded graph. This pre-open Validate is the
	// load-bearing half of the intentional double-Validate: it must run before open()
	// (assembleChatEnv re-checks the RELOADED config after the overlay).
	if err := cfg.Validate(); err != nil {
		return nil, nil, err
	}
	pool, err := open(ctx, &cfg.DB)
	if err != nil {
		return nil, nil, fmt.Errorf("db open: %w", err)
	}
	if err := settings.OverlayEnv(ctx, settings.NewStore(pool)); err != nil {
		fmt.Fprintln(os.Stderr, "warn: settings overlay:", err)
	}
	cfg, err = loadConfig()
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return cfg, pool, nil
}

// releaseBootResources is the boot close-on-error path (QUAL-04b): it drains any MCP
// closers THEN closes the pool, mirroring chatEnv.close's shutdown order (the reasoning
// learner writes through a graph client held in the closers, so it must drain before the
// pool). Both arguments are nil-safe.
func releaseBootResources(pool interface{ Close() }, closers []func() error) {
	_ = closeMCPServers(closers)
	if pool != nil {
		pool.Close()
	}
}

// assembleChatEnv builds the composition root on an already-open pool. It arms a
// close-on-error guard FIRST so EVERY error return after this point releases the pool
// AND drains any MCP closers already opened — a boot failure (a bad command-hook config,
// a failed MCP mount, a reloaded config that no longer validates) never leaks a pgxpool
// or an MCP subprocess (QUAL-04b; the real leak was the CommandHookManagerFromEnv failure
// path, which returned after the pool + registry were built with no cleanup). The guard
// is disarmed only by the happy-path success flag, after which chatEnv.close owns the
// lifecycle.
func assembleChatEnv(ctx context.Context, cfg *config.Config, pool *pgxpool.Pool) (*chatEnv, error) {
	var mcpClosers []func() error
	success := false
	defer func() {
		if !success {
			releaseBootResources(pool, mcpClosers)
		}
	}()

	// Fail fast on an empty required infra secret (O-04) on the RELOADED config: the
	// pre-open Validate in resolveConfigAndPool guarded the initial config before
	// db.Open; this second Validate re-checks the post-overlay/reloaded config (a
	// secret the overlay cleared, or a reloaded value that no longer validates). BOTH
	// Validates are intentional — keep the pre-open one before any db.Open.
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// PROF-04 lenient half: under dev/local_trusted the profile-aware Validate above
	// does not fail on a misconfigured cataloged knob (it is a Warn, not a Fatal), so
	// surface those diagnostics here. This is a print, NOT a gate — no runtime
	// enforcement, no new gating call site (scope fence; fatals are already handled by
	// the profile-aware Validate above). Under the strict tiers boot already failed fast
	// on any Fatal, so only residual WARNs (e.g. a default prod bucket) reach this loop.
	for _, v := range cfg.ValidateProfile(cfg.Profile) {
		if v.Sev == config.Warn {
			fmt.Fprintf(os.Stderr, "warn: config: %s: %s\n", v.Knob, v.Msg)
		}
	}
	// D-14: local_trusted shares dev's constraint set exactly; this banner is the ONLY
	// behavioral differentiator (a trusted single-machine deploy with full host
	// capability active). dev prints no banner.
	if cfg.Profile == config.ProfileLocalTrusted {
		fmt.Fprintln(os.Stderr, "trusted local mode — full host capability active")
	}

	convStore := conversations.New(pool, conversations.Config{
		RunDir:       cfg.RunDir,
		TurnCapBytes: cfg.ConversationTurnCapBytes,
	})
	pauseStore := askuser.New(pool)
	idStore := identity.New(pool)
	cacheStore := cachemetrics.New(pool)
	toolInvocationStore := toolinvocations.New(pool)
	// The single in-process policy PEP (GATE-01), constructed ONCE and injected at the
	// three NewLlmAgent roots (runner below, swarm via ctx-relay, cron agent_job).
	gw := gateway.New(cfg.Profile, toolInvocationStore)

	// Boot reconciliation GC (D-A5-02 / Req#12): reconcile orphan sidecar dirs
	// BEFORE serving. A scan failure is a WARN-level degradation, not a boot-blocker.
	if err := conversations.ScanOrphans(ctx, pool, conversations.ScanParams{
		RunDir:             cfg.RunDir,
		WarnThresholdBytes: int64(cfg.RunDirWarnThresholdBytes),
	}); err != nil {
		fmt.Fprintln(os.Stderr, "warn: boot orphan scan:", err)
	}

	// Initialize the cl100k tiktoken encoder once (D-A2-06) so the first turn does
	// not pay the vocab-parse latency.
	if err := conversations.InitEncoder(); err != nil {
		fmt.Fprintln(os.Stderr, "warn: tiktoken init:", err)
	}

	// The per-identity box router (Phase 37) is built ONCE here so the SAME instance backs both the
	// box-capable tools (routed below at registry construction, plan 37-07) AND the idle-suspend
	// reaper (buildDispatch reads chat.sandboxRouter). Nil under a non-strict profile or a
	// Docker-unavailable host — a safe host-direct no-op everywhere (SC-4).
	sandboxRouter := buildSandboxRouter(cfg)

	// The live `task` tool persists against the open pool (10-05 deviation #3): both
	// `aura chat` and `aura serve` get the scheduler verb wired to the real DB. sandboxRouter
	// threads onto shell_exec/fs_read/fs_write/send_file/skill (NOT web_*, D-11).
	// Retain the task-store adapter: it backs BOTH the `task` tool (registry) AND the
	// scheduled-task resume hook (on-channel HITL approval) below.
	taskStore := newCronTaskStore(pool, convStore)
	reg, toolHandles, mcpClosers, err := buildRegistryWithMCP(ctx, cfg, taskStore, sandboxRouter)
	if err != nil {
		// pool + closers released by the deferred close-on-error guard.
		return nil, fmt.Errorf("mcp: %w", err)
	}

	// Fail-loud boot wiring guard (D-02d): a mutating multiplexed tool the classifier
	// cannot tier panics here rather than silently under-gating a live turn.
	gateway.ValidateClassifiable(reg)

	// Reasoning-tier self-improvement (spike 053) is one opt-in feature behind
	// AURA_LLM_REASONING_LEARNING: only when it is on do we open a graph client
	// (the mcp-neo4j-cypher subprocess) so the classifier can fold oracle-labeled
	// :ReasoningExample nodes (read) and the async learner write new ones. Default
	// OFF means the classifier runs seed-only — its validated baseline — with zero
	// extra subprocess. Best-effort even when on: a missing binary or a down Neo4j
	// leaves it seed-only and never blocks boot.
	var reasoningStore *reasoningstore.Store
	var toolSelectStore *toolselectstore.Store
	// WR-05 — coupling note: the tool-selection active-learning loop has NO independent
	// enable flag. It deliberately rides AURA_LLM_REASONING_LEARNING (cfg.LLM.ReasoningLearning)
	// below: the toolSelectStore is constructed only inside this gate, sharing the SAME
	// graph client the reasoning learner opens (no extra mcp-neo4j-cypher subprocess). So
	// reasoning-tier self-improvement OFF ⇒ tool-selection self-improvement OFF too, and
	// vice versa; they cannot be toggled independently. AURA_TOOLSELECT_ORACLE gates only
	// the PAID escalation tier, never whether the loop runs at all. (Documented per WR-05;
	// a dedicated env var was deliberately NOT added to avoid scope expansion.)
	if cfg.LLM.ReasoningLearning {
		if gclient, gerr := knowledge.Open(ctx, &cfg.Neo4j); gerr != nil {
			fmt.Fprintln(os.Stderr, "warn: reasoning example store unavailable:", gerr)
		} else {
			mcpClosers = append(mcpClosers, gclient.Close)
			reasoningStore = &reasoningstore.Store{Client: gclient}
			// The tool-selection active-learning loop (D-06/D-07) rides the SAME graph
			// client: when the reasoning learner is on (and Neo4j opened) the tool_search
			// ranker also self-improves via :ToolSelectionExample. No extra subprocess.
			toolSelectStore = &toolselectstore.Store{Client: gclient}
		}
	}

	hookManager, err := agent.CommandHookManagerFromEnv(os.LookupEnv)
	if err != nil {
		return nil, fmt.Errorf("command hooks: %w", err)
	}
	client := newLLMClient(cfg.LLM)
	deps := runner.Deps{
		Conv:            convStore,
		Pause:           pauseStore,
		Identity:        idStore,
		CacheMetrics:    cacheStore,
		ToolInvocations: toolInvocationStore,
		// Atomic cross-store HITL durability (D-03/D-05): the pool-owning committer spans
		// a pause claim + its answer turn (and pause exposure) in ONE db.WithTx.
		ResumeCommitter:  runner.NewPoolResumeCommitter(pool, convStore, pauseStore),
		Client:           client,
		Registry:         reg,
		LLM:              cfg.LLM,
		RunDir:           cfg.RunDir,
		PreviewCap:       cfg.ToolPreviewCap,
		EvictAfter:       cfg.ContextToolEvictAfterTurns,
		ArchivalRecaller: archivalRecallProvider(cfg), // nil unless AURA_CONTEXT_MEMORY_RECALL (default off)
		AlwaysBlock:      alwaysBlockProvider(cfg),
		// The gateway resume hook records an operator's accept of a relayed gateway_approval
		// pause into the SAME gateway instance (gw) the runner's PEP reads (D-03 point 2).
		ResumeHook:  chainResumeHooks(newSkillResumeHook(cfg, pool), newShellResumeHook(toolHandles.ShellApprovals), newGatewayResumeHook(gw), newScheduledTaskResumeHook(taskStore)),
		HookManager: hookManager,
		Gateway:     gw,
		// Local embedding-based reasoning-tier classifier (granite sidecar):
		// replaces the per-turn LLM router round-trip. Empty EmbedURL => the agent
		// falls back to the LLM router.
		Embedder: embeddingClient(cfg, documentHTTPClient(cfg)),
	}
	// Set the store-backed fields only when the graph client opened (a nil
	// *Store wrapped in an interface would be a non-nil interface and panic).
	if reasoningStore != nil {
		deps.ExampleStore = reasoningStore
		deps.ReasoningSaver = reasoningStore
	}
	if toolSelectStore != nil {
		deps.ToolSelectSaver = toolSelectStore
	}
	run := runner.New(deps)
	success = true // disarm the close-on-error guard; chatEnv.close now owns the lifecycle.
	return &chatEnv{cfg: cfg, pool: pool, conv: convStore, pause: pauseStore, identity: idStore, run: run, client: client, reg: reg, gateway: gw, toolInvocations: toolInvocationStore, toolHandles: toolHandles, mcpClosers: mcpClosers, sandboxRouter: sandboxRouter}, nil
}

func openSettingsOverlayPool(ctx context.Context) (*pgxpool.Pool, bool, error) {
	dbCfg := config.LoadDB()
	if strings.TrimSpace(dbCfg.DB.URL) == "" {
		return nil, false, nil
	}
	pool, err := db.Open(ctx, &dbCfg.DB)
	if err != nil {
		return nil, false, err
	}
	return pool, true, nil
}
