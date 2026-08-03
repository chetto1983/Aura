// serve subcommand for `aura serve`: the first long-lived Aura daemon (D-15). It
// hosts the scheduler tick loop on the shared composition root (bootChatEnv, the
// error-returning boot also used by `aura chat`), wires the real per-TaskKind
// handlers + the composite Notifier + the live cron store into the cron Dispatcher
// seam (the wiring 10-05 deferred to the composition root), and runs until SIGINT/
// SIGTERM. Shutdown is graceful: cancelling the root ctx stops new ticks, the
// in-flight tick finishes + joins its workers (Scheduler.Start returns), then the
// MCP closers are reverse-closed and the pool released — goleak-clean (Pitfall 6:
// the shared boot has no os.Exit, so this shutdown path always runs).
//
// The serve daemon owns the LIVE store wiring three downstream seams need:
//   - the cron.Dispatch handler map (reminder/agent_job/backup_*) — handlers import
//     internal/agent/tools, and tools imports cron, so cron cannot import handlers;
//     the map is adapted here (the composition root imports both, 10-05 deviation #1);
//   - a *tools.Registry → cron.SelfSendResolver adapter for the MCP self-send Notifier;
//   - a cron.Store → tools.taskStore adapter injected into the live `task` tool so the
//     LLM-facing scheduler verb persists against the real DB (10-05 deviation #3).
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/channels"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/envutil"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/readiness"
	"github.com/chetto1983/aura/internal/webauth"
)

// aguiShutdownTimeout bounds the HTTP-layer drain of in-flight SSE streams inside
// drainShutdown (the bounded-drain body, O-06/AP-17). The outer drainWithGrace
// (AURA_SERVE_SHUTDOWN_GRACE_SEC) bounds the whole drain; this is the inner
// http.Server.Shutdown deadline so a slow SSE client cannot wedge the HTTP teardown.
const aguiShutdownTimeout = 10 * time.Second

// aguiReadHeaderTimeout bounds the request-header read to defang slow-loris on the
// unauthenticated loopback endpoint (T-12-09).
const aguiReadHeaderTimeout = 10 * time.Second

// reconcileTickInterval is the cadence of the crash-orphan reconciler in the daemon
// (D-01d / GATE-04 recovery): often enough to close a crash-orphaned reservation within
// a bounded window, sparse enough that the start∧¬end anti-join is negligible load. It
// is a fixed default (no new env knob this phase); the boot one-shot fires immediately
// at start, so a daemon that just came up after a crash reconciles before the first tick.
const reconcileTickInterval = 10 * time.Minute

const serveUsage = "usage: aura serve [--no-telegram | --only=cli]"

// resolveMaxToolExecWindow returns the upper bound on a single run's tool-execution
// lifetime — max(AURA_LOOP_NODE_TIMEOUT_SEC, AURA_LOOP_MAX_WALLCLOCK_SEC), defaults 0 /
// 300s (mirroring agent.defaultBudgetWallclockSec). It feeds the reconciler's
// effectiveGrace so the grace strictly exceeds the run lifetime even if an operator
// raises either knob (the WARNING-4 collision-impossibility invariant). A simple
// getenv-with-default is deliberate here — the Budget owns the fail-fast parse.
func resolveMaxToolExecWindow() time.Duration {
	nodeTimeout := time.Duration(envutil.IntDefault("AURA_LOOP_NODE_TIMEOUT_SEC", 0)) * time.Second
	wallclock := time.Duration(envutil.IntDefault("AURA_LOOP_MAX_WALLCLOCK_SEC", 300)) * time.Second
	if nodeTimeout > wallclock {
		return nodeTimeout
	}
	return wallclock
}

