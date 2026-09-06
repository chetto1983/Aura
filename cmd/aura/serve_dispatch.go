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
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/moby/moby/client"

	"github.com/chetto1983/aura/internal/channels"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/cron/handlers"
	"github.com/chetto1983/aura/internal/obs"
	"github.com/chetto1983/aura/internal/sandbox/usersandbox"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/skills"
)

// nanoCPUsPerCPU converts a whole-CPU count (cfg.Sandbox.CPULimit) to moby's NanoCPUs cgroup
// unit (1 CPU = 1e9 nano-CPUs, D-14).
const nanoCPUsPerCPU = 1_000_000_000

// var _ asserts at the composition root that *channels.Registry satisfies the
// cron-local ChannelDeliverer seam (via its 20-01 DeliverToIdentity method) — the
// assertion lives in cmd/aura, NOT in cron (cron must not import channels).
var _ cron.ChannelDeliverer = (*channels.Registry)(nil)

// *channels.Registry ALSO satisfies the cron-local ApprovalChannel seam (via DeliverApproval,
// Amendment #92 revised): the approval-reminder sweep pushes the actionable on-channel HITL
// prompt through it. Assertion lives here, not in cron (cron must not import channels).
var _ cron.ApprovalChannel = (*channels.Registry)(nil)

// buildDispatch assembles the cron Dispatcher from the live runtime (D-15/10-05): the
// real per-TaskKind handlers adapted onto the cron-local Handler seam, the composite
// Notifier over the mounted MCP self-send registry, the scheduler's quiet-hours
// predicate, and the late-bound *channels.Registry wired as the cron.ChannelDeliverer
// so a scheduled notification can prefer the origin channel (Phase 20 R4/R7). The
// agent_job handler runs the parent registry minus swarm_spawn (childRegistry, owned
// by the handlers package) over the live LLM client.
func buildDispatch(chat *chatEnv, store *cron.Store, reg *channels.Registry, ownerExportSweepers ...handlers.OwnerExportSweeper) *cron.Dispatch {
	agentDeps := newCronAgentDeps(chat)
	var observabilityChecker handlers.ObservabilityChecker
	if chat.cfg.ObservabilityCheckEnabled {
		observabilityChecker = obs.NewSidecarChecker(
			&http.Client{Timeout: 5 * time.Second},
			"http://tempo:3200",
			"http://prometheus:9090",
			"http://grafana:3000",
		)
	}
	real := map[cron.TaskKind]handlers.Handler{
		cron.KindReminder:       handlers.ReminderHandler{},
		cron.KindAgentJob:       handlers.AgentJobHandler{Deps: agentDeps},
		cron.KindBackupPostgres: handlers.BackupHandler{Variant: handlers.BackupPostgres},
		cron.KindSkillTTLSweep: handlers.SkillTTLSweepHandler{
			Sweeper: &snippetSweeperAdapter{w: newSkillWriter(chat.cfg, chat.pool)},
			TTL:     time.Duration(chat.cfg.SkillSnippetTTLDays) * 24 * time.Hour,
		},
		// The D-27 grace-window purge sweep (VERIF-3/HI-01): the live *agui.Deprovisioner
		// satisfies handlers.IdentityPurger via PurgeExpired. A nil-pool build yields a
		// no-op Purger, so this registration is always safe.
		cron.KindIdentityPurge: handlers.NewIdentityPurgeHandler(buildDeprovisioner(chat)),
		// The D-08 idle-suspend reaper (plan 37-05): the live *usersandbox.SandboxRouter
		// satisfies handlers.SandboxReaper via SuspendIdle. chat.sandboxRouter is always non-nil
		// (buildSandboxRouter never returns nil), and SuspendIdle no-ops on a backend-less router,
		// so this registration is always safe — no typed-nil dance needed.
		cron.KindSandboxReap: handlers.NewSandboxReapHandler(chat.sandboxRouter),
		// The D-15/OQ3 share-link expiry GC sweep (37F-18 wiring): chat.shareSvc
		// (share_service_wiring.go) satisfies handlers.ShareExpirer via ExpireDue directly, no
		// adapter — always non-nil once serve boots, so this registration is always safe.
		cron.KindShareExpirySweep: handlers.NewShareExpiryHandler(chat.shareSvc),
		cron.KindRetentionSweep:   handlers.NewRetentionHandler(newRuntimeRetentionEngine(chat.cfg, chat.pool), ownerExportSweepers...),
		// The scheduled caller for the vectors (serve_memory_backfill.go): it visits every
		// identity's memory database and embeds the facts that have none. buildMemoryEmbedBackfill
		// returns a bare nil when ArcadeDB or the embedding sidecar is unconfigured, which
		// registers the disabled no-op sweep rather than a failing one.
		cron.KindMemoryEmbedBackfill: handlers.NewMemoryEmbedBackfillHandler(buildMemoryEmbedBackfill(chat)),
		// The scheduled caller for MENTIONS edges (serve_memory_backfill.go): it visits every
		// identity's memory database and rebuilds that identity's edges. Unlike the embed
		// sweep, linking needs no embedding sidecar — buildMemoryMentionLink returns a bare nil
		// only when ArcadeDB itself or the tenant secret is unconfigured.
		cron.KindMemoryMentionLink:  handlers.NewMemoryMentionLinkHandler(buildMemoryMentionLink(chat)),
		cron.KindObservabilityCheck: handlers.NewObservabilityCheckHandler(observabilityChecker),
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
		// The registry satisfies both exact-conversation and explicit Telegram delivery.
		ChannelDeliverer: reg,
		// On-channel HITL re-surface of a pending_approval task (Amendment #92 revised): the
		// sweep ensures a real scheduled_task_approval pause on the task's origin conversation
		// (reused from the model relay, else minted host-side via the Runner) and pushes the
		// actionable prompt to the origin channel. chat.pause + chat.run are always non-nil once
		// serve boots (assembleChatEnv), so the sweep is live here (kill-switch is the cadence env).
		ApprovalPauseEnsurer: approvalPauseEnsurer{pauses: chat.pause, minter: chat.run},
		ApprovalChannel:      reg,
		// Write the outcome back where it was asked for. The push above finds an operator
		// who is elsewhere; this answers the one who is still looking at the conversation
		// they scheduled from. Both are wanted — measured 2026-08-26, a reminder scheduled
		// in the cockpit was delivered to Telegram while the cockpit conversation ended at
		// "scheduled ✅" and never learned the outcome.
		ConversationRecorder: conversationRecorder{store: chat.conv},
	})
}

