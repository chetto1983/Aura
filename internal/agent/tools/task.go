package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/cron"
)

// TaskTool is the ONE scheduling verb the model sees (D-09/D-11). A single entry —
// name "task" — fronts the whole scheduler grammar via an `action` enum
// (schedule|list|cancel|run_now) dispatched through an ActionRouter, replacing the
// pre-rewrite 587-LOC five-tool god-class. D-11 made it non-deferred; 82dc4706e
// (2026-07-30) deferred it on measurement (795 tokens on every turn for a verb a
// fraction of turns use), so the roster line and tool_search's BM25 over this spec
// are the only two ways the model reaches it — the Summary and Description must
// carry every word an operator uses for "later / periodically / wake up / cron"
// (measured 2026-08-30: "scheduling" and "scheduler" found nothing and the model
// told the operator it could not wake itself up). The schema is OpenAI-wire-safe
// (D-10): top-level required=["action"] ONLY, with NO root oneOf/anyOf/enum (a
// root enum 400s DeepSeek, which is OpenAI-compat) — per-action requirements live
// in the field `description` strings.
//
// The live cron store and the scheduler notifier are injected at registration (10-05)
// through the consumer-declared taskStore and taskDestinations seams below.
type TaskTool struct {
	Store taskStore
	// Destinations resolves, before a task is persisted, the recipient an external notify
	// route would reach for the caller (the live scheduler Notifier). Nil skips the check,
	// as on the pool-free manifest path.
	Destinations taskDestinations
	// TelegramAccounts lists the Telegram accounts the caller links, so a notify=telegram task
	// names the ones it reaches (task_telegram_recipients.go). Nil checks nothing.
	TelegramAccounts taskTelegramAccounts
	// Timezones and DeploymentZone give a schedule with no tz the identity's zone, then
	// AURA_TIMEZONE (task_timezone.go). Both empty leave cron.ParseSchedule's Europe/Rome.
	Timezones      taskTimezones
	DeploymentZone string
	// MessageDrafts stores a scheduled message's draft against the mounted trusted tool
	// (task_message.go). Nil parks no message.
	MessageDrafts taskMessageDrafts
	// Defaults is the operator's default channel, applied when a schedule names no notify
	// (task_default_notify.go). Nil, or nothing set, leaves the delivery choice directive.
	Defaults taskDefaults

	routerOnce sync.Once
	router     *ActionRouter
}

// taskDestinations is the consumer-declared seam over the scheduler notifier's
// schedule-time check; cron.Notifier satisfies it.
type taskDestinations interface {
	Destination(ctx context.Context, route cron.NotifyRoute) (string, error)
}

// ScheduledTask is the tool-local projection of a scheduler row the task tool
// renders/operates on. It is deliberately decoupled from internal/cron's domain
// type so the tools package declares its own seam (interface segregation): the
// live cron store adapts its rows into this shape at registration (10-05).
type ScheduledTask struct {
	ID           string
	Kind         string
	ScheduleKind string
	Status       string // active | pending_approval | paused | cancelled | ...
	NextRunAt    time.Time
	RiskTier     string
	Payload      string
	NotifyRoute  string
	// PausedReason is one of cron's PausedBy constants while Status is "paused".
	PausedReason        string
	ConsecutiveFailures int
	TelegramRecipients  []int64
}

// CreateTaskInput carries a resolved, validated task the tool asks the store to
// persist. Status is set by the tool (active, or pending_approval when scoring
// gates it). NextRunAt is the first fire the tool computed.
type CreateTaskInput struct {
	Kind         string
	ScheduleKind string
	CronExpr     string
	EveryMinutes int
	RunAt        time.Time
	TZ           string
	Payload      []byte
	StepBudget   int
	NotifyRoute  string
	Status       string
	NextRunAt    time.Time
	// OriginConversationID is forwarded from toolCallCtx(ctx).sessionID — the
	// conversation that scheduled the task; "" for a bare ctx (CLI / unit test).
	// The tool only forwards the raw id; the conv→identity snapshot happens in the
	// cmd/aura adapter (so tools never imports internal/conversations).
	OriginConversationID string
	// TelegramRecipients are the Telegram accounts a notify=telegram task reaches; nil for
	// every other route.
	TelegramRecipients []int64
	// OriginToolCallID is the model call that parked the task; a second create for the same
	// call returns the first row (migration 0155). "" for a task the tool created itself.
	OriginToolCallID string
}

