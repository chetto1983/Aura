// serve_adapters.go holds the composition-root adapters that bridge the cron-local
// consumer-declared interfaces (10-05 deviation #1/#3) onto the live runtime types,
// keeping package cron free of an internal/agent/tools import and the tools package
// free of an internal/cron import. Two adapters live here:
//
//   - selfSendResolver: a *tools.Registry → cron.SelfSendResolver over the mounted
//     MCP self-send tools (send_message / send_email), namespaced <server>__<tool>;
//   - cronTaskStore: a cron.Store → tools.taskStore so the live LLM-facing `task` tool
//     persists against the real Postgres (the status-aware INSERT + approve/run_now
//     UPDATEs the cron.Store does not expose are run as raw parameterized SQL over the
//     pool, mirroring cmd/aura/task.go — never string-concatenated, T-10-01).
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/gateway"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/chetto1983/aura/internal/runner"
	"github.com/chetto1983/aura/internal/scoring"
	"github.com/chetto1983/aura/internal/skilladapters"
	"github.com/chetto1983/aura/internal/skills"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newTaskTool builds the non-deferred `task` tool, injecting the live store only when
// one is present. A nil ts leaves TaskTool.Store as a genuine nil interface (not an
// interface wrapping a nil pointer), so the pool-free manifest path lists the tool's
// Spec without a half-wired store that would panic on a nil-pointer method call.
func newTaskTool(ts *cronTaskStore) *tools.TaskTool {
	t := &tools.TaskTool{AlertThreshold: scoring.Risky}
	if ts != nil {
		t.Store = ts
	}
	return t
}

// --- SelfSendResolver adapter (Notifier MCP self-send, D-19) ---

// selfSendResolver resolves an MCP self-send tool by its bare name (send_message /
// send_email) off the mounted registry. MCP tools are namespaced <server>__<tool>
// (mcptools/name.go), so the resolver matches the bare suffix after the "__"
// delimiter (or an exact bare name, for a non-namespaced tool).
type selfSendResolver struct {
	reg *tools.Registry
}

var _ cron.SelfSendResolver = (*selfSendResolver)(nil)

// newSelfSendResolver builds the resolver over the mounted registry. A nil registry
// yields a resolver that never resolves (the Notifier then degrades every route to
// stdout, the always-available fallback sink).
func newSelfSendResolver(reg *tools.Registry) *selfSendResolver {
	return &selfSendResolver{reg: reg}
}

// Resolve finds the registered tool whose name is bareName or ends with
// "__"+bareName (the MCP namespacing), returning a SelfSendTool handle. It returns
// false when no matching tool is mounted (the Notifier falls back to stdout, D-22).
func (r *selfSendResolver) Resolve(bareName string) (cron.SelfSendTool, bool) {
	if r.reg == nil {
		return nil, false
	}
	suffix := "__" + bareName
	for _, t := range r.reg.All() {
		name := t.Spec().Name
		if name == bareName || strings.HasSuffix(name, suffix) {
			return selfSendTool{tool: t}, true
		}
	}
	return nil, false
}

// selfSendTool wraps a resolved MCP tool so Send executes it with the self-send args
// the Notifier built. A non-nil error (or a tool result the MCP server flagged as an
// error) surfaces so the composite Notifier falls back to stdout (D-22).
type selfSendTool struct {
	tool tools.Tool
}

var _ cron.SelfSendTool = selfSendTool{}

// Send executes the MCP self-send tool. The tools.ToolResult carries the delivery
// preview; an Execute error is the MCP-side failure the Notifier treats as undelivered.
func (s selfSendTool) Send(ctx context.Context, args json.RawMessage) error {
	if _, err := s.tool.Execute(ctx, args); err != nil {
		return fmt.Errorf("mcp self-send %q: %w", s.tool.Spec().Name, err)
	}
	return nil
}

// --- taskStore adapter (live `task` tool persistence, 10-05 deviation #3) ---

// cronTaskStore adapts the live Postgres pool + cron.Store onto the tools.taskStore
// seam the `task` tool dispatches against. CreateScheduledTask/CancelScheduledTask
// reuse cron.Store; ListScheduledTasks/RunScheduledTaskNow/ApproveScheduledTask run
// the status-aware reads/UPDATEs cron.Store does not expose as raw parameterized SQL
// (the cmd/aura/task.go CLI precedent), never string-concatenated.
type cronTaskStore struct {
	pool  *pgxpool.Pool
	store *cron.Store
	conv  *conversations.Store // schedule-time origin-conversation → identity resolver (Phase 20, Fork 1)
}

