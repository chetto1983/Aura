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
	"strings"
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
	// The per-identity box router (Phase 37) is built ONCE at composition (assembleChatEnv) and
	// retained on chat so the SAME instance backs both the box-capable tools (plan 37-07 wiring)
	// AND this reaper registration — never two routers with divergent box-handle maps. It is nil
	// under a non-strict profile or a Docker-unavailable host (a safe host-direct no-op everywhere).
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
		cron.KindIdentityPurge: handlers.NewIdentityPurgeHandler(buildDeprovisioner(chat)),
		// The D-08 idle-suspend reaper (plan 37-05): the live *usersandbox.SandboxRouter
		// satisfies handlers.SandboxReaper via SuspendIdle. A nil router (non-strict profile
		// or Docker-unavailable) leaves sandboxReaper a nil interface, so the handler is the
		// disabled no-op — always safe, exactly like the identity_purge registration.
		cron.KindSandboxReap: handlers.NewSandboxReapHandler(sandboxReaper),
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

// buildSandboxRouter constructs the per-identity box router at composition, sourced entirely from
// cfg.Sandbox (37-01). It returns NIL — a safe host-direct no-op everywhere (Route/Strict/
// SuspendIdle all nil-guard) — under a non-strict profile (the box is interposed ONLY under
// single_user_hardened / server_production, SC-4) or when a Docker client cannot be constructed (a
// Docker-unavailable host must never fail boot). Under a strict profile the box gets the always-on
// egress floor (SBX-04): newSandboxBackend wires usersandbox.WithEgress from cfg.Sandbox.EgressImage,
// so every routed box carries the DROP-RFC1918/metadata/bridge tenancy sidecar. Because the config
// default is NON-EMPTY (aura-egress:latest, SC#4) the floor is on-by-default, and box creation is
// fail-CLOSED when that image is unavailable (ensureImage pull-fail -> Resolve error -> Route
// routed=true,err -> the tool DENIES) — a strict-profile box never comes up un-floored.
func buildSandboxRouter(cfg *config.Config) *usersandbox.SandboxRouter {
	if cfg == nil || !cfg.Profile.Strict() {
		return nil // non-strict: host-direct everywhere, no box runtime needed (SC-4)
	}
	cli, err := client.New(client.FromEnv) // moby v0.4.1 New negotiates the API version by default (downgrades for older daemons).
	if err != nil {
		slog.Warn("aura: docker client unavailable — sandbox routing disabled (host-direct)", "err", err)
		return nil
	}
	return usersandbox.NewSandboxRouter(newSandboxBackend(cli, cfg), cfg.Profile, cfg.Sandbox)
}

// newSandboxBackend builds the production DockerBackend from cfg: the box image + cgroup caps, the
// per-identity materialize sources (skills / Agent.md / pyscripts, so a routed shell_exec finds a
// snippet the box materialized at /skills/<name>/... — D-10, plan 37-07), AND the always-on egress
// sidecar (SBX-04, D-07) via WithEgress sourced from cfg.Sandbox.EgressImage. It is split from
// buildSandboxRouter so the WithEgress wiring is regression-testable without a Docker daemon (the DockerBackend never
// dials at construction, so a nil client is safe): a docker-free cmd/aura test asserts EgressImage()
// echoes cfg.Sandbox.EgressImage. WithEgress("") is a guarded no-op, but the config default is
// non-empty so under a strict profile the floor is always wired.
func newSandboxBackend(cli *client.Client, cfg *config.Config) *usersandbox.DockerBackend {
	return usersandbox.NewDockerBackend(cli, cfg.Sandbox.Image, limitsFrom(cfg.Sandbox),
		usersandbox.WithMaterializeSources(sandboxMaterializeSources(cfg)),
		usersandbox.WithEgress(cfg.Sandbox.EgressImage))
}

// sandboxMaterializeSources resolves the per-identity host dirs docker-cp'd INTO the box at
// resolve (D-10): the skills export dir (landing at /skills — the SnippetSandboxPath root the
// routed shell_exec runs snippets from). Agent.md / pyscripts roots are per-identity and land with
// their own dedicated wiring; the skills export dir is the one the snippet-exec E2E depends on.
func sandboxMaterializeSources(cfg *config.Config) usersandbox.SourceResolver {
	exportDir := cfg.SkillExportDir
	return func(string) []usersandbox.MaterializeSource {
		if strings.TrimSpace(exportDir) == "" {
			return nil
		}
		return []usersandbox.MaterializeSource{{HostDir: exportDir, Dest: "/skills"}}
	}
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