// taskStore is the consumer-declared seam the task tool dispatches against
// (golang-structs-interfaces: the consumer owns the interface). The live
// internal/cron store satisfies it through a thin adapter wired at registration (10-05).
type taskStore interface {
	CreateScheduledTask(ctx context.Context, in CreateTaskInput) (ScheduledTask, error)
	ListScheduledTasks(ctx context.Context) ([]ScheduledTask, error)
	CancelScheduledTask(ctx context.Context, id string) error
	RunScheduledTaskNow(ctx context.Context, id string) error
	PauseScheduledTask(ctx context.Context, id string) error
	ResumeScheduledTask(ctx context.Context, id string) (time.Time, error)
}

// taskArgs is the wire shape of the task tool arguments. Only `action` is
// required at the schema root (D-10); the other fields are per-action and their
// requirements are documented in the schema field descriptions, never as a root
// oneOf/anyOf/enum.
type taskArgs struct {
	Action       string          `json:"action"`
	ScheduleKind string          `json:"schedule_kind"`
	Cron         string          `json:"cron"`
	At           string          `json:"at"`
	EveryMinutes int             `json:"every_minutes"`
	TZ           string          `json:"tz"`
	Kind         string          `json:"kind"`
	Payload      json.RawMessage `json:"payload"`
	StepBudget   int             `json:"step_budget"`
	Notify       string          `json:"notify"`
	TaskID       string          `json:"task_id"`
	// TelegramRecipients is the operator's choice among the linked Telegram accounts.
	TelegramRecipients []int64 `json:"telegram_recipients"`
}

// taskParamsSchema is the OpenAI-wire-safe JSON schema (D-10). The root object's
// only required field is `action`; per-action requirements are spelled out in the
// field descriptions. There is intentionally NO root-level oneOf/anyOf/enum — a
// root enum 400s OpenAI-compat providers (DeepSeek). The `action` property does
// carry an enum (that is a property-level enum on a string, which is wire-safe).
const taskParamsSchema = `{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["schedule", "list", "cancel", "run_now", "pause", "resume"], "description": "The scheduler operation: schedule (create a task), list (show active, paused and awaiting-approval tasks), cancel (stop a task for good by id), run_now (fire a task immediately by id), pause (stop an active task from firing until resumed, by id), resume (reactivate a paused task from now, by id)."},
    "schedule_kind": {"type": "string", "enum": ["at", "every", "cron"], "description": "Required when action=schedule. at=one-shot at a fixed instant; every=fixed interval in minutes; cron=a cron expression evaluated in the task timezone."},
    "cron": {"type": "string", "description": "Required when action=schedule and schedule_kind=cron. A standard 5-field cron expression, e.g. '30 9 * * 1-5'."},
    "at": {"type": "string", "description": "Required when action=schedule and schedule_kind=at. An RFC-3339 instant, e.g. '2030-01-01T09:30:00Z'."},
    "every_minutes": {"type": "integer", "description": "Required when action=schedule and schedule_kind=every. Interval in minutes (minimum 5)."},
    "tz": {"type": "string", "description": "Optional IANA timezone for a cron schedule, e.g. 'Europe/Rome'. Defaults to the operator's own timezone, then the deployment's."},
    "kind": {"type": "string", "enum": ["reminder", "agent_job", "backup_postgres", "message"], "description": "Required when action=schedule. The task kind: reminder (deliver text), agent_job (run an agent turn), backup_postgres (database dump), message (send one fixed message to someone else once, reviewed by the operator now)."},
    "payload": {"type": "object", "description": "Optional when action=schedule. The task payload: for a reminder {\"text\": \"...\"}, for an agent_job {\"goal\": \"...\"}, for a message {\"channel\":\"whatsapp\",\"recipient\":\"<JID or number>\",\"text\":\"...\"} or {\"channel\":\"email\",\"account\":\"<accountId>\",\"to\":[\"...\"],\"subject\":\"...\",\"text\":\"...\"}. Scanned for destructive intent (rm/drop/delete) which gates the task to pending_approval."},
    "step_budget": {"type": "integer", "description": "Optional when action=schedule and kind=agent_job. Maximum agent steps for the job run."},
    "notify": {"type": "string", "enum": ["none", "whatsapp", "email", "stdout", "telegram"], "description": "Optional when action=schedule. Where the task output is delivered. If the request names no channel, omit notify: the operator's default channel is applied, and when none is set a delivery_choice_required directive comes back. none keeps the outcome only in the run ledger; telegram is an intentional push to the linked Telegram accounts named in telegram_recipients; whatsapp/email are explicit external self-sends; stdout means the owning conversation when present or literal CLI/system output."},
    "telegram_recipients": {"type": "array", "items": {"type": "integer"}, "description": "Optional, only with notify=telegram. The Telegram user ids of the linked accounts the task reaches. With two or more linked and none named, nothing is persisted and a telegram_recipients_required choice directive comes back: relay it with ask_user(kind=choice)."},
    "task_id": {"type": "string", "description": "Required when action=cancel, run_now, pause or resume. The id of the target task."}
  },
  "required": ["action"]
}`

