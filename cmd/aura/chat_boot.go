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
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent"
	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/approvalgrants"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/assets"
	"github.com/chetto1983/aura/internal/cachemetrics"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/identity"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/onboarding"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/settings"
	"github.com/chetto1983/aura/internal/share"
	"github.com/chetto1983/aura/internal/steer"
	"github.com/chetto1983/aura/internal/toolinvocations"
	"github.com/jackc/pgx/v5/pgxpool"
)

// chatEnv is the booted composition root shared by every chat subcommand: the
// config, the open pool, the three Stores, and the Runner that drives the REPL.
type chatEnv struct {
	cfg                   *config.Config
	pool                  *pgxpool.Pool
	conv                  *conversations.Store
	pause                 *askuser.Store
	identity              *identity.Store
	run                   *runner.Runner
	client                llm.Client
	llmRuntime            *llm.Runtime
	llmFallback           llm.Config
	reg                   *tools.Registry
	gateway               *gateway.Gateway
	operations            *idempotency.Store
	toolInvocations       *toolinvocations.Store // the append-only ledger the gateway reserves + the reconciler sweeps
	deleteReconciler      *runner.DeleteReconciler
	conversationProjector *runner.ConversationProjector
	memoryCaptureQueue    *runner.MemoryCaptureQueue
	assets                *assets.Service
	// shareSvc is the WEBSHARE-02/03 share lifecycle (buildShareService, share_service_wiring.go,
	// serve-only — nil under `aura chat`). Backs three composition-root seams at once: the HTTP
	// surface (via SetShareService's adapter), the D-15 delete cascade (chat.run.SetShareRevoker),
	// and buildDispatch's share_expiry_sweep handler.
	shareSvc    *share.Service
	toolHandles runtimeToolHandles
	mcpClosers  []func() error
	liveMCP     *liveMCPMount
	// sandboxRouter is the per-identity box routing seam (Phase 37, plan 37-05) and the ONLY
	// execution path for shell_exec and the fs_* tools. buildSandboxRouter never returns nil: a
	// composition failure yields a backend-less router whose every Route denies, because nil
	// would be read as "no containment needed". buildDispatch registers it as the
	// KindSandboxReap reaper; plan 37-07 wires it onto the box-capable tools.
	sandboxRouter *usersandbox.SandboxRouter
	// elicitation is the late-bound SEP-2322 consent surface (plan 45.1-06/07).
	// serve.go binds the channels Registry onto it once that Registry exists;
	// `aura chat` leaves it unbound, and an unbound holder declines with a WARN.
	elicitation *surfacingElicitationConsent
	// steer is the shared process-wide mid-turn steer inbox (amendment #132,
	// AURA_AGUI_RUN_STEER, D-01/D-12): the ONE *steer.PostgresStore instance
	// injected into both runner.Deps.Steer (the agent-side drain, boxed via
	// runner.SteerInboxOrNil) and agui.Server.SetSteerInbox (the cockpit
	// route), so a push and a drain can never disagree about which queue they
	// mean (T-52-31). Postgres-backed with a per-kind TTL since Phase 51 plan
	// 02 (D-06/D-07) — the in-memory Inbox this field held before is deleted,
	// not kept behind a flag. nil when the AGUISteer flag is off — every
	// consumer's own nil-safe seam then degrades to "steer is unwired" rather
	// than half-live.
	steer *steer.PostgresStore
}

// close releases the pool (the OTel TracerProvider is owned by the REPL path).
func (e *chatEnv) close() {
	if e.deleteReconciler != nil {
		e.deleteReconciler.Stop()
	}
	if e.memoryCaptureQueue != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		if err := e.memoryCaptureQueue.Close(ctx); err != nil {
			slog.Warn("memory capture shutdown incomplete", "error", err)
		}
		cancel()
	}
	if e.conversationProjector != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := e.conversationProjector.Close(ctx); err != nil {
			slog.Warn("conversation projector shutdown incomplete", "error", err)
		}
		cancel()
	}
	if e.toolHandles.BackgroundShells != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = e.toolHandles.BackgroundShells.Shutdown(ctx)
	}
	if e.liveMCP != nil {
		_ = e.liveMCP.Close()
	}
	_ = closeMCPServers(e.mcpClosers)
	if e.pool != nil {
		e.pool.Close()
	}
}