// conversationRecorder adapts *conversations.Store onto cron.ConversationRecorder, the
// same consumer-declared-interface idiom as approvalPauseEnsurer: cron declares the one
// method it needs and the composition root supplies it, so cron imports no conversation
// package. Seq 0 lets AppendTurn allocate the next sequence under the conversation's row
// lock, which is what an out-of-band append needs — the scheduler is not inside the
// turn loop that would otherwise be numbering these.
type conversationRecorder struct {
	store *conversations.Store
}

func (r conversationRecorder) AppendAssistantTurn(ctx context.Context, conversationID, text string) error {
	if r.store == nil {
		return nil
	}
	return r.store.AppendTurn(ctx, conversations.AppendTurnParams{
		ConversationID: conversationID,
		Role:           "assistant",
		Content:        text,
	})
}

func newCronAgentDeps(chat *chatEnv) handlers.AgentDeps {
	return handlers.AgentDeps{
		Client:     chat.client,
		LLM:        chat.cfg.LLM,
		Runtime:    chat.llmRuntime,
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
// cfg.Sandbox (37-01). It is built on EVERY profile and NEVER returns nil: the box is the agent's
// only filesystem and only shell now, so a profile no longer selects between contain and
// host-direct — it only selects the container runtime (specFor picks Runsc under
// server_production, Runc elsewhere). A Docker client that cannot be composed still yields a
// non-nil, backend-less router so every Route DENIES; returning nil here would be a host-fallback
// door reopened at the composition root.
//
// The box gets the always-on egress floor (SBX-04): newSandboxBackend wires usersandbox.WithEgress
// from cfg.Sandbox.EgressImage, so every box carries the DROP-RFC1918/metadata/bridge tenancy
// sidecar. Because the config default is NON-EMPTY (aura-egress:latest, SC#4) the floor is
// on-by-default, and box creation is fail-CLOSED when that image is unavailable (ensureImage
// pull-fail -> Resolve error -> Route err -> the tool DENIES).
func buildSandboxRouter(cfg *config.Config, pool *pgxpool.Pool) *usersandbox.SandboxRouter {
	if cfg == nil {
		// No config means no box spec. A denying router is the only honest answer; nil would be
		// read as "no containment needed".
		slog.Error("aura: sandbox router built without config — EVERY tool call will be denied")
		return usersandbox.NewSandboxRouter(nil, "", config.SandboxConfig{})
	}
	// client.New does NO network I/O — it parses DOCKER_HOST, builds an *http.Client and returns
	// (moby/moby/client@v0.5.0 client.go:191-259; API-version negotiation happens on the first
	// request). So this error means a malformed DOCKER_HOST or unreadable TLS material, never
	// "the daemon is down".
	cli, err := client.New(client.FromEnv)
	if err != nil {
		slog.Error("aura: docker client could not be composed — EVERY tool call will be denied", "err", err)
		return usersandbox.NewSandboxRouter(nil, cfg.Profile, cfg.Sandbox)
	}
	pingSandboxDaemon(cli)
	return usersandbox.NewSandboxRouter(newSandboxBackend(cli, cfg, pool), cfg.Profile, cfg.Sandbox)
}

// pingSandboxDaemon names a dead daemon ONCE, at boot, where an operator is already reading logs.
// Without it the first evidence is a moby dial error buried in the `detail` key of a
// sandbox_unavailable tool result — per tool call, in the model's context, nowhere near the
// operator. The router is returned either way: a dead daemon denies, it does not degrade.
func pingSandboxDaemon(cli *client.Client) {
	ctx, cancel := context.WithTimeout(context.Background(), sandboxPingTimeout)
	defer cancel()
	if _, err := cli.Ping(ctx, client.PingOptions{}); err != nil {
		slog.Error("aura: docker daemon unreachable — EVERY tool call will be denied until it is restored",
			"docker_host", cli.DaemonHost(), "err", err)
	}
}

// sandboxPingTimeout bounds the boot-time reachability check: long enough for a loaded local
// daemon, short enough that an unreachable socket cannot stall serve boot.
const sandboxPingTimeout = 5 * time.Second

// newSandboxBackend builds the production DockerBackend from cfg: the box image + cgroup caps, the
// per-identity materialize sources (the skills root, so a routed shell_exec finds a
// snippet the box materialized at /skills/<name>/... — D-10, plan 37-07), AND the always-on egress
// sidecar (SBX-04, D-07) via WithEgress sourced from cfg.Sandbox.EgressImage. It is split from
// buildSandboxRouter so the WithEgress wiring is regression-testable without a Docker daemon (the DockerBackend never
// dials at construction, so a nil client is safe): a docker-free cmd/aura test asserts EgressImage()
// echoes cfg.Sandbox.EgressImage. WithEgress("") is a guarded no-op, but the config default is
// non-empty so under a strict profile the floor is always wired.
func newSandboxBackend(cli *client.Client, cfg *config.Config, pool *pgxpool.Pool) *usersandbox.DockerBackend {
	return usersandbox.NewDockerBackend(cli, cfg.Sandbox.Image, limitsFrom(cfg.Sandbox),
		usersandbox.WithMaterializeSources(sandboxMaterializeSources(cfg, newSharedSkillReader(cfg, pool))),
		usersandbox.WithEgress(cfg.Sandbox.EgressImage))
}

// sandboxMaterializeSources resolves the host dirs docker-cp'd INTO the box at resolve
// (D-10): the skill export trees, landing at /skills — the SnippetSandboxPath root the routed
// shell_exec runs snippets from.
//
// There are THREE kinds of them since amendment #214, and their order is the whole point.
// The skills SHARED with this identity come first, their own export second, the deployment's
// last, so that on a name collision the operator's skill is the one the box ends up running
// and a person's own beats anything handed to them — the same precedence
// skills.Roots.LoaderRoots gives the model's context, because a box that runs a different
// `deploy` than the one the model was briefed on is worse than either alone. Sources sharing
// a Dest merge in order, and MaterializeIn clears each dest once before any of them lands.
//
// The identity parameter is no longer ignored, and that comment had to go with the reason
// for it: the export dir is per identity now, so B's box holds B's skills and the house's,
// never A's — and, since criterion 5, exactly those of A's that A has actually shared.
func sandboxMaterializeSources(cfg *config.Config, shared *skills.SharedReader) usersandbox.SourceResolver {
	layout := skillLayout(cfg)
	return func(ctx context.Context, identityID string) ([]usersandbox.MaterializeSource, error) {
		roots, err := layout.For(identityID)
		if err != nil {
			// Fail to the house library rather than to nothing: a box with no /skills is a box
			// where every stored snippet has silently stopped existing, and the model is told
			// to run a path that is not there.
			slog.Warn("sandbox materialize: identity cannot name an export root, using the deployment export",
				"identity_id", identityID, "err", err)
			roots = skills.Roots{Export: layout.Export}
		}
		out := sharedSkillSources(ctx, shared, identityID)
		// An unscoped caller resolves both to the same directory; listing it twice would
		// clear and re-copy one tree for nothing.
		dirs := []string{roots.Export}
		if layout.Export != roots.Export {
			dirs = append(dirs, layout.Export)
		}
		for _, dir := range dirs {
			if strings.TrimSpace(dir) == "" {
				continue
			}
			out = append(out, usersandbox.MaterializeSource{HostDir: dir, Dest: skills.InSandboxSkillsRoot})
		}
		return out, nil
	}
}

// sharedSkillSources maps the grants standing right now onto one source per shared skill.
//
// ONE SOURCE PER SKILL, rooted at that skill's own directory and landing at /skills/<name>.
// The obvious shape — the owner's export tree at /skills — is the mistake amendment #215
// records in its general form: an export tree holds EVERY skill its owner has, and tarDir
// walks whatever it is given, so handing B the tree because A shared one skill from it puts
// all of A's skills in B's box. The unit of a grant is a skill, so the unit of the source is
// the skill's directory, and there is then nothing beside it to carry.
//
// A lookup that fails degrades to no shared skills, loudly, rather than failing the resolve:
// a box is how its owner reaches every one of their own tools, and taking it away because
// somebody else's share could not be confirmed is a larger outage than the missing share. The
// direction is also the safe one — the mirror then removes what it cannot re-justify.
func sharedSkillSources(ctx context.Context, shared *skills.SharedReader, identityID string) []usersandbox.MaterializeSource {
	entries, err := shared.For(ctx, identityID)
	if err != nil {
		slog.Error("sandbox materialize: could not resolve what is shared with this identity — filling the box from their own library only",
			"identity_id", identityID, "err", err)
		return nil
	}
	out := make([]usersandbox.MaterializeSource, 0, len(entries))
	for _, e := range entries {
		out = append(out, usersandbox.MaterializeSource{
			HostDir: e.Dir,
			Dest:    skills.InSandboxSkillsRoot + "/" + e.Name,
		})
	}
	return out
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