// Spec returns the non-deferred manifest entry (D-11). The Summary is one tight
// line; the Description is short and turn-stable (cache-load-bearing).
func (t *TaskTool) Spec() Spec {
	return Spec{
		Name:    "task",
		Summary: "Your scheduler and timer (scheduling): schedule future, periodic or recurring work — a reminder or message to send the operator later on WhatsApp, Telegram or email (in 10 minutes, tomorrow at 9), agent_job wake-ups, every N minutes or on a cron; list, pause, resume, cancel, or run one now.",
		Description: "Aura's scheduler, timer and scheduling wake-up: manage scheduled work via a single action enum. action=schedule creates a one-shot (at), interval (every), or cron task of a kind (reminder|agent_job|backup_postgres); action=list shows active, paused and awaiting-approval tasks; action=cancel/run_now/pause/resume operate on a task_id. " +
			"The scheduler pauses a task by itself after repeated failed runs; list marks it, and it should be resumed only once the cause is fixed. " +
			"When the operator asks for recurring or future work (a daily summary, a morning digest, a periodic check, a reminder, something later or tomorrow), or when you need to wake up later to check on something, schedule it here instead of trying to do it now: a reminder delivers its payload text; an agent_job runs a fresh agent turn AT FIRE TIME with the goal in its payload, so you do NOT need the job's tools available now — the job resolves its own tools when it runs. Put the operator's intent in the payload goal and schedule it. An agent_job cannot send a message to anyone other than the operator; to send a fixed message later, use kind=message. " +
			"Before scheduling, make sure the task has everything it will need when it fires: when (time or cadence), what (the reminder text or a self-contained job goal), and for a message the exact recipient and text; where it is delivered comes from an explicit notify, else the operator's default channel. A scheduled run starts in a fresh session and cannot ask anyone anything. If any of these is missing or ambiguous in the request, ask the operator one question about it, time first, and schedule only after the answer. Never invent a time or a recipient. " +
			"A reminder or message the operator wants later on a channel (\"remind me on WhatsApp in 10 minutes\", \"email me tomorrow\", \"message me on Telegram in an hour\") is a reminder task with notify set to that channel: at fire time the scheduler delivers it to the operator's own WhatsApp, email or Telegram. Never send it now with a messaging tool, and never look up the operator's contact for it. " +
			"For action=schedule always honor an explicit notify choice. If the request names no channel, omit notify: the operator's default channel is applied, and when none is set a delivery_choice_required directive comes back. " +
			"kind=message sends one fixed message to someone else (WhatsApp or email) once, at most 23h45m ahead, with schedule_kind=at: resolve the exact recipient first and ask the operator now if two contacts match; an email also needs payload.account, the account it is sent from (calendar list_accounts; ask the operator which when there is more than one); the operator reviews the recipient and the text before the task exists, and it is sent at its time with no model in between. " +
			"A task that needs approval (every agent_job, and payloads scored destructive) is shown to the operator for approval in this chat before it can fire; with nobody in the chat it waits, and the operator is reminded on their channel.",
		Parameters: json.RawMessage(taskParamsSchema),
		// Deferred: 795 token per una capacita' che entra in gioco solo quando l'operatore
		// chiede lavoro ricorrente o futuro — una frazione dei turni.
		Deferred: true,
		// D-02/D-02d: the task verb multiplexes schedule/list/cancel/run_now behind one
		// `action` enum, so it is the fail-closed Mutating floor and carries the
		// Multiplexed hint the gateway boot-guard reads.
		Mutating:       true,
		Multiplexed:    true,
		OperationScope: OperationScopeAgent, OperationNormalizer: OperationNormalizerCanonical,
		ReplayPolicy: ReplayToolResult,
	}
}

// Execute parses the `action` discriminator and dispatches through the
// ActionRouter (lazily built once, bound to this tool's store). It never panics
// on a bad action — the router returns a structured error.
func (t *TaskTool) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	return dispatchStoreAction(ctx, "task", raw, t.Store != nil, "no task store is configured", t.actionRouter)
}