// serveEnv is the booted daemon: the shared chat composition root plus the cron
// Store + Scheduler the tick loop runs. close() reverse-releases everything the boot
// acquired (MCP closers + pool) via the embedded chatEnv.
type serveEnv struct {
	*chatEnv
	store     *cron.Store
	scheduler *cron.Scheduler
	httpSrv   *http.Server // the AG-UI gateway (Slice 8b), mounted alongside the tick loop
	readiness *readiness.Snapshot

	// channels is the Phase-13 channels Registry (Telegram). It mounts as a
	// fail-soft daemon sibling of the AG-UI gateway; runServe StartAll/StopAll it.
	channels *channels.Registry
	// setupSrv is the loopback setup-wizard HTTP server (:9081, Slice 9a/UX-03), a
	// third http.Server sibling to httpSrv. runServe runs + Shutdowns it fail-soft.
	setupSrv *http.Server

	// sweeper is the periodic sidecar-sweep worker (audit M-06 part 2): in a long-
	// running daemon sidecars accumulate between reboots, so it re-runs the boot
	// ScanOrphans on AURA_RUN_DIR_SWEEP_INTERVAL_SEC. runServe Start/Stops it.
	sweeper *conversations.Sweeper

	// assetProcessingWorker claims durable asset_process ingestion jobs and runs the
	// shared asset processor pipeline. runServe Start/Stops it with the daemon.
	assetProcessingWorker *runtimeIngestionWorker

	// reconciler is the crash-orphan reconciler (D-01d / GATE-03 durability + GATE-04
	// recovery): it closes a start∧¬end reservation left by a crash by appending a
	// terminal indeterminate `end` fact, never re-invoking a mutating orphan. Its
	// lifecycle mirrors the sweeper's (Start at boot, Stop in drainShutdown, goleak-clean).
	reconciler *gateway.Reconciler

	// authulaProvider is the active embedded Authula web-auth framework.
	// onboardingAuthulaProvider is kept as a distinct slot so cleanup stays correct if a
	// future setup-only composition creates a separate provisioning provider.
	authulaProvider           *webauth.Provider
	onboardingAuthulaProvider *webauth.Provider

	// runRegistry is the detached-run session registry (fix-plan 1.3 Tier B); nil
	// unless AURA_AGUI_RUN_DETACH=true. drainShutdown Closes it (cancel-walk every
	// detached run + reaper join) BEFORE the HTTP drain so cancelled producers flush
	// their terminal frames and attached SSE viewers close promptly.
	runRegistry *agui.RunRegistry
}

// close reverse-releases the daemon-owned resources the embedded chatEnv does not:
// the Authula provider's cleanup workers (when wired), THEN the shared chatEnv close
// (pool + MCP closers). It is the single teardown the deferred env.close() in runServe
// drives.
func (e *serveEnv) close() {
	closeAuthulaProviders(e.authulaProvider, e.onboardingAuthulaProvider)
	e.chatEnv.close()
}

func closeAuthulaProviders(active, onboarding *webauth.Provider) {
	if onboarding != nil && onboarding != active {
		if err := onboarding.Close(); err != nil {
			slog.Warn("aura serve: onboarding authula provider close", "err", err)
		}
	}
	if active != nil {
		if err := active.Close(); err != nil {
			slog.Warn("aura serve: authula provider close", "err", err)
		}
	}
}