// newCronTaskStore builds the adapter over the live pool. A nil pool yields an
// adapter whose methods error — but the registry only wires it when a pool exists
// (serve/chat boot); the pool-free manifest path registers the tool with no store.
// conv is the conversations.Store the adapter calls to snapshot the owning identity
// at schedule time; a nil conv leaves the origin un-resolved (identity → 'local').
func newCronTaskStore(pool *pgxpool.Pool, conv *conversations.Store) *cronTaskStore {
	return &cronTaskStore{pool: pool, store: cron.New(pool), conv: conv}
}

// CreateScheduledTask persists a resolved task via cron.Store, honoring the tool's
// computed status (active | pending_approval) in a SINGLE INSERT. cron.Store.CreateTask
// now binds the initial status, so a pending_approval task is gated atomically — there
// is no INSERT-active-then-UPDATE window in which a crash could leave a destructive
// task active and claimable by the next tick (WR-03 / T-10-12 / D-27). This matches
// the CLI's one-statement insert.
func (s *cronTaskStore) CreateScheduledTask(ctx context.Context, in tools.CreateTaskInput) (tools.ScheduledTask, error) {
	status := in.Status
	if status == "" {
		status = "active"
	}
	// Snapshot the owning identity ONCE at schedule time (transactional-outbox /
	// Klaviyo pattern, Fork 1 / D-01): a later-deleted origin conversation still
	// resolves the same owning channel, and the dispatcher (20-03) reads
	// task.IdentityID directly with zero lookup. The invocation identity is the
	// fallback only when no origin conversation exists; an unscoped call fails
	// closed instead of falling through cron.Store's legacy `local` default.
	identityID := identityctx.IdentityID(ctx)
	if in.OriginConversationID != "" && s.conv != nil {
		conv, err := s.conv.Get(ctx, in.OriginConversationID)
		switch {
		case err == nil:
			identityID = conv.IdentityID
		case errors.Is(err, conversations.ErrConversationNotFound):
			// Origin gone (or a stray id) → leave identityID="" → 'local'; soft, no fail.
		default:
			// A real DB error: hard-fail rather than persist a wrong/empty identity,
			// so the operator sees the failure instead of a misrouted reminder.
			return tools.ScheduledTask{}, fmt.Errorf("resolve origin identity: %w", err)
		}
	}
	if identityID == "" {
		return tools.ScheduledTask{}, errors.New("schedule task requires an identity")
	}
	created, err := s.store.CreateTask(ctx, cron.CreateTaskParams{
		Kind: cron.TaskKind(in.Kind),
		Spec: cron.ScheduleSpec{
			Kind:         cron.ScheduleKind(in.ScheduleKind),
			CronExpr:     in.CronExpr,
			EveryMinutes: in.EveryMinutes,
			RunAt:        in.RunAt,
			TZ:           in.TZ,
		},
		Payload:              in.Payload,
		StepBudget:           in.StepBudget,
		NextRunAt:            in.NextRunAt,
		NotifyRoute:          in.NotifyRoute,
		Status:               status,
		IdentityID:           identityID,
		OriginConversationID: in.OriginConversationID,
	})
	if err != nil {
		return tools.ScheduledTask{}, err
	}
	return tools.ScheduledTask{
		ID:           created.ID,
		Kind:         string(created.Kind),
		ScheduleKind: string(created.ScheduleKind),
		Status:       created.Status,
		NextRunAt:    created.NextRunAt,
	}, nil
}

