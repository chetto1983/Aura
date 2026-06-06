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
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/cron/handlers"
	"github.com/chetto1983/aura/internal/scoring"
)

// aguiShutdownTimeout bounds the graceful drain of in-flight SSE streams when the
// daemon shuts down (after the scheduler tick loop returns on ctx-cancel).
const aguiShutdownTimeout = 10 * time.Second

// aguiReadHeaderTimeout bounds the request-header read to defang slow-loris on the
// unauthenticated loopback endpoint (T-12-09).
const aguiReadHeaderTimeout = 10 * time.Second

const serveUsage = "usage: aura serve"

// serveEnv is the booted daemon: the shared chat composition root plus the cron
// Store + Scheduler the tick loop runs. close() reverse-releases everything the boot
// acquired (MCP closers + pool) via the embedded chatEnv.
type serveEnv struct {
	*chatEnv
	store     *cron.Store
	scheduler *cron.Scheduler
	httpSrv   *http.Server // the AG-UI gateway (Slice 8b), mounted alongside the tick loop
}

// runServe is the `aura serve` entry point: boot, start the tick loop, block on a
// termination signal, then shut down gracefully. A boot failure exits non-zero with a
// human-readable line (sysexits posture, the web/task CLI convention); the daemon
// itself never panics on a transient fault.
func runServe(args []string) {
	if len(args) > 0 {
		fmt.Fprintln(os.Stderr, serveUsage)
		os.Exit(exitUsage)
	}

	// signal.NotifyContext cancels the root ctx on SIGINT/SIGTERM; the scheduler's
	// Start returns on that cancel after the in-flight tick drains (graceful, D-15).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	env, err := bootServe(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aura serve:", err)
		os.Exit(exitInfra)
	}
	defer env.close()

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

	slog.Info("aura serve: scheduler daemon started", "tick", "running")
	// Start blocks until ctx is cancelled (signal) or it returns an error; on a clean
	// shutdown it returns nil after the in-flight tick joins its workers.
	schedErr := env.scheduler.Start(ctx)

	// Graceful HTTP drain on shutdown: bound the in-flight SSE streams so a slow client
	// cannot wedge shutdown forever. Runs on a fresh ctx (the root ctx is already
	// cancelled by the signal that returned Start).
	shutCtx, cancel := context.WithTimeout(context.Background(), aguiShutdownTimeout)
	defer cancel()
	if err := env.httpSrv.Shutdown(shutCtx); err != nil {
		slog.Warn("aura serve: agui http server shutdown", "err", err)
	}

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
func bootServe(ctx context.Context) (*serveEnv, error) {
	chat, err := bootChatEnv(ctx)
	if err != nil {
		return nil, err
	}

	store := cron.New(chat.pool)
	scheduler := cron.NewScheduler(chat.pool, store, cron.SchedulerConfig{
		Dispatch: buildDispatch(chat, store),
	})
	// Seed the daily snippet TTL sweep (D-16) idempotently — only when no
	// skill_ttl_sweep task already exists. The 0010-widened kind CHECK admits the row.
	if err := seedSkillTTLSweep(ctx, store); err != nil {
		slog.Warn("aura serve: seed skill TTL sweep", "err", err)
	}

	// The AG-UI gateway (Slice 8b) reuses the already-composed Runner + conversations
	// store; it mounts on the same daemon and shares the graceful ctx-cancel drain
	// (Assumption A3). The bind is hardcoded loopback via the config default — the
	// compensating control for the auth-deferred posture this phase (amendment #35).
	aguiServer := agui.NewServer(chat.run, chat.conv, agui.ServerConfig{
		CORSPermissive: chat.cfg.AGUICORSPermissive,
		BufferCap:      chat.cfg.AGUIBufferCap,
	})
	httpSrv := &http.Server{
		Addr:              chat.cfg.AGUIBind,
		Handler:           aguiServer.Mux(),
		ReadHeaderTimeout: aguiReadHeaderTimeout,
	}
	return &serveEnv{chatEnv: chat, store: store, scheduler: scheduler, httpSrv: httpSrv}, nil
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

// buildDispatch assembles the cron Dispatcher from the live runtime (D-15/10-05): the
// real per-TaskKind handlers adapted onto the cron-local Handler seam, the composite
// Notifier over the mounted MCP self-send registry, and the scheduler's quiet-hours
// predicate. The agent_job handler runs the parent registry minus swarm_spawn
// (childRegistry, owned by the handlers package) over the live LLM client.
func buildDispatch(chat *chatEnv, store *cron.Store) *cron.Dispatch {
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
	return cron.NewDispatch(hmap, cron.DispatchDeps{
		Store:          store,
		Notifier:       notifier,
		AlertThreshold: scoring.Risky,
		// DuringQuietHours is a pure Now-based predicate over AURA_SCHEDULER_QUIET_HOURS
		// (D-23); it holds no tick state, so the live scheduler's method is the predicate.
		QuietHours: cron.NewScheduler(chat.pool, store, cron.SchedulerConfig{}).DuringQuietHours,
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