// runServe is the `aura serve` entry point: boot, start the tick loop, block on a
// termination signal, then shut down gracefully. A boot failure exits non-zero with a
// human-readable line (sysexits posture, the web/task CLI convention); the daemon
// itself never panics on a transient fault.
func runServe(args []string) {
	for _, a := range args {
		if a != "--no-telegram" && a != "--only=cli" {
			fmt.Fprintln(os.Stderr, serveUsage)
			os.Exit(exitUsage)
		}
	}
	override, _ := serveTelegramOverride(args)

	// signal.NotifyContext cancels signalCtx on SIGINT/SIGTERM; the scheduler's Start
	// returns on that cancel after the in-flight tick drains (graceful, D-15). The
	// signal is ONLY the stop-trigger — it must NOT directly cancel the per-turn work
	// ctx, or an in-flight turn is hard-killed mid-stream instead of reaching its
	// terminal frame (audit O-06 / AP-17). workCtx is therefore a SEPARATE,
	// background-derived ctx that parents the actual work (the channel handler turns +
	// HTTP requests); on the signal the daemon stops accepting NEW work, drains the
	// in-flight turns under a bounded grace, THEN workCancel fires as the final backstop.
	signalCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	workCtx, workCancel := context.WithCancel(context.Background())
	defer workCancel()

	env, err := bootServe(workCtx, override)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura serve:", err)
		os.Exit(exitInfra)
	}
	defer env.close()
	ctx := workCtx

	v, _, _ := buildInfo()
	serveObs, err := startServeObservability(ctx, env.cfg, v, env.pool, net.Listen)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura serve observability:", err)
		os.Exit(exitInfra)
	}
	defer serveObs.shutdownRuntime()

	// Bind before starting any serving goroutine. A collision is a synchronous boot
	// failure; the joined lifecycle below owns every later Serve return.
	listener, err := bindServeListener(env.httpSrv.Addr, net.Listen)
	if err != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_ = serveObs.abort(shutCtx)
		cancel()
		fmt.Fprintln(os.Stderr, "aura serve:", err)
		os.Exit(exitInfra)
	}
	env.readiness.MarkListenerBound()
	slog.Info("aura serve: agui http server listening", "addr", env.cfg.AGUIBind)
	slog.Info("aura serve: private metrics server listening", "addr", serveObs.address())

	// The channels Registry (Telegram) + the setup wizard server (:9081) mount as
	// fail-soft siblings of the AG-UI gateway: a failed channel or a taken setup
	// port is logged, never aborts the daemon (T-13-09-DaemonAbort).
	startChannelSubsystems(ctx, env.channels, env.setupSrv)

	// Periodic sidecar sweep (M-06 part 2): launched on the work ctx (NOT signal-
	// derived) so a SIGTERM does not kill an in-flight filesystem walk mid-sweep; the
	// drain joins it. A disabled interval launches no goroutine. Stopped in drainShutdown.
	env.sweeper.Start(ctx)
	env.assetProcessingWorker.Start(ctx)
	// Crash-orphan reconciler (D-01d): launched on the work ctx like the sweeper so a
	// SIGTERM does not abort an in-flight anti-join mid-sweep; the drain joins it. A boot
	// one-shot fires immediately, then it ticks; a disabled/nil store launches no goroutine.
	env.reconciler.Start(ctx)
	env.deleteReconciler.Start(ctx)
	// Background-shell TTL reaper (MUSR-04): bounds runaway background jobs on the same
	// work ctx as the sweeper; the drain's BackgroundShells.Shutdown joins it. A disabled
	// TTL / nil registry launches no goroutine.
	if env.toolHandles.BackgroundShells != nil {
		env.toolHandles.BackgroundShells.StartReaper(ctx)
	}

	slog.Info("aura serve: scheduler daemon started", "tick", "running")
	// Start blocks until signalCtx is cancelled (SIGINT/SIGTERM) or it returns an
	// error; on a clean shutdown it returns nil after the in-flight tick joins its
	// workers. workCtx is STILL LIVE here, so an in-flight turn keeps running while
	// the bounded drain below gives it a grace window to finalize (O-06/AP-17).
	lifecycleErr := runServeComponentsWithMetrics(signalCtx, env.readiness, listener, env.httpSrv, env.scheduler, serveObs.metrics, func() {
		metricsCtx, metricsCancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := serveObs.stopMetrics(metricsCtx); err != nil {
			slog.Warn("aura serve: private metrics shutdown", "err", err)
		}
		metricsCancel()
		shutdownBackgroundShells(env)

		// Bounded in-flight turn drain (O-06/AP-17): the signal stopped NEW work, but a
		// turn already mid-stream must reach its terminal frame rather than being hard-
		// killed. drainShutdown stops the poller + HTTP listener (no new turns) and joins
		// the in-flight turns under the grace window; only AFTER the grace does workCancel
		// fire as the final backstop. drainWithGrace never blocks past the grace on an
		// overrunning turn — the backstop unwedges it. workCtx parents the per-turn work,
		// so it stays valid for the whole drain (it is NOT signal-derived).
		grace := time.Duration(env.cfg.ServeShutdownGraceSec) * time.Second
		res := drainWithGrace(func() { drainShutdown(workCtx, env) }, grace)
		if res == drainResultTimedOut {
			slog.Warn("aura serve: in-flight turn drain exceeded grace, forcing shutdown", "grace", grace)
		}
		// Final backstop: cancel the work ctx so any straggler past the grace unwinds, and
		// env.close() (deferred at the top) releases the pool the channels held.
		workCancel()
	})

	if lifecycleErr != nil {
		serveObs.shutdownRuntime()
		fmt.Fprintln(os.Stderr, "aura serve:", lifecycleErr)
		os.Exit(exitInfra)
	}
	slog.Info("aura serve: graceful shutdown complete")
}

