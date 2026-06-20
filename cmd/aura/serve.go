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
	"errors"
	"fmt"
	"log/slog"
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
	"github.com/chetto1983/aura/internal/cron/handlers"
	"github.com/chetto1983/aura/internal/knowledge"
	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/web"
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

const serveUsage = "usage: aura serve [--no-telegram | --only=cli]"

// serveEnv is the booted daemon: the shared chat composition root plus the cron
// Store + Scheduler the tick loop runs. close() reverse-releases everything the boot
// acquired (MCP closers + pool) via the embedded chatEnv.
type serveEnv struct {
	*chatEnv
	store     *cron.Store
	scheduler *cron.Scheduler
	httpSrv   *http.Server // the AG-UI gateway (Slice 8b), mounted alongside the tick loop

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

	// authulaProvider is the embedded Authula web-auth framework, non-nil only when
	// AURA_WEB_AUTH_PROVIDER=authula (Option A2). close() releases its plugins + core
	// systems (the expiry-cleanup workers) so shutdown is goleak-clean; nil otherwise.
	authulaProvider *webauth.Provider
}

// close reverse-releases the daemon-owned resources the embedded chatEnv does not:
// the Authula provider's cleanup workers (when wired), THEN the shared chatEnv close
// (pool + MCP closers). It is the single teardown the deferred env.close() in runServe
// drives.
func (e *serveEnv) close() {
	if e.authulaProvider != nil {
		if err := e.authulaProvider.Close(); err != nil {
			slog.Warn("aura serve: authula provider close", "err", err)
		}
	}
	e.chatEnv.close()
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
	shutdownObs, err := obs.Init(ctx, obs.Config{
		Service:      "aura",
		Version:      v,
		OtelExporter: env.cfg.OtelExporter,
		OtelEndpoint: env.cfg.OtelEndpoint,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura serve observability:", err)
		os.Exit(exitInfra)
	}
	defer func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := shutdownObs(shutCtx); err != nil {
			slog.Warn("aura serve: observability shutdown", "err", err)
		}
	}()

	// The AG-UI HTTP server runs in its own goroutine alongside the scheduler tick
	// loop. ListenAndServe returning anything other than ErrServerClosed (the clean
	// Shutdown signal) is logged but NEVER exits the process — the scheduler keeps the
	// daemon useful even if the gateway port is taken (fail-soft, Pitfall 6).
	slog.Info("aura serve: agui http server listening", "addr", env.cfg.AGUIBind)
	go func() {
		if err := env.httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			slog.Error("aura serve: agui http server stopped", "err", err)
		}
	}()

	// The channels Registry (Telegram) + the setup wizard server (:9081) mount as
	// fail-soft siblings of the AG-UI gateway: a failed channel or a taken setup
	// port is logged, never aborts the daemon (T-13-09-DaemonAbort).
	startChannelSubsystems(ctx, env.channels, env.setupSrv)

	// Periodic sidecar sweep (M-06 part 2): launched on the work ctx (NOT signal-
	// derived) so a SIGTERM does not kill an in-flight filesystem walk mid-sweep; the
	// drain joins it. A disabled interval launches no goroutine. Stopped in drainShutdown.
	env.sweeper.Start(ctx)

	slog.Info("aura serve: scheduler daemon started", "tick", "running")
	// Start blocks until signalCtx is cancelled (SIGINT/SIGTERM) or it returns an
	// error; on a clean shutdown it returns nil after the in-flight tick joins its
	// workers. workCtx is STILL LIVE here, so an in-flight turn keeps running while
	// the bounded drain below gives it a grace window to finalize (O-06/AP-17).
	schedErr := env.scheduler.Start(signalCtx)

	if env.toolHandles.BackgroundShells != nil {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := env.toolHandles.BackgroundShells.Shutdown(shutCtx); err != nil {
			slog.Warn("aura serve: background shell shutdown", "err", err)
		}
		cancel()
	}

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

	if schedErr != nil {
		fmt.Fprintln(os.Stderr, "aura serve: scheduler stopped:", schedErr)
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

	store := cron.New(chat.pool)
	objectStore, err := buildObjectStore(ctx, chat.cfg)
	if err != nil {
		chat.close()
		return nil, fmt.Errorf("build object store: %w", err)
	}
	chat.assets = buildAssetService(chat.cfg, chat.pool, objectStore)
	// Build the channels Registry FIRST (Phase 20 boot reorder): buildDispatch wires
	// the late-bound *channels.Registry pointer as the cron.ChannelDeliverer, so the
	// Registry must exist before dispatch is assembled. bootChannelsAndSetup needs only
	// chat + override (both available here) — the per-channel Deliverer capability is
	// resolved at delivery, not at build, so the late-bound pointer is sufficient.
	reg, setupSrv := bootChannelsAndSetup(ctx, chat, channelOverride)
	dispatch := buildDispatch(chat, store, reg)
	scheduler := cron.NewScheduler(chat.pool, store, cron.SchedulerConfig{
		Dispatch: dispatch,
		// Consult each kind's ReschedulesOnRecovery at boot catch-up (M-g): a handler
		// that does not reschedule on recovery (reminder, skill_ttl_sweep) is never
		// auto-re-fired for a missed window — only its cadence resumes.
		ReschedulesOnRecovery: dispatch.ReschedulesOnRecovery,
	})
	// Seed the daily snippet TTL sweep (D-16) idempotently — only when no
	// skill_ttl_sweep task already exists. The 0010-widened kind CHECK admits the row.
	if err := seedSkillTTLSweep(ctx, store); err != nil {
		slog.Warn("aura serve: seed skill TTL sweep", "err", err)
	}

	// The AG-UI gateway (Slice 8b) reuses the already-composed Runner + conversations
	// store; it mounts on the same daemon and shares the graceful ctx-cancel drain
	// (Assumption A3). The bind may now be non-loopback (WEB-02/D-06 lifted the
	// hardcoded-loopback restriction); config.GuardWebBind (called below before httpSrv
	// is built) refuses a non-loopback AURA_AGUI_BIND unless AURA_WEB_AUTH_SECRET or
	// AURA_WEB_TRUST_PROXY is set, so the auth boundary — not a hardcoded bind — is the
	// compensating control.
	aguiServer := agui.NewServer(chat.run, chat.conv, agui.ServerConfig{
		CORSPermissive: chat.cfg.AGUICORSPermissive,
		BufferCap:      chat.cfg.AGUIBufferCap,
		HealthCheck: func(ctx context.Context) error {
			if err := chat.pool.Ping(ctx); err != nil {
				return fmt.Errorf("db ping: %w", err)
			}
			return nil
		},
		HealthDetails: func() map[string]any {
			// WEB-04/D-07: the read-only health panel reads bind + build from this
			// EXISTING /healthz body — no new backend endpoint. Both are non-secret
			// (a bind address and a build version, not a DSN/credential).
			buildVersion, _, _ := buildInfo()
			details := map[string]any{
				"bind_address":  chat.cfg.AGUIBind,
				"build_version": buildVersion,
			}
			last := scheduler.LastTick()
			if last.IsZero() {
				details["scheduler_last_tick"] = ""
			} else {
				details["scheduler_last_tick"] = last.Format(time.RFC3339)
			}
			return details
		},
		// /readyz reflects the daemon's REQUIRED backends (O-05/AP-14): Postgres
		// (the open pool) and Neo4j (a native-driver connectivity dial — the daemon
		// holds no long-lived graph client, the MCP subprocess is conditional). When
		// any required dep is unreachable /readyz answers 503 so an orchestrator
		// stops routing to this instance; /healthz stays a cheap PG-only liveness.
		ReadinessProbes: serveReadinessProbes(chat),
	})
	aguiServer.SetAssetService(chat.assets)
	// Wire the cross-thread HITL approval read (APRV-01 / D-04). Without this the
	// GET /api/approvals poll answers 503 and the whole approval center is dead in
	// production — SetApprovalStore was only ever called in tests, so the live daemon
	// shipped with s.approvals == nil. chat.pause is the same askuser.Store the Runner
	// resumes through, so the badge/list read the identical pending set.
	aguiServer.SetApprovalStore(chat.pause)
	// Wire the DISP-05/D-09 image-proxy fetcher: a fresh web.Client reusing the SAME
	// SSRF-hardened transport web_search/web_fetch use (hostname blocklist → DNS-pin →
	// classify → image content-type allowlist + size cap). Without this the
	// GET /api/image-proxy route answers 503 and the cockpit's web_result thumbnails/
	// favicons never load. It mounts behind the RequireAuth whole-origin gate (the
	// parent mux below), so it is never an open relay.
	aguiServer.SetImageProxy(web.NewClient(chat.cfg))
	// Wire the Phase-27 GRAPH-01 read-only graph explorer. Per RESEARCH A7/Open-Q2 the
	// serve daemon opens ONE boot-time knowledge.Client (the mcp-neo4j-cypher subprocess)
	// for the gateway lifetime — distinct from the ReasoningLearning-gated client in
	// chat.go, which is conditional. Best-effort: a missing binary or a down Neo4j leaves
	// the two /api/graph/* routes at 503 (SetGraphView never called) and MUST NOT abort
	// serve boot (a graph-explorer outage is not a daemon outage). On success the client's
	// Close joins the SAME reverse-close teardown (chatEnv.mcpClosers) that drains the
	// other MCP subprocesses at shutdown.
	if gclient, gerr := knowledge.Open(ctx, &chat.cfg.Neo4j); gerr != nil {
		slog.Warn("aura serve: graph explorer unavailable", "err", gerr)
	} else {
		chat.mcpClosers = append(chat.mcpClosers, gclient.Close)
		aguiServer.SetGraphView(knowledge.NewGraphView(gclient))
	}
	// Wire the Phase-28 read-only governance boards (GOV-01/02/03): the MCP registry +
	// per-row live probe, the skills lifecycle + audit ledger, the scheduler tasks + run
	// history. Built best-effort over the existing seams (the managed MCP config, the
	// skills loader/stage-reader/audit store, the cron Store) — a provider that cannot be
	// constructed is left nil so its board answers 503, MUST NOT abort boot (the
	// SetGraphView best-effort precedent). The reads inherit RequireAuth from the parent
	// mux; no capability gate (read-only).
	aguiServer.SetGovernanceProviders(buildGovernanceProviders(chat.cfg, chat.pool, store))
	// The embedded operator SPA (internal/webui) mounts additively at "/" on the
	// SAME loopback server: newServeHandler is a parent mux that keeps the AG-UI
	// routes authoritative and falls everything else through to the static shell
	// (FND-02). A webui embed failure is fatal at boot — a committed dist makes it
	// unreachable, but a half-wired host must not start.
	//
	// WEB-03: the parent mux is wrapped in the in-binary web-auth boundary. buildAuthDeps
	// derives the HMAC signing key from AURA_WEB_AUTH_SECRET, binds the session to the
	// seeded `local` identity, and sets SecretConfigured from the non-empty secret. When
	// no secret is configured (loopback dev) RequireAuth is a no-op pass-through, so the
	// daemon serves unauthenticated exactly as before; GuardWebBind (below) guarantees an
	// unconfigured secret is only reachable on a loopback bind.
	auth, authulaProvider, err := buildAuthDeps(ctx, chat)
	if err != nil {
		chat.close()
		return nil, fmt.Errorf("build auth deps: %w", err)
	}
	// Wire the Phase-28 onboarding wizard + provisioning saga (ONBD-01/02). Built
	// best-effort over the daemon's existing seams (the identity Store for the capability
	// picker + the aura-leg write, the Authula provider's CoreServices for Leg B, the
	// Telegram Store for the Leg C mint + the status poll, the LLM extractor + profile
	// store for the interview). The Authula provider is nil on the passphrase path, so
	// provisioning is wired only when Authula is the auth provider; an interview-only
	// service still answers start/step, and provision answers a sanitized backend-
	// unavailable error. A missing piece leaves the routes degraded, MUST NOT abort boot
	// (the SetGovernanceProviders best-effort precedent). The mounts (RequireCapability on
	// start+provision, RequireAuth on step+telegram-status) live in serve_webui.go.
	aguiServer.SetOnboardingService(buildOnboardingService(ctx, chat, authulaProvider))
	serveHandler, err := newServeHandler(aguiServer.Mux(), auth, authulaProvider)
	if err != nil {
		_ = authulaProvider.Close()
		chat.close()
		return nil, fmt.Errorf("build serve handler: %w", err)
	}
	// WEB-02 fail-fast: refuse to start a non-loopback bind that has no web-auth
	// credential. The returned error flows to runServe, which prints "aura serve: <err>"
	// and exits exitInfra (no second exit path). Loopback boots unchanged.
	if err := config.GuardWebBind(chat.cfg.AGUIBind, chat.cfg.WebAuthSecret, chat.cfg.WebTrustProxy); err != nil {
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

	// The channels Registry + the setup-wizard server (:9081) were built above (the
	// Phase 20 boot reorder) so the Registry could be wired into buildDispatch as the
	// cron.ChannelDeliverer; runServe StartAll/StopAll them (Phase 13, UX-02/03).
	return &serveEnv{
		chatEnv:         chat,
		store:           store,
		scheduler:       scheduler,
		httpSrv:         httpSrv,
		channels:        reg,
		setupSrv:        setupSrv,
		sweeper:         sweeper,
		authulaProvider: authulaProvider,
	}, nil
}

// serveReadinessProbes builds the /readyz probe set over the daemon's required
// backends (O-05/AP-14): Postgres (the open pool's Ping) and Neo4j (a bounded
// native-driver connectivity dial). The daemon holds no long-lived graph client —
// the mcp-neo4j-cypher subprocess is opened only behind the reasoning-learner gate
// — so the Neo4j probe dials from cfg.Neo4j each call; the per-probe timeout in the
// agui handler bounds it. A probe whose dependency handle is absent (a nil pool)
// is omitted rather than reported as a false failure.
func serveReadinessProbes(chat *chatEnv) []agui.ReadinessProbe {
	probes := make([]agui.ReadinessProbe, 0, 2)
	if chat.pool != nil {
		probes = append(probes, agui.ReadinessProbe{
			Name:  "postgres",
			Check: func(ctx context.Context) error { return chat.pool.Ping(ctx) },
		})
	}
	probes = append(probes, agui.ReadinessProbe{
		Name:  "neo4j",
		Check: func(ctx context.Context) error { return knowledge.VerifyConnectivity(ctx, &chat.cfg.Neo4j) },
	})
	return probes
}

// seedSkillTTLSweep idempotently seeds the daily snippet TTL-sweep task (D-16): it
// scans the active tasks for an existing skill_ttl_sweep and only inserts one if
// absent. The schedule is a daily 03:00 cron (a quiet hour), TZ Europe/Rome (the
// scheduler default). A seed failure is non-fatal (logged by the caller) — the daemon
// still runs; the operator can re-seed by restarting. The INSERT succeeds against the
// 0010-widened scheduler_tasks.kind CHECK (A2 landmine closed).
func seedSkillTTLSweep(ctx context.Context, store *cron.Store) error {
	tasks, err := store.ListActiveTasks(ctx)
	if err != nil {
		return fmt.Errorf("list active tasks: %w", err)
	}
	for _, t := range tasks {
		if t.Kind == cron.KindSkillTTLSweep {
			return nil // already seeded — idempotent
		}
	}
	spec, err := cron.ParseSchedule(string(cron.KindCron), "0 3 * * *", 0, time.Time{}, "Europe/Rome")
	if err != nil {
		return fmt.Errorf("parse daily schedule: %w", err)
	}
	next, err := cron.NextRunAt(spec, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("compute next run: %w", err)
	}
	if _, err := store.CreateTask(ctx, cron.CreateTaskParams{
		Kind:      cron.KindSkillTTLSweep,
		Spec:      spec,
		NextRunAt: next,
	}); err != nil {
		return fmt.Errorf("create skill_ttl_sweep task: %w", err)
	}
	slog.Info("aura serve: seeded daily skill TTL sweep", "schedule", "0 3 * * * Europe/Rome")
	return nil
}

// var _ asserts at the composition root that *channels.Registry satisfies the
// cron-local ChannelDeliverer seam (via its 20-01 DeliverToIdentity method) — the
// assertion lives in cmd/aura, NOT in cron (cron must not import channels).
var _ cron.ChannelDeliverer = (*channels.Registry)(nil)

// buildDispatch assembles the cron Dispatcher from the live runtime (D-15/10-05): the
// real per-TaskKind handlers adapted onto the cron-local Handler seam, the composite
// Notifier over the mounted MCP self-send registry, the scheduler's quiet-hours
// predicate, and the late-bound *channels.Registry wired as the cron.ChannelDeliverer
// so a scheduled notification can prefer the origin channel (Phase 20 R4/R7). The
// agent_job handler runs the parent registry minus swarm_spawn (childRegistry, owned
// by the handlers package) over the live LLM client.
func buildDispatch(chat *chatEnv, store *cron.Store, reg *channels.Registry) *cron.Dispatch {
	agentDeps := handlers.AgentDeps{
		Client:     chat.client,
		LLM:        chat.cfg.LLM,
		Registry:   chat.reg,
		PreviewCap: chat.cfg.ToolPreviewCap,
		RunDir:     chat.cfg.RunDir,
		// Real artifact jobs measure 150-360s live; the 120s handler fallback starved
		// them mid-LLM-call (#53/D-42). Env-tunable: AURA_AGENT_JOB_MAX_DURATION_SEC.
		MaxDuration: time.Duration(chat.cfg.AgentJobMaxDurationSec) * time.Second,
	}
	real := map[cron.TaskKind]handlers.Handler{
		cron.KindReminder:       handlers.ReminderHandler{},
		cron.KindAgentJob:       handlers.AgentJobHandler{Deps: agentDeps},
		cron.KindBackupPostgres: handlers.BackupHandler{Variant: handlers.BackupPostgres},
		cron.KindBackupNeo4j:    handlers.BackupHandler{Variant: handlers.BackupNeo4j},
		cron.KindSkillTTLSweep: handlers.SkillTTLSweepHandler{
			Sweeper: &snippetSweeperAdapter{w: newSkillWriter(chat.cfg, chat.pool)},
			TTL:     time.Duration(chat.cfg.SkillSnippetTTLDays) * 24 * time.Hour,
		},
	}
	hmap := make(map[cron.TaskKind]cron.Handler, len(real))
	for kind, h := range real {
		hmap[kind] = handlerAdapter{inner: h}
	}

	notifier := cron.NewNotifier(newSelfSendResolver(chat.reg))
	quietScheduler := cron.NewScheduler(chat.pool, store, cron.SchedulerConfig{})
	return cron.NewDispatch(hmap, cron.DispatchDeps{
		Store:          store,
		Notifier:       notifier,
		AlertThreshold: scoring.Risky,
		// DuringQuietHours is a pure Now-based predicate over AURA_SCHEDULER_QUIET_HOURS
		// (D-23); it holds no tick state, so the live scheduler's method is the predicate.
		QuietHours:    quietScheduler.DuringQuietHours,
		QuietHoursEnd: quietScheduler.QuietHoursEnd,
		// Prefer the origin channel (Phase 20 R4/R7): the *channels.Registry satisfies
		// cron.ChannelDeliverer via DeliverToIdentity. The default-on kill-switch is
		// resolved once at config load (AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL, default true).
		ChannelDeliverer:    reg,
		PreferOriginChannel: chat.cfg.SchedulerPreferOriginChannel,
	})
}

// handlerAdapter bridges a handlers.Handler (the internal/cron/handlers impls, which
// import internal/agent/tools) onto the cron-local cron.Handler seam. The two
// interfaces are structurally identical but live in different packages to break the
// tools→cron→handlers import cycle (10-05 deviation #1); this adapter does the trivial
// Job/HandlerMeta field copy at the composition root that imports both.
type handlerAdapter struct {
	inner handlers.Handler
}

var _ cron.Handler = handlerAdapter{}

// Meta projects the handlers.HandlerMeta onto cron.HandlerMeta (same fields).
func (a handlerAdapter) Meta() cron.HandlerMeta {
	m := a.inner.Meta()
	return cron.HandlerMeta{
		Kind:                  cron.TaskKind(m.Kind),
		MaxDuration:           m.MaxDuration,
		ReschedulesOnRecovery: m.ReschedulesOnRecovery,
	}
}

// Run projects the cron.Job onto handlers.Job and delegates to the real handler.
func (a handlerAdapter) Run(ctx context.Context, job cron.Job) (string, error) {
	return a.inner.Run(ctx, handlers.Job{
		Payload:     job.Payload,
		StepBudget:  job.StepBudget,
		RunID:       job.RunID,
		MissedSince: job.MissedSince,
	})
}
