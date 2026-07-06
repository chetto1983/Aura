// serve_dispatch.go assembles the cron Dispatcher from the live serve runtime (D-15/
// 10-05): the real per-TaskKind handlers adapted onto the cron-local Handler seam, the
// composite Notifier, the quiet-hours predicate, and the late-bound *channels.Registry as
// the cron.ChannelDeliverer. It is split from serve.go (the boot/lifecycle) to keep each
// file focused and under the 600-LOC ceiling; the handlerAdapter bridge breaks the
// tools→cron→handlers import cycle at the composition root that imports both.
package main

import (
	"context"
	"log/slog"
	"time"

	"github.com/moby/moby/client"

	"github.com/chetto1983/aura/internal/channels"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/cron/handlers"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/scoring"
)

// nanoCPUsPerCPU converts a whole-CPU count (cfg.Sandbox.CPULimit) to moby's NanoCPUs cgroup
// unit (1 CPU = 1e9 nano-CPUs, D-14).
const nanoCPUsPerCPU = 1_000_000_000

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
	// Build the per-identity box router (Phase 37, plan 37-05). It is nil under a non-strict
	// profile or a Docker-unavailable host — a safe host-direct no-op everywhere — and is
	// retained on chat so plan 37-07 can wire it onto the box-capable tools.
	chat.sandboxRouter = buildSandboxRouter(chat)
	// A genuinely-nil SandboxReaper interface (not a typed-nil *SandboxRouter) yields the
	// handler's "disabled (no reaper)" no-op — exactly the identity_purge nil-Purger note.
	var sandboxReaper handlers.SandboxReaper
	if chat.sandboxRouter != nil {
		sandboxReaper = chat.sandboxRouter
	}

	agentDeps := handlers.AgentDeps{
		Client:     chat.client,
		LLM:        chat.cfg.LLM,
		Registry:   chat.reg,
		PreviewCap: chat.cfg.ToolPreviewCap,
		RunDir:     chat.cfg.RunDir,
		// Real artifact jobs measure 150-360s live; the 120s handler fallback starved
		// them mid-LLM-call (#53/D-42). Env-tunable: AURA_AGENT_JOB_MAX_DURATION_SEC.
		MaxDuration: time.Duration(chat.cfg.AgentJobMaxDurationSec) * time.Second,
		// The same policy PEP the interactive runner uses (GATE-01): a headless agent_job
		// has no responder, so a mutating GateRecommended call degrades to deny-with-guidance.
		Gateway: chat.gateway,
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
		// The D-27 grace-window purge sweep (VERIF-3/HI-01): the live *agui.Deprovisioner
		// satisfies handlers.IdentityPurger via PurgeExpired. A nil-pool build yields a
		// no-op Purger, so this registration is always safe.
		cron.KindIdentityPurge: handlers.IdentityPurgeHandler{Purger: buildDeprovisioner(chat)},
		// The D-08 idle-suspend reaper (plan 37-05): the live *usersandbox.SandboxRouter
		// satisfies handlers.SandboxReaper via SuspendIdle. A nil router (non-strict profile
		// or Docker-unavailable) leaves sandboxReaper a nil interface, so the handler is the
		// disabled no-op — always safe, exactly like the identity_purge registration.
		cron.KindSandboxReap: handlers.SandboxReapHandler{Reaper: sandboxReaper},
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
		Payload:              job.Payload,
		StepBudget:           job.StepBudget,
		RunID:                job.RunID,
		MissedSince:          job.MissedSince,
		OriginConversationID: job.OriginConversationID,
	})
}

// buildSandboxRouter constructs the per-identity box router at the serve composition root,
// sourced entirely from cfg.Sandbox (37-01). It returns NIL — a safe host-direct no-op
// everywhere (Route/Strict/SuspendIdle all nil-guard) — under a non-strict profile (the box is
// interposed ONLY under single_user_hardened / server_production, SC-4) or when a Docker client
// cannot be constructed (a Docker-unavailable host must never fail serve boot). The tools are
// NOT wired onto the router here — that is plan 37-07; this only builds the backend + router so
// the reaper can be registered and 37-07 has a router handle to interpose.
func buildSandboxRouter(chat *chatEnv) *usersandbox.SandboxRouter {
	if chat == nil || chat.cfg == nil || !chat.cfg.Profile.Strict() {
		return nil // non-strict: host-direct everywhere, no box runtime needed (SC-4)
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		slog.Warn("aura serve: docker client unavailable — sandbox routing disabled (host-direct)", "err", err)
		return nil
	}
	backend := usersandbox.NewDockerBackend(cli, chat.cfg.Sandbox.Image, limitsFrom(chat.cfg.Sandbox))
	return usersandbox.NewSandboxRouter(backend, chat.cfg.Profile, chat.cfg.Sandbox)
}

// limitsFrom maps the AURA_SANDBOX_* cgroup knobs (config) into usersandbox.Resources: the
// CPU-count cap becomes NanoCPUs, memory + pids pass through (D-14). It is the DockerBackend's
// construction-time fallback cap set; a routed Route always supplies a full spec via specFor.
func limitsFrom(sc config.SandboxConfig) usersandbox.Resources {
	return usersandbox.Resources{
		NanoCPUs:    int64(sc.CPULimit) * nanoCPUsPerCPU,
		MemoryBytes: sc.MemoryLimit,
		PidsLimit:   sc.PidsLimit,
	}
}