// bootServe builds the daemon over the shared composition root (D-15). It reuses
// bootChatEnv (pool + MCP mounts + registry + Runner) and adds the cron Store + the
// Dispatcher wired with the live handlers, Notifier, and quiet-hours predicate. A boot
// failure returns the error so runServe can exit cleanly without a leaked pool/MCP.
func bootServe(ctx context.Context, channelOverride func(name string) (enabled, ok bool)) (*serveEnv, error) {
	chat, err := bootServeChatEnv(ctx)
	if err != nil {
		return nil, err
	}
	readinessState := readiness.NewSnapshot(readiness.Config{
		MigrationCompatible: true,
		SchedulerEnabled:    true,
	})

	// VERIF-7 / D-18: wire the background-shell poll/kill admin-capability seam to the live
	// identity store now that it exists. buildBaseRegistryWithHandles retained the ShellPoll/
	// ShellKill pointers with a nil .Caps (owner-only fail-closed on the pool-free manifest
	// paths); at serve boot chat.identity (*identity.Store) is available, so setting .Caps
	// makes the admin cross-session recovery exemption reachable (adminShellCapability =
	// governance.write, which the seeded local admin holds via 0026) while a foreign non-admin
	// poll/kill stays denied. chat.identity satisfies the tools capabilityChecker seam via
	// HasCapability; the nil-pointer guard keeps a store-less path owner-only.
	if chat.toolHandles.ShellPoll != nil {
		chat.toolHandles.ShellPoll.Caps = chat.identity
	}
	if chat.toolHandles.ShellKill != nil {
		chat.toolHandles.ShellKill.Caps = chat.identity
	}

	store := cron.New(chat.pool)
	objectStore, err := buildObjectStore(ctx, chat.cfg)
	if err != nil {
		chat.close()
		return nil, fmt.Errorf("build object store: %w", err)
	}
	chat.assets = buildAssetService(chat.cfg, chat.pool, objectStore)
	ownerExports := agui.NewObjectStoreExportDestination(objectStore, chat.cfg.ObjectStoreBucket)
	// Wire send_file's ingest seam to the live asset service now that chat.assets exists
	// (VERIF-7 post-construction, mirroring the .Caps set above): an authenticated
	// channel-driven delivery ingests into an owned Garage asset (WEBART-01). The nil guard
	// keeps the static/manifest/CLI paths path-only (D-02).
	if chat.toolHandles.SendFile != nil {
		chat.toolHandles.SendFile.Assets = sendFileAssetAdapter{svc: chat.assets}
	}
	// share_service_wiring.go: wires WEBSHARE-02/03 into HTTP, the D-15 delete cascade, and
	// the share_expiry_sweep cron handler below — all three were previously unwired.
	shareSvc, shareAPI := buildShareService(chat, objectStore)
	chat.shareSvc = shareSvc
	chat.run.SetShareRevoker(shareSvc)
	// Build the channels Registry FIRST (Phase 20 boot reorder): buildDispatch wires
	// the late-bound *channels.Registry pointer as the cron.ChannelDeliverer, so the
	// Registry must exist before dispatch is assembled. bootChannelsAndSetup needs only
	// chat + override (both available here) — the per-channel Deliverer capability is
	// resolved at delivery, not at build, so the late-bound pointer is sufficient.
	reg, setupSrv := bootChannelsAndSetup(ctx, chat, channelOverride)
	dispatch := buildDispatch(chat, store, reg, ownerExports)
	scheduler := cron.NewScheduler(chat.pool, store, cron.SchedulerConfig{
		Dispatch:  dispatch,
		Readiness: readinessState,
		// Consult each kind's ReschedulesOnRecovery at boot catch-up (M-g): a handler
		// that does not reschedule on recovery (the periodic sweeps — skill_ttl_sweep,
		// retention, purge) is never auto-re-fired for a missed window — only its
		// cadence resumes. Reminder/agent_job/backup DO re-fire once (fix-plan 1.2).
		ReschedulesOnRecovery: dispatch.ReschedulesOnRecovery,
	})
	// Seed the daily snippet TTL sweep (D-16) idempotently — only when no
	// skill_ttl_sweep task already exists. The 0010-widened kind CHECK admits the row.
	if err := seedSkillTTLSweep(ctx, store); err != nil {
		slog.Warn("aura serve: seed skill TTL sweep", "err", err)
	}
	// Seed the D-27 grace-window identity purge sweep (VERIF-3/HI-01) idempotently — only
	// when no identity_purge task already exists. The 0033-widened kind CHECK admits the row.
	if err := seedIdentityPurgeSweep(ctx, store); err != nil {
		slog.Warn("aura serve: seed identity purge sweep", "err", err)
	}
	// Seed the D-08 idle-suspend sandbox reap sweep (plan 37-05) idempotently — only when no
	// sandbox_reap task already exists. The 0034-widened kind CHECK admits the row; the cadence
	// tracks AURA_SANDBOX_IDLE_TTL_SEC. Safe even when the router is nil (disabled no-op reaper).
	if err := seedSandboxReapSweep(ctx, store, chat.cfg.Sandbox.IdleTTLSec); err != nil {
		slog.Warn("aura serve: seed sandbox reap sweep", "err", err)
	}
	// Seed the D-15/OQ3 share-link expiry sweep (0040's kind CHECK already admits it).
	if err := seedShareExpirySweep(ctx, store); err != nil {
		slog.Warn("aura serve: seed share expiry sweep", "err", err)
	}
	if err := seedRetentionSweep(ctx, store); err != nil {
		slog.Warn("aura serve: seed retention sweep", "err", err)
	}
	// Seed the memory embedding backfill (0091's kind CHECK admits it). Until this sweep
	// existed a fact written while the embedding sidecar was absent or slow kept no vector
	// for good, and semantic recall answered on a partially-embedded corpus.
	if err := seedMemoryEmbedBackfillSweep(ctx, store); err != nil {
		slog.Warn("aura serve: seed memory embed backfill sweep", "err", err)
	}
	// The AG-UI gateway (Slice 8b) construction + its independent SetX wiring lives in
	// wireAGUIServer (serve_agui.go, refactor-on-touch split when the SSE-heartbeat knob
	// landed a new ServerConfig field — CLAUDE.md 600-LOC ceiling). The auth-dependent
	// wiring (onboarding/bootstrap/password-reset) stays below, once auth/authulaProvider
	// exist.
	aguiServer, runRegistry := wireAGUIServer(chat, store, scheduler, readinessState, ownerExports, shareAPI, objectStore)
	// The embedded operator SPA (internal/webui) mounts additively at "/" on the
	// SAME loopback server: newServeHandler is a parent mux that keeps the AG-UI
	// routes authoritative and falls everything else through to the static shell
	// (FND-02). A webui embed failure is fatal at boot — a committed dist makes it
	// unreachable, but a half-wired host must not start.
	//
	// WEB-03: the parent mux is wrapped in the Authula web-auth boundary. buildAuthDeps
	// constructs the Authula provider and wires its session validator into RequireAuth.
	// GuardWebBind below keeps non-loopback binds behind Authula auth or an explicit
	// trust-proxy deployment.
	auth, authulaProvider, err := buildAuthDeps(ctx, chat)
	if err != nil {
		chat.close()
		return nil, fmt.Errorf("build auth deps: %w", err)
	}
	onboardingAuthulaProvider := authulaProvider
	if onboardingAuthulaProvider == nil && authulaProvisioningConfigured(chat.cfg) {
		var authulaErr error
		onboardingAuthulaProvider, _, authulaErr = buildAuthulaProvider(ctx, chat, auth.LocalIdentityID)
		if authulaErr != nil {
			slog.Warn("aura serve: onboarding provisioning authula unavailable", "err", authulaErr)
		}
	}
	// Wire the Phase-28 onboarding wizard + provisioning saga (ONBD-01/02). Built
	// best-effort over the daemon's existing seams (the identity Store for the capability
	// picker + the aura-leg write, the Authula provider's CoreServices for Leg B, the
	// recovery adapter, the Telegram Store for Leg C mint/status/compensation, and the
	// memory-backed profile store the Amendment #95 seed form writes through). A missing
	// provisioning piece, including bot username, fails before writes and MUST NOT abort boot.
	// The mounts live in serve_webui.go: RequireCapability(identity.create) on start +
	// provision ONLY; status, the seed-form submit (POST /api/onboarding/profile) and
	// telegram-status are self-scoped and carry RequireAuth alone.
	aguiServer.SetOnboardingService(buildOnboardingService(ctx, chat, onboardingAuthulaProvider))
	aguiServer.SetOnboardingStatusSource(newOnboardingStatusAdapter(chat))
	wireBootstrapService(aguiServer, chat.pool, authulaProvider)
	var resetTokenPepper []byte
	if authulaProvider != nil {
		resetTokenPepper, err = agui.DeriveResetTokenPepper(chat.cfg.AuthulaSecret)
		if err != nil {
			closeAuthulaProviders(authulaProvider, onboardingAuthulaProvider)
			chat.close()
			return nil, fmt.Errorf("derive password-reset token pepper: %w", err)
		}
	}
	wirePasswordResetService(aguiServer, chat.pool, reg, authulaProvider, resetTokenPepper)
	serveHandler, err := newServeHandler(aguiServer.Mux(), auth, authulaProvider)
	if err != nil {
		closeAuthulaProviders(authulaProvider, onboardingAuthulaProvider)
		chat.close()
		return nil, fmt.Errorf("build serve handler: %w", err)
	}
	// WEB-02 fail-fast: refuse to start a non-loopback bind that has no web-auth
	// credential. The returned error flows to runServe, which prints "aura serve: <err>"
	// and exits exitInfra (no second exit path). Loopback boots unchanged.
	if err := config.GuardWebBind(chat.cfg.AGUIBind, auth.SecretConfigured, chat.cfg.WebTrustProxy); err != nil {
		closeAuthulaProviders(authulaProvider, onboardingAuthulaProvider)
		chat.close()
		return nil, err
	}
	httpSrv := &http.Server{
		Addr:              chat.cfg.AGUIBind,
		Handler:           serveHandler,
		ReadHeaderTimeout: aguiReadHeaderTimeout,
	}

	// Periodic sidecar sweep (M-06 part 2): the boot ScanOrphans is one-shot, so a
	// long-running daemon would let aged/orphaned sidecars accumulate between reboots.
	// This worker re-runs the SAME boot sweep (orphan dirs + tmp TTL + audit size WARN)
	// on AURA_RUN_DIR_SWEEP_INTERVAL_SEC; <=0 disables it (the boot sweep still runs).
	sweeper := conversations.NewRunDirSweeper(chat.pool, conversations.ScanParams{
		RunDir:             chat.cfg.RunDir,
		WarnThresholdBytes: int64(chat.cfg.RunDirWarnThresholdBytes),
	}, time.Duration(chat.cfg.RunDirSweepIntervalSec)*time.Second)
	assetProcessingWorker := newRuntimeAssetProcessingWorker(chat.cfg, chat.pool, chat.assets, chat.cfg.AssetProcessingConcurrent)

	// Crash-orphan reconciler (D-01d): closes a start∧¬end reservation left by a crash
	// between reserve and Execute by APPENDING a terminal indeterminate `end` fact — it
	// NEVER re-invokes a mutating orphan (the side effect may already have fired). It runs
	// over the SAME ledger store the gateway reserves through, with the resolved
	// maxToolExecWindow so effectiveGrace strictly exceeds any single run's tool-execution
	// lifetime (the WARNING-4 collision-impossibility invariant). Under dev/local_trusted
	// no reservations are written, so it is a harmless no-op; it stays lifecycle-clean
	// either way (Start at boot, Stop in drainShutdown).
	reconciler := gateway.NewReconciler(chat.toolInvocations, reconcileTickInterval, resolveMaxToolExecWindow())

	// The channels Registry + the setup-wizard server (:9081) were built above (the
	// Phase 20 boot reorder) so the Registry could be wired into buildDispatch as the
	// cron.ChannelDeliverer; runServe StartAll/StopAll them (Phase 13, UX-02/03).
	return &serveEnv{
		chatEnv:                   chat,
		store:                     store,
		scheduler:                 scheduler,
		httpSrv:                   httpSrv,
		readiness:                 readinessState,
		channels:                  reg,
		setupSrv:                  setupSrv,
		sweeper:                   sweeper,
		assetProcessingWorker:     assetProcessingWorker,
		reconciler:                reconciler,
		authulaProvider:           authulaProvider,
		onboardingAuthulaProvider: onboardingAuthulaProvider,
		runRegistry:               runRegistry,
	}, nil
}