// bootCLIChat is what every INTERACTIVE CLI entry point uses (D-A2-05): it builds the
// composition root and scopes the returned ctx to the operator identity.
//
// The scoping is not optional. Without a principal on ctx the runner falls back to an
// identity literally named `local` — the pre-Authula seed, which serve_auth.go DELETES the
// first time an operator enrolls. An appliance provisioned through onboarding therefore has
// no `local` row at all, and every CLI conversation call died with
// `resolve owner identity: get identity "local": identity not found`. It is resolved AFTER
// the boot on purpose: config validation (a missing API key above all) must still be the
// first thing an operator hears about.
//
// `aura serve` deliberately does NOT come through here — it boots via bootServeChatEnv and
// carries a real authenticated principal per request (agui.withPrincipal).
func bootCLIChat(ctx context.Context, label string) (context.Context, *chatEnv) {
	env := bootChatNamed(ctx, label)
	identityID, err := identityctx.OperatorIdentity(ctx, env.pool)
	if err != nil {
		env.close()
		fmt.Fprintln(os.Stderr, label+":", err)
		os.Exit(1)
	}
	return identityctx.WithIdentityID(ctx, identityID), env
}

// bootChatNamed wraps the error-returning bootChatEnv (D-15) and translates a boot
// failure into the CLI's os.Exit convention (a missing API key is the one fail-fast with
// a friendly line, never a panic). The shared boot itself NEVER calls os.Exit, so
// `aura serve` can reuse it and handle a transient boot error gracefully (Pitfall 6).
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
// It loads config, opens the pool, verifies migration compatibility, constructs the
// Stores, runs the boot orphan scan (Req#12) BEFORE serving, initializes the
// tiktoken encoder once, mounts MCP, and constructs the Runner. It NEVER calls
// os.Exit — every failure is returned so serve can shut down its already-booted
// resources cleanly (Pitfall 6: an os.Exit in the shared boot would skip a daemon's
// graceful shutdown).
func bootChatEnv(ctx context.Context) (*chatEnv, error) {
	return bootChatEnvWithConfig(ctx, config.Load)
}

// bootServeChatEnv shares the chat composition root with serve's keyless LLM
// config loader. Runtime LLM calls still fail closed if no key is configured.
func bootServeChatEnv(ctx context.Context) (*chatEnv, error) {
	return bootChatEnvWithConfig(deferOAuthMountsUntilListener(ctx), config.LoadServe)
}

func bootChatEnvWithConfig(ctx context.Context, loadConfig func() (*config.Config, error)) (*chatEnv, error) {
	// Capture the pre-settings route once. DELETE on a hot route must restore this
	// deployment fallback, not the DB value OverlayEnv copies into process env below.
	baseline, baselineErr := loadConfig()
	cfg, pool, err := resolveConfigAndPool(ctx, loadConfig, db.Open)
	if err != nil {
		return nil, err
	}
	env, err := assembleChatEnvAtMigrationHead(
		ctx, cfg, pool, db.CheckMigrationHead, db.VerifyRLSEnforced, assembleChatEnv,
	)
	if err != nil {
		return nil, err
	}
	env.llmFallback = cfg.LLM
	if baselineErr == nil && baseline != nil {
		env.llmFallback = baseline.LLM
	}
	return env, nil
}

// dbOpener opens a pgx pool from a DB config; it matches db.Open. resolveConfigAndPool
// takes it as a parameter so its pool lifecycle (close-on-reload-failure) is
// unit-testable without a live Postgres — db.Open eagerly Pings, so the real opener
// cannot be driven past pool-open in a test.
type dbOpener func(ctx context.Context, cfg *db.Config) (*pgxpool.Pool, error)

type migrationHeadChecker func(context.Context, string) error

// rlsEnforcementChecker matches db.VerifyRLSEnforced. It is injected for the same reason
// migrationHeadChecker is: the real one needs a live Postgres to answer.
type rlsEnforcementChecker func(context.Context, *pgxpool.Pool) error

type bootSettingsOps struct {
	openKeyless func(context.Context) (*pgxpool.Pool, bool, error)
	overlay     func(context.Context, *pgxpool.Pool) error
}

type chatEnvAssembler func(
	context.Context,
	*config.Config,
	*pgxpool.Pool,
) (*chatEnv, error)