func (t *TaskTool) actionRouter() *ActionRouter {
	t.routerOnce.Do(func() {
		t.router = NewActionRouter(map[string]ActionFunc{
			"schedule": t.actionSchedule,
			"list":     t.actionList,
			"cancel":   t.actionCancel,
			"run_now":  t.actionRunNow,
			"pause":    t.actionPause,
			"resume":   t.actionResume,
		})
	})
	return t.router
}

// actionSchedule validates the grammar, computes the risk tier, and persists the
// task as active or — when scoring recommends a gate (Risky/Destructive) — as
// pending_approval so a destructive scheduled task can never fire before an
// explicit approve (T-10-12, D-27). The cron expression is validated by
// ParseSchedule (gronx) before any persist (T-10-14, V5).
func (t *TaskTool) actionSchedule(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	var a taskArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return ToolResult{}, fmt.Errorf("task schedule args: %w", err)
	}
	if err := validateSchedule(a); err != nil {
		return ToolResult{}, err
	}
	if a.Kind == "message" {
		return t.scheduleMessage(ctx, a)
	}
	if strings.TrimSpace(a.TZ) == "" {
		a.TZ = t.scheduleZone(ctx)
	}
	spec, next, err := t.resolveSchedule(a)
	if err != nil {
		return ToolResult{}, err
	}

	tier, needsApproval := taskNeedsApproval(a)
	plan, directive, err := t.resolveDelivery(ctx, &a)
	if err != nil {
		return ToolResult{}, err
	}
	if directive != nil {
		return *directive, nil
	}
	status := "active"
	if needsApproval {
		status = "pending_approval"
	}

	// Headless workers have a flat technical session id, so use their explicit
	// origin conversation for the durable owner. Direct calls retain the historic
	// tool-call-session fallback.
	originConvID := OriginConversationIDFromContext(ctx)
	if originConvID == "" {
		if tc, ok := toolCallCtx(ctx); ok {
			originConvID = tc.sessionID
		}
	}

	created, err := t.Store.CreateScheduledTask(ctx, CreateTaskInput{
		Kind:                 a.Kind,
		ScheduleKind:         spec.ScheduleKind,
		CronExpr:             spec.CronExpr,
		EveryMinutes:         spec.EveryMinutes,
		RunAt:                spec.RunAt,
		TZ:                   spec.TZ,
		Payload:              payloadJSON(a.Payload),
		StepBudget:           a.StepBudget,
		NotifyRoute:          a.Notify,
		Status:               status,
		NextRunAt:            next,
		OriginConversationID: originConvID,
		TelegramRecipients:   TelegramRecipientIDs(plan.Recipients),
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("task schedule: %w", err)
	}

	// With a person in the chat the agent parked this call behind the approval question
	// (HostReview), so this branch runs only headless: the task waits, and the sweep reminds the
	// operator on their channel (decision 24).
	if status == "pending_approval" {
		return awaitingApprovalResult(created.ID, a.Kind, spec.summary(), string(tier), plan.Note), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "scheduled task %s (kind=%s, %s, risk=%s, status=active)", created.ID, a.Kind, spec.summary(), tier)
	if !next.IsZero() {
		fmt.Fprintf(&b, "\nNext run at %s.", next.UTC().Format(time.RFC3339))
	}
	if plan.Note != "" {
		b.WriteString("\n" + plan.Note)
	}
	s := b.String()
	return ToolResult{Preview: s, Bytes: len(s)}, nil
}

// resolvedSpec is the validated grammar the tool persists. It mirrors the cron
// ScheduleSpec fields but lives in the tools package (no internal/cron import).
type resolvedSpec struct {
	ScheduleKind string
	CronExpr     string
	EveryMinutes int
	RunAt        time.Time
	TZ           string
}

func (r resolvedSpec) summary() string {
	switch r.ScheduleKind {
	case "at":
		return "at " + r.RunAt.UTC().Format(time.RFC3339)
	case "every":
		return fmt.Sprintf("every %dm", r.EveryMinutes)
	case "cron":
		return fmt.Sprintf("cron %q tz=%s", r.CronExpr, r.TZ)
	default:
		return r.ScheduleKind
	}
}