// ListScheduledTasks returns active + pending_approval tasks (the LLM-facing list,
// mirroring the CLI taskList query).
func (s *cronTaskStore) ListScheduledTasks(ctx context.Context) ([]tools.ScheduledTask, error) {
	identityID, err := taskIdentity(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, kind, schedule_kind, status, next_run_at, payload, notify_route
		FROM aura.scheduler_tasks
		WHERE identity_id = $1
		  AND status IN ('active', 'pending_approval')
		ORDER BY next_run_at ASC NULLS LAST, id ASC`, identityID)
	if err != nil {
		return nil, fmt.Errorf("list scheduled tasks: %w", err)
	}
	defer rows.Close()

	var out []tools.ScheduledTask
	for rows.Next() {
		var t tools.ScheduledTask
		var next *time.Time
		var payload []byte
		if err := rows.Scan(&t.ID, &t.Kind, &t.ScheduleKind, &t.Status, &next, &payload, &t.NotifyRoute); err != nil {
			return nil, fmt.Errorf("scan scheduled task: %w", err)
		}
		if next != nil {
			t.NextRunAt = *next
		}
		t.Payload = string(payload)
		out = append(out, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list scheduled tasks: %w", err)
	}
	return out, nil
}

// CancelScheduledTask soft-cancels only inside the identity carried by the tool call.
func (s *cronTaskStore) CancelScheduledTask(ctx context.Context, id string) error {
	identityID, err := taskIdentity(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE aura.scheduler_tasks
		SET status = 'cancelled', updated_at = now()
		WHERE id = $1::uuid
		  AND identity_id = $2
		  AND status IN ('active', 'pending_approval')`, id, identityID)
	if err != nil {
		return fmt.Errorf("cancel task %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s is not active or not owned by this identity", id)
	}
	return nil
}