func assembleChatEnvAtMigrationHead(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
	check migrationHeadChecker,
	checkRLS rlsEnforcementChecker,
	assemble chatEnvAssembler,
) (*chatEnv, error) {
	if err := check(ctx, cfg.DB.MigrateURL); err != nil {
		releaseBootResources(pool, nil)
		return nil, fmt.Errorf("postgres migration compatibility: %w", err)
	}
	// A superuser or BYPASSRLS role silently voids every owner-isolation policy, so this
	// refuses to serve rather than serve without tenant isolation. It sits next to the
	// migration-head gate because both are the same kind of boot-time contract check on a
	// database the daemon did not provision itself.
	if err := checkRLS(ctx, pool); err != nil {
		releaseBootResources(pool, nil)
		return nil, err
	}
	return assemble(ctx, cfg, pool)
}

// resolveConfigAndPool loads the config and opens the DB pool, handing BOTH to
// migration gate and assembleChatEnv. It owns the pool across every post-open
// failure: if the settings overlay or the config reload fails it closes the pool
// before returning. On success it returns the OPEN pool and the caller takes over
// its lifecycle. Returning the pool from here also fixes a latent shadow bug: the
// previous inline form declared the overlay pool with `:=` inside the keyless branch,
// shadowing the outer var, so an overlay-success boot proceeded on a nil pool.
func resolveConfigAndPool(ctx context.Context, loadConfig func() (*config.Config, error), open dbOpener) (*config.Config, *pgxpool.Pool, error) {
	return resolveConfigAndPoolWithSettings(
		ctx,
		loadConfig,
		open,
		bootSettingsOps{
			openKeyless: openSettingsOverlayPool,
			overlay: func(ctx context.Context, pool *pgxpool.Pool) error {
				return settings.OverlayEnv(ctx, settings.NewStore(pool))
			},
		},
	)
}