// resolveSchedule validates the at|every|cron grammar via the shipped cron engine
// (cron.ParseSchedule — gronx.IsValid gates the cron expr before any persist,
// T-10-14/V5) and computes the first fire via cron.NextRunAt (DST-safe, D-07). The
// `at` instant is parsed from an RFC-3339 string the model supplies.
func (t *TaskTool) resolveSchedule(a taskArgs) (resolvedSpec, time.Time, error) {
	var at time.Time
	if a.ScheduleKind == "at" {
		parsed, err := time.Parse(time.RFC3339, a.At)
		if err != nil {
			return resolvedSpec{}, time.Time{}, fmt.Errorf("task schedule: at must be RFC-3339: %w", err)
		}
		at = parsed
	}

	spec, err := cron.ParseSchedule(a.ScheduleKind, a.Cron, a.EveryMinutes, at, a.TZ)
	if err != nil {
		return resolvedSpec{}, time.Time{}, fmt.Errorf("task schedule: %w", err)
	}
	next, err := cron.FirstFire(spec, time.Now())
	if err != nil {
		return resolvedSpec{}, time.Time{}, fmt.Errorf("task schedule: %w", err)
	}
	return resolvedSpec{
		ScheduleKind: string(spec.Kind),
		CronExpr:     spec.CronExpr,
		EveryMinutes: spec.EveryMinutes,
		RunAt:        spec.RunAt,
		TZ:           spec.TZ,
	}, next, nil
}

func (t *TaskTool) actionList(ctx context.Context, _ json.RawMessage) (ToolResult, error) {
	rows, err := t.Store.ListScheduledTasks(ctx)
	if err != nil {
		return ToolResult{}, fmt.Errorf("task list: %w", err)
	}
	s := renderTaskList(rows)
	return ToolResult{Preview: s, Bytes: len(s)}, nil
}

// renderTaskList formats active, paused and pending tasks, flagging awaiting-approval and paused rows
// and showing each task's next fire (Claude's discretion format, D-14/D-17).
func renderTaskList(rows []ScheduledTask) string {
	if len(rows) == 0 {
		return "no scheduled tasks"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%d task(s):\n", len(rows))
	for _, r := range rows {
		flag := ""
		if r.Status == "pending_approval" {
			flag = " [awaiting approval]"
		} else if r.Status == "paused" {
			flag = pausedFlag(r)
		} else if r.Status == "active" && r.NextRunAt.IsZero() {
			// An active task with no next fire can never be selected by the tick
			// (DueTasks filters next_run_at <= now) — surface it so the model/operator
			// can run_now or cancel it instead of silently never firing (WR-01).
			flag = " [unschedulable]"
		}
		next := "—"
		if !r.NextRunAt.IsZero() {
			next = r.NextRunAt.UTC().Format(time.RFC3339)
		}
		fmt.Fprintf(&b, "  %s  kind=%s  %s  next=%s%s", r.ID, r.Kind, r.ScheduleKind, next, flag)
		if payload := taskPayloadPreview(r.Payload); payload != "" {
			fmt.Fprintf(&b, "  payload=%s", payload)
		}
		if r.NotifyRoute != "" {
			fmt.Fprintf(&b, "  notify=%s", r.NotifyRoute)
		}
		if len(r.TelegramRecipients) > 0 {
			fmt.Fprintf(&b, "  telegram_recipients=%s", telegramRecipientsListing(r.TelegramRecipients))
		}
		b.WriteByte('\n')
	}
	return strings.TrimRight(b.String(), "\n")
}

const taskPayloadPreviewRunes = 500

func taskPayloadPreview(payload string) string {
	payload = strings.Join(strings.Fields(payload), " ")
	runes := []rune(payload)
	if len(runes) <= taskPayloadPreviewRunes {
		return payload
	}
	return string(runes[:taskPayloadPreviewRunes]) + "..."
}

func (t *TaskTool) actionCancel(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	id, err := requireTaskID(raw, "cancel")
	if err != nil {
		return ToolResult{}, err
	}
	if err := t.Store.CancelScheduledTask(ctx, id); err != nil {
		return ToolResult{}, fmt.Errorf("task cancel: %w", err)
	}
	s := "cancelled task " + id
	return ToolResult{Preview: s, Bytes: len(s)}, nil
}

func (t *TaskTool) actionRunNow(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
	id, err := requireTaskID(raw, "run_now")
	if err != nil {
		return ToolResult{}, err
	}
	if err := t.Store.RunScheduledTaskNow(ctx, id); err != nil {
		return ToolResult{}, fmt.Errorf("task run_now: %w", err)
	}
	s := "running task " + id + " now"
	return ToolResult{Preview: s, Bytes: len(s)}, nil
}

func requireTaskID(raw json.RawMessage, action string) (string, error) {
	var a taskArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return "", fmt.Errorf("task %s args: %w", action, err)
	}
	if strings.TrimSpace(a.TaskID) == "" {
		return "", fmt.Errorf("task %s: task_id is required", action)
	}
	return a.TaskID, nil
}

func payloadJSON(p json.RawMessage) []byte {
	if len(p) == 0 {
		return []byte("{}")
	}
	return p
}