// RunScheduledTaskNow flips an active task's next_run_at to now so the next tick
// claims it. A pending_approval task is refused — approval is the only path out of
// pending_approval (D-13: run_now must not bypass the gate).
func (s *cronTaskStore) RunScheduledTaskNow(ctx context.Context, id string) error {
	identityID, err := taskIdentity(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE aura.scheduler_tasks
		SET next_run_at = now(), updated_at = now()
		WHERE id = $1::uuid
		  AND identity_id = $2
		  AND status = 'active'`, id, identityID)
	if err != nil {
		return fmt.Errorf("run_now task %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s is not active (pending_approval or cancelled tasks cannot be run now)", id)
	}
	return nil
}

func taskIdentity(ctx context.Context) (string, error) {
	identityID := identityctx.IdentityID(ctx)
	if identityID == "" {
		return "", errors.New("scheduled task operation requires an identity")
	}
	return identityID, nil
}

// ApproveScheduledTask is the only transition out of pending_approval (T-10-13): it
// flips status to active so the gated task can fire.
func (s *cronTaskStore) ApproveScheduledTask(ctx context.Context, id string) error {
	identityID, err := taskIdentity(ctx)
	if err != nil {
		return err
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE aura.scheduler_tasks
		SET status = 'active', updated_at = now()
		WHERE id = $1::uuid
		  AND identity_id = $2
		  AND status = 'pending_approval'`, id, identityID)
	if err != nil {
		return fmt.Errorf("approve task %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("task %s is not awaiting approval", id)
	}
	return nil
}

// --- skill tool wiring (live `skill` tool, 11-02; adapters in internal/skilladapters) ---

// taskStorePool extracts the live pool from the task-store adapter (nil-safe): the
// pool-free manifest path passes a nil store, so the skill tool's write actions are
// not wired there.
func taskStorePool(ts *cronTaskStore) *pgxpool.Pool {
	if ts == nil {
		return nil
	}
	return ts.pool
}

// newSkillTool builds the non-deferred `skill` tool. A nil cfg or empty skills dir
// yields a tool with a nil loader (its Spec still lists, the manifest shows
// "(none loaded)") so the pool-free manifest path registers it without a half-wired
// loader. When a skills dir is configured the builtins are materialized first so
// skill-creator appears in the very first scan. When a live pool is supplied
// (serve/chat boot) the write actions are wired to the durable, gated Writer (11-05)
// via skilladapters.NewWriter; the pool-free path leaves Writer nil (write actions
// error loudly). Discovery+install is no longer a tool concern (amendment #51 /
// D-40): the find-skills always-on skill teaches self-extension via the sandbox CLI.
// registerSkillTools registers BOTH halves of the skills grammar over ONE SkillTool:
// the read verb `skill` (list/info/use) and the write verb `skill_manage` (authoring,
// install, snippet lifecycle). They share the instance so one loader, one writer and one
// cache-invalidation path serve both — a skill written through skill_manage is visible to
// skill info/use in the same turn. Registering them together is what keeps the two call
// sites (chat/serve boot and the cache audit) from drifting apart.
func registerSkillTools(reg *tools.Registry, cfg *config.Config, writerPool *pgxpool.Pool) *tools.SkillManageTool {
	skillTool := newSkillTool(cfg, writerPool)
	manageTool := &tools.SkillManageTool{Skills: skillTool}
	reg.Register(skillTool)
	reg.Register(manageTool)
	// Registered beside the skill tools because a pack IS a group of them plus the
	// connectors that ship alongside, and the model reaches for one right after the
	// other: read the pack, then install the skills it names through skill_manage.
	reg.Register(&tools.PackTool{})
	return manageTool
}

// skillInstallHook adapts the skill tool's install into the seam shell_exec takes, so a
// `npx skills add` typed into the box is answered by the host pipeline instead of succeeding into
// a container directory no loader reads. It returns nil when no writer is wired (no pool): the
// shell then refuses the command with guidance rather than running it, which is the same
// fail-closed answer every other box-boundary decision gives.
func skillInstallHook(manage *tools.SkillManageTool) func(context.Context, string) (string, error) {
	if manage == nil || manage.Skills == nil || manage.Skills.Writer == nil {
		return nil
	}
	return manage.Skills.InstallSource
}

func newSkillTool(cfg *config.Config, writerPool *pgxpool.Pool) *tools.SkillTool {
	if cfg == nil || cfg.SkillsDir == "" {
		return &tools.SkillTool{}
	}
	if err := skills.MaterializeBuiltins(cfg.SkillsDir); err != nil {
		slog.Warn("skill tool: materialize builtins failed", "dir", cfg.SkillsDir, "err", err)
	}
	// One loader per identity, resolved on the call (amendment #214): the model reads its
	// OWN library overlaid on the deployment's, and never another person's.
	loaders := newIdentityLoaders(cfg, newSharedSkillReader(cfg, writerPool))
	tool := &tools.SkillTool{
		Loader: skilladapters.NewIdentityLoader(loaders.forIdentity, loaders.invalidateAll, cfg.SkillManifestCapBytes),
	}
	if writerPool != nil {
		w := newSkillWriter(cfg, writerPool)
		// The model's install path is the SAME Installer the cockpit uses — one fetch +
		// validate + audit implementation, not a second one that could drift.
		installer := skills.NewInstaller(skills.InstallerConfig{
			Writer:       w,
			Blocklist:    cfg.SkillInjectionBlocklist,
			BodyCapBytes: cfg.SkillBodyCapBytes,
			WorkDir:      cfg.RunDir,
		})
		tool.Writer = skilladapters.NewWriter(w, installer)
	}
	return tool
}

func chainResumeHooks(hooks ...runner.ResumeHook) runner.ResumeHook {
	filtered := make([]runner.ResumeHook, 0, len(hooks))
	for _, hook := range hooks {
		if hook != nil {
			filtered = append(filtered, hook)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	return func(ctx context.Context, pending askuser.Pending, resp runner.ResponseInput) error {
		for _, hook := range filtered {
			if err := hook(ctx, pending, resp); err != nil {
				return err
			}
		}
		return nil
	}
}

func newShellResumeHook(approvals *tools.ShellApprovals) runner.ResumeHook {
	if approvals == nil {
		return nil
	}
	return func(ctx context.Context, pending askuser.Pending, resp runner.ResponseInput) error {
		if pending.Kind != tools.KindApproval || len(pending.ResumeContext) == 0 ||
			(resp.Action != askuser.ActionAccept && resp.Action != askuser.ActionExpired) {
			return nil
		}
		var rc struct {
			Type          string `json:"type"`
			CommandSHA256 string `json:"command_sha256"`
		}
		if err := json.Unmarshal(pending.ResumeContext, &rc); err != nil {
			return fmt.Errorf("shell resume context: %w", err)
		}
		if rc.Type != "shell_exec_approval" {
			return nil
		}
		if rc.CommandSHA256 == "" {
			return fmt.Errorf("shell resume context: missing command_sha256")
		}
		if resp.Action == askuser.ActionExpired {
			approvals.DiscardChallenge(pending.ConversationID, rc.CommandSHA256)
			return nil
		}
		return approvals.ApproveChallenge(pending.ConversationID, rc.CommandSHA256, pending.Question)
	}
}

// newGatewayResumeHook is the production ResumeHook that records an operator's accept of a
// relayed gateway_approval ask_user pause into the gateway's cross-turn approval ledger —
// the byte-for-byte analog of newShellResumeHook (challenge + question gated), and the SOLE
// production writer of GatewayApprovals (D-03c: the model relaying via ask_user does NOT
// grant approval). It runs host-side after the authenticated approval-center resolve
// (SubmitAnswers -> applyResumeHook). It records ONLY IF routeApprove issued a challenge for
// this digest AND the operator-visible pending.Question matches the gateway-generated one
// (g.ApproveChallenge) — so a model relaying a benign/false question records NOTHING and the
// re-drive stays withheld (CR-01 informed-consent binding). It keys on the AUTHENTICATED
// pending.ConversationID (server-stored at pause creation), never the model-relayed
// resume_context id, so an accept surfaced from conv-A cannot authorize a re-emit in conv-B
// (WR-02). A decline/cancel/wrong-type records NOTHING (fail-closed). OperatorID is "local"
// — single_user_hardened has exactly one principal (the owner); multi-identity operator
// attribution is deferred to Phase 36 (D-03b).
func newGatewayResumeHook(g *gateway.Gateway) runner.ResumeHook {
	if g == nil {
		return nil
	}
	return func(ctx context.Context, pending askuser.Pending, resp runner.ResponseInput) error {
		if pending.Kind != tools.KindApproval || len(pending.ResumeContext) == 0 ||
			(resp.Action != askuser.ActionAccept && resp.Action != askuser.ActionExpired) {
			return nil
		}
		var rc struct {
			Type       string `json:"type"`
			Tool       string `json:"tool"`
			ArgsSHA256 string `json:"args_sha256"`
		}
		if err := json.Unmarshal(pending.ResumeContext, &rc); err != nil {
			return fmt.Errorf("gateway resume context: %w", err)
		}
		if rc.Type != "gateway_approval" {
			return nil
		}
		if rc.Tool == "" || rc.ArgsSHA256 == "" {
			return fmt.Errorf("gateway resume context: missing tool or args_sha256")
		}
		if resp.Action == askuser.ActionExpired {
			g.DiscardApprovalChallenge(pending.ConversationID, rc.Tool, rc.ArgsSHA256)
			return nil
		}
		// Record ONLY IF the gateway issued a challenge for this (authenticated conversation,
		// tool, digest) AND the operator-visible pending.Question equals the gateway-generated
		// one (ApproveChallenge — the CR-01 informed-consent binding, mirroring
		// newShellResumeHook). The authenticated pending.ConversationID is the key (WR-02),
		// never the model-relayed resume_context id. A mismatched/benign question or a missing
		// challenge → error, nothing recorded → the re-drive re-issues the approval (withheld).
		//
		// resp.Content is the label the operator pressed. It is passed through as DATA, never
		// trusted as an instruction: the gateway resolves it against the label table of the
		// challenge it issued, so a value that matches nothing grants the narrowest scope
		// (amendment #127). identityctx carries the authenticated principal a durable
		// "always" grant belongs to; without one the accept degrades to this conversation.
		scope, err := g.ApproveChallenge(ctx, gateway.ApprovalAccept{
			ConversationID:  pending.ConversationID,
			Tool:            rc.Tool,
			ArgsFingerprint: rc.ArgsSHA256,
			Question:        pending.Question,
			Answer:          resp.Content,
			IdentityID:      identityctx.IdentityID(ctx),
			OperatorID:      "local",
		})
		if err != nil {
			return err
		}
		slog.Info("gateway approval recorded",
			"conversation_id", pending.ConversationID, "tool", rc.Tool, "scope", string(scope))
		return nil
	}
}

// snippetSweeperAdapter bridges the live *skills.Writer onto the handlers.SnippetSweeper
// seam the skill_ttl_sweep handler drives (D-16): it projects the Writer's SweepResult
// onto the handler's (archived, kept) shape, keeping internal/cron/handlers free of an
// internal/skills SweepResult dependency.
type snippetSweeperAdapter struct {
	w *skills.Writer
}

// SweepExpiredSnippets archives every snippet older than ttl via the Writer (each gets
// the D-29 `auto` audit row) and returns the archived + kept names.
func (a *snippetSweeperAdapter) SweepExpiredSnippets(ctx context.Context, ttl time.Duration, now time.Time, actorID string) (archived, kept []string, err error) {
	res, serr := a.w.SweepExpiredSnippets(ctx, ttl, now, skills.AuditActor{ActorID: actorID})
	if serr != nil {
		return nil, nil, serr
	}
	return res.Archived, res.Kept, nil
}