func resolveConfigAndPoolWithSettings(
	ctx context.Context,
	loadConfig func() (*config.Config, error),
	open dbOpener,
	settingsOps bootSettingsOps,
) (*config.Config, *pgxpool.Pool, error) {
	cfg, err := loadConfig()
	if err != nil {
		if !errors.Is(err, llm.ErrMissingAPIKey) && !isMissingAPIKey(err) {
			return nil, nil, err
		}
		// Keyless first load: the required infra secrets may live in the DB settings
		// overlay. openSettingsOverlayPool returns a nil pool whenever !ok/err, so a
		// failed overlay path never leaks a live pool.
		pool, ok, overlayErr := settingsOps.openKeyless(ctx)
		if overlayErr != nil || !ok {
			return nil, nil, err
		}
		overlayBootSettings(ctx, pool, settingsOps)
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
	overlayBootSettings(ctx, pool, settingsOps)
	cfg, err = loadConfig()
	if err != nil {
		pool.Close()
		return nil, nil, err
	}
	return cfg, pool, nil
}

func overlayBootSettings(
	ctx context.Context,
	pool *pgxpool.Pool,
	settingsOps bootSettingsOps,
) {
	if err := settingsOps.overlay(ctx, pool); err != nil {
		fmt.Fprintln(os.Stderr, "warn: settings overlay:", err)
	}
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
func assembleChatEnv(
	ctx context.Context,
	cfg *config.Config,
	pool *pgxpool.Pool,
) (*chatEnv, error) {
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
	operations := idempotency.New(pool, idempotency.Config{})
	gw.SetOperationRegistry(operations)
	// The durable half of the approval scopes (amendment #127). Without it the gateway keeps
	// the two in-memory scopes and an "always" accept degrades to "for this conversation" —
	// the honest failure, not a silent one.
	gw.SetGrantStore(approvalgrants.New(pool))

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
	// reaper (buildDispatch reads chat.sandboxRouter). Never nil: an unreachable Docker daemon
	// yields a router that denies every call, which is the containment answer (GATE-01).
	sandboxRouter := buildSandboxRouter(cfg)

	// The live `task` tool persists against the open pool (10-05 deviation #3): both
	// `aura chat` and `aura serve` get the scheduler verb wired to the real DB. sandboxRouter
	// threads onto shell_exec/fs_read/fs_write/send_file/skill (NOT web_*, D-11).
	// Retain the task-store adapter: it backs BOTH the `task` tool (registry) AND the
	// scheduled-task resume hook (on-channel HITL approval) below.
	taskStore := newCronTaskStore(pool, convStore)
	// The elicitation consent surface is created BEFORE the mount and bound AFTER
	// the channels Registry exists (serve.go). Mounts happen here; channels do not
	// exist yet, so binding at construction would always bind nil.
	elicitation := newElicitationConsent()
	reg, toolHandles, mcpClosers, err := buildRegistryWithMCP(
		ctx,
		cfg,
		pool,
		taskStore,
		sandboxRouter,
		elicitation,
	)
	if err != nil {
		// pool + closers released by the deferred close-on-error guard.
		return nil, fmt.Errorf("mcp: %w", err)
	}
	// The skill library is a deployment-wide administrator catalog. Attach the live
	// identity store to the exact skill_manage instance before any turn can execute it;
	// pool-free manifest paths retain a nil checker and therefore fail closed.
	wireSkillManageCapability(&toolHandles, idStore)
	// Fail-loud boot wiring guard (D-02d): a mutating multiplexed tool the classifier
	// cannot tier panics here rather than silently under-gating a live turn.
	gateway.ValidateClassifiable(reg)

	hookManager, err := agent.CommandHookManagerFromEnv(os.LookupEnv)
	if err != nil {
		return nil, fmt.Errorf("command hooks: %w", err)
	}
	// Resolve provider-published context, output cap, and rates before constructing
	// the boot snapshot. Metadata remains fail-soft at boot so an unavailable catalogue
	// cannot prevent Aura from starting with its validated deployment fallback.
	if err := cfg.LLM.ResolveModelProfile(ctx); err != nil {
		slog.Warn("model profile unresolved; using configured fallback",
			"provider", cfg.LLM.Provider, "model", cfg.LLM.Model, "err", err)
	}
	client := newLLMClient(cfg.LLM)
	llmRuntime := llm.NewRuntime(client, cfg.LLM)
	// The mid-turn steer inbox (amendment #132, D-01/D-12): ONE
	// *steer.PostgresStore instance for the whole process, gated on its own
	// flag exactly as RunRegistry is gated on AGUIRun.Detach, so the explicit
	// rollback leaves a nil seam rather than a half-live feature.
	var steerInbox *steer.PostgresStore
	if cfg.AGUISteer.Enabled {
		steerInbox = newSteerInbox(pool, cfg)
	}
	memoryContext := toolHandles.MemoryContext
	if memoryContext == nil && toolHandles.Memory != nil {
		memoryContext = newMemoryContextProvider(toolHandles.Memory, cfg.MemoryPreloadTopK, time.Duration(cfg.MemoryPreloadTimeoutMS)*time.Millisecond)
	}
	toolHandles.MemoryContext = memoryContext
	conversationProjector := newChatConversationProjector(cfg, convStore)
	reasoningMemory := newChatReasoningMemory(cfg)
	memoryCaptureQueue := buildMemoryCaptureQueue(cfg)
	deps := runner.Deps{
		Conv:                  convStore,
		Pause:                 pauseStore,
		ApprovalExpiry:        pauseStore,
		Identity:              idStore,
		CacheMetrics:          cacheStore,
		ToolInvocations:       toolInvocationStore,
		MemoryContext:         memoryContext,
		MemoryCaptureQueue:    memoryCaptureQueue,
		ConversationProjector: conversationProjector,
		// Atomic cross-store HITL durability (D-03/D-05): the pool-owning committer spans
		// a pause claim + its answer turn (and pause exposure) in ONE db.WithTx.
		ResumeCommitter: runner.NewPoolResumeCommitter(pool, convStore, pauseStore),
		Client:          client,
		Runtime:         llmRuntime,
		Registry:        reg,
		Timezone:        cfg.Timezone,
		// The operator profile from Postgres (migration 0097): the clock zone this turn
		// renders in, and the deterministic block on messages[1]. It replaced a memory-graph
		// read that the turn path could not make.
		Profiles: onboarding.NewProfileStore(pool),
		LLM:      cfg.LLM,
		RunDir:   cfg.RunDir,
		// Amendment #88: the per-turn "Working directory" hint mirrors the fixed
		// WorkspaceRoot every host-direct tool now resolves against, replacing the
		// process-cwd fallback (runner.New still falls back to os.Getwd if this is "").
		Workspace:            cfg.WorkspaceDir,
		PreviewCap:           cfg.ToolPreviewCap,
		HistoryCap:           cfg.HistoryHardCapTurns,
		EvictAfter:           cfg.ContextToolEvictAfterTurns,
		CompactionEnabled:    cfg.ContextCompactionEnabled,
		CompactionModel:      cfg.ContextCompactionModel,
		MemoryPreloadEnabled: cfg.MemoryPreloadEnabled,
		// Amendment #91 (fix-plan 1.12): bounded display-only CoT persistence cap.
		ReasoningPersistMaxRunes: cfg.ReasoningPersistMaxRunes,
		AlwaysBlock:              alwaysBlockProvider(cfg),
		// The gateway resume hook records an operator's accept of a relayed gateway_approval
		// pause into the SAME gateway instance (gw) the runner's PEP reads (D-03 point 2).
		ResumeHook:  chainResumeHooks(newShellResumeHook(toolHandles.ShellApprovals), newGatewayResumeHook(gw), newScheduledTaskResumeHook(taskStore)),
		HookManager: hookManager,
		// The verify-on-stop evidence ledger (migration 0094). Built ONCE here because it
		// holds the pool; the runner derives the per-turn read and write halves from it.
		VerificationStore: agent.NewEvidenceStore(pool),
		// Project detection reads the SAME per-identity box the agent's shell runs in —
		// the aura process's own /workspace is a different volume, so a host-filesystem
		// detector answered "not a project" for every path the agent could name. Built
		// once here too: it memoizes one box probe per (identity, directory).
		VerificationDetector: agent.NewBoxProjectDetector(sandboxRouter),
		Gateway:              gw,
		// Local embedding-based reasoning-tier classifier (embedding sidecar):
		// replaces the per-turn LLM router round-trip. Empty EmbedURL => the agent
		// falls back to the LLM router.
		Embedder: embeddingClient(cfg, documentHTTPClient(cfg)),
		// SteerInboxOrNil is the ONE place a concrete, possibly-nil *steer.PostgresStore
		// is boxed into the agent.SteerInbox interface Deps.Steer now carries (Phase 51
		// plan 02) — avoids the classic Go nil-interface trap (D-06).
		Steer: runner.SteerInboxOrNil(steerInbox),
	}
	wireChatReasoningMemory(&deps, reasoningMemory)
	run := runner.New(deps)
	if err := run.ValidateCompactionConfig(); err != nil {
		return nil, fmt.Errorf("chat boot: %w", err)
	}
	deleteReconciler := runner.NewDeleteReconciler(run, time.Minute)
	roster := identityRoster{store: idStore}
	wireChatConversationReconciliation(
		deleteReconciler, conversationProjector, roster,
	)
	if reasoningMemory != nil {
		deleteReconciler.SetReasoningRetention(reasoningMemory.retention, roster, cfg.Retention.BatchSize)
	}
	deleteReconciler.Start(ctx)
	success = true // disarm the close-on-error guard; chatEnv.close now owns the lifecycle.
	return &chatEnv{cfg: cfg, pool: pool, conv: convStore, pause: pauseStore, identity: idStore, run: run, client: client, llmRuntime: llmRuntime, reg: reg, gateway: gw, operations: operations, toolInvocations: toolInvocationStore, deleteReconciler: deleteReconciler, conversationProjector: conversationProjector, memoryCaptureQueue: memoryCaptureQueue, toolHandles: toolHandles, mcpClosers: mcpClosers, sandboxRouter: sandboxRouter, elicitation: elicitation, steer: steerInbox}, nil
}

// newSteerInbox builds the process-wide mid-turn steer/delegation-result store from
// the ratified AURA_AGUI_RUN_STEER* caps (amendment #132 item 10) and the D-06/D-07
// per-kind TTLs (Phase 51 plan 02, AURA_STEER_QUEUE_TTL_SEC / AURA_DELEGATION_RESULT_TTL_SEC),
// EXPLICITLY — never a zero steer.Config. internal/steer's own package-level fallbacks
// (defaultMax=32, defaultMaxBytes=32768) exist only for a caller that never wires a
// Config at all; this composition root is not that caller, so passing cfg's fields
// verbatim is what keeps the two default sets from silently diverging again — the D-11
// catalogue-vs-loader drift, reproduced one layer down, that 52-04 exists to close.
func newSteerInbox(pool *pgxpool.Pool, cfg *config.Config) *steer.PostgresStore {
	return steer.NewPostgresStore(pool, steer.Config{
		Max:                 cfg.AGUISteer.Max,
		MaxBytes:            cfg.AGUISteer.MaxBytes,
		SteerTTL:            time.Duration(cfg.SteerQueueTTLSec) * time.Second,
		DelegationResultTTL: time.Duration(cfg.DelegationResultTTLSec) * time.Second,
	})
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
