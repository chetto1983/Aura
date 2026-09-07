package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/scoring"
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
// The live cron store is injected at registration (10-05) via the consumer-declared
// taskStore seam below, so this package never imports internal/cron concretely.
type TaskTool struct {
	Store taskStore
	// AlertThreshold is the risk tier at/above which a scheduled task fires an
	// immediate alert (config owns AURA_RISK_ALERT_THRESHOLD; scoring takes it as
	// an argument, never reads env). Empty defaults to Risky.
	AlertThreshold scoring.RiskTier

	routerOnce sync.Once
	router     *ActionRouter
}

// ScheduledTask is the tool-local projection of a scheduler row the task tool
// renders/operates on. It is deliberately decoupled from internal/cron's domain
// type so the tools package declares its own seam (interface segregation): the
// live cron store adapts its rows into this shape at registration (10-05).
type ScheduledTask struct {
	ID           string
	Kind         string
	ScheduleKind string
	Status       string // active | pending_approval | cancelled | ...
	NextRunAt    time.Time
	RiskTier     string
	Payload      string
	NotifyRoute  string
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
}

// taskStore is the consumer-declared seam the task tool dispatches against
// (golang-structs-interfaces: the consumer owns the interface). The live
// internal/cron store satisfies it through a thin adapter wired at registration
// (10-05), keeping internal/agent/tools free of an internal/cron import.
type taskStore interface {
	CreateScheduledTask(ctx context.Context, in CreateTaskInput) (ScheduledTask, error)
	ListScheduledTasks(ctx context.Context) ([]ScheduledTask, error)
	CancelScheduledTask(ctx context.Context, id string) error
	RunScheduledTaskNow(ctx context.Context, id string) error
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
}

// taskParamsSchema is the OpenAI-wire-safe JSON schema (D-10). The root object's
// only required field is `action`; per-action requirements are spelled out in the
// field descriptions. There is intentionally NO root-level oneOf/anyOf/enum — a
// root enum 400s OpenAI-compat providers (DeepSeek). The `action` property does
// carry an enum (that is a property-level enum on a string, which is wire-safe).
const taskParamsSchema = `{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["schedule", "list", "cancel", "run_now"], "description": "The scheduler operation: schedule (create a task), list (show active + awaiting-approval tasks), cancel (stop a task by id), run_now (fire a task immediately by id)."},
    "schedule_kind": {"type": "string", "enum": ["at", "every", "cron"], "description": "Required when action=schedule. at=one-shot at a fixed instant; every=fixed interval in minutes; cron=a cron expression evaluated in the task timezone."},
    "cron": {"type": "string", "description": "Required when action=schedule and schedule_kind=cron. A standard 5-field cron expression, e.g. '30 9 * * 1-5'."},
    "at": {"type": "string", "description": "Required when action=schedule and schedule_kind=at. An RFC-3339 instant, e.g. '2030-01-01T09:30:00Z'."},
    "every_minutes": {"type": "integer", "description": "Required when action=schedule and schedule_kind=every. Interval in minutes (minimum 5)."},
    "tz": {"type": "string", "description": "Optional IANA timezone for a cron schedule, e.g. 'Europe/Rome'. Defaults to the scheduler default timezone."},
    "kind": {"type": "string", "enum": ["reminder", "agent_job", "backup_postgres"], "description": "Required when action=schedule. The task kind: reminder (deliver text), agent_job (run an agent turn), backup_postgres (database dump)."},
    "payload": {"type": "object", "description": "Optional when action=schedule. The task payload: for a reminder {\"text\": \"...\"}, for an agent_job {\"goal\": \"...\"}. Scanned for destructive intent (rm/drop/delete) which gates the task to pending_approval."},
    "step_budget": {"type": "integer", "description": "Optional when action=schedule and kind=agent_job. Maximum agent steps for the job run."},
    "notify": {"type": "string", "enum": ["none", "whatsapp", "email", "stdout", "telegram"], "description": "Required when action=schedule. Where the task output is delivered. If the operator did not choose, call ask_user(kind=choice) before scheduling. An omitted value persists nothing and returns the same choice directive. none keeps the outcome only in the run ledger; telegram is an intentional push to the bound Telegram chat; whatsapp/email are explicit external self-sends; stdout means the owning conversation when present or literal CLI/system output."},
    "task_id": {"type": "string", "description": "Required when action=cancel or run_now. The id of the target task."}
  },
  "required": ["action"]
}`

// Spec returns the non-deferred manifest entry (D-11). The Summary is one tight
// line; the Description is short and turn-stable (cache-load-bearing).
func (t *TaskTool) Spec() Spec {
	return Spec{
		Name:    "task",
		Summary: "Your scheduler and timer (scheduling): schedule future, periodic or recurring work — reminders and agent_job wake-ups later, tomorrow, at a time, every N minutes or on a cron; list, cancel, or run one now.",
		Description: "Aura's scheduler, timer and scheduling wake-up: manage scheduled work via a single action enum. action=schedule creates a one-shot (at), interval (every), or cron task of a kind (reminder|agent_job|backup_postgres); action=list shows active and awaiting-approval tasks; action=cancel/run_now operate on a task_id. " +
			"When the operator asks for recurring or future work (a daily summary, a morning digest, a periodic check, a reminder, something later or tomorrow), or when you need to wake up later to check on something, schedule it here instead of trying to do it now: a reminder delivers its payload text; an agent_job runs a fresh agent turn AT FIRE TIME with the goal in its payload, so you do NOT need the job's tools available now — the job resolves its own tools when it runs. Put the operator's intent in the payload goal and schedule it. " +
			"For action=schedule always honor an explicit notify choice. If the operator did not specify one, call ask_user(kind=choice) before scheduling. An omitted notify persists nothing and returns a delivery_choice_required guard directive. " +
			"Destructive payloads (rm/drop/delete) are routed to pending_approval and require operator approval outside this model-facing tool before they fire.",
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
	var head struct {
		Action string `json:"action"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return ToolResult{}, fmt.Errorf("task args: %w", err)
	}
	if head.Action == "" {
		return ToolResult{}, fmt.Errorf("task: action is required")
	}
	// The pool-free manifest path constructs a TaskTool with a nil Store (Spec only);
	// every action dereferences Store, so guard here rather than nil-panic mid-dispatch.
	if t.Store == nil {
		return ToolResult{}, fmt.Errorf("task %s: no task store is configured in this context", head.Action)
	}
	return t.actionRouter().Dispatch(ctx, head.Action, raw)
}

func (t *TaskTool) actionRouter() *ActionRouter {
	t.routerOnce.Do(func() {
		t.router = NewActionRouter(map[string]ActionFunc{
			"schedule": t.actionSchedule,
			"list":     t.actionList,
			"cancel":   t.actionCancel,
			"run_now":  t.actionRunNow,
		})
	})
	return t.router
}

func (t *TaskTool) alertThreshold() scoring.RiskTier {
	if t.AlertThreshold == "" {
		return scoring.Risky
	}
	return t.AlertThreshold
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
	if a.Kind == "" {
		return ToolResult{}, fmt.Errorf("task schedule: kind is required")
	}
	if strings.TrimSpace(a.Notify) == "" {
		return taskNotifyChoiceRequiredResult(), nil
	}
	spec, next, err := t.resolveSchedule(a)
	if err != nil {
		return ToolResult{}, err
	}

	tier := scoring.ComputeTaskTier(scoring.TaskArgs{
		Kind:         a.Kind,
		ScheduleKind: a.ScheduleKind,
		Payload:      a.Payload,
	})
	// The JSON-schema enum is advisory: an OpenAI-compat provider may pass anything
	// through. Reject it here, where the model can still correct itself.
	if !cron.ValidNotifyRoute(a.Notify) {
		return ToolResult{}, fmt.Errorf("task schedule: notify %q is not a delivery route (want none|whatsapp|email|stdout|telegram)", a.Notify)
	}

	status := "active"
	if scoring.GateRecommended(tier) {
		status = "pending_approval"
	}
	// AG-016: an agent_job runs a fresh FULL-tool agent turn at fire time, so a
	// benign-looking goal can still drive arbitrary later action. Gate EVERY
	// agent_job to pending_approval rather than trusting the rm/drop/delete keyword
	// score — the keyword check is necessary but not sufficient for a code-capable
	// scheduled run. reminders/backups keep their tier-based gating.
	if a.Kind == "agent_job" {
		status = "pending_approval"
	}

	// Capture the scheduling conversation id off the tool-call ctx (==a.sessionID,
	// set at llm_agent.go:470). The two-value form is bare-ctx-safe: a CLI/unit-test
	// ctx yields "" with no panic (mirror shellSessionKey, shell_exec.go:328-333).
	originConvID := ""
	if tc, ok := toolCallCtx(ctx); ok {
		originConvID = tc.sessionID
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
	})
	if err != nil {
		return ToolResult{}, fmt.Errorf("task schedule: %w", err)
	}

	// A pending_approval task must NOT fire until the operator approves it ON THE
	// CHANNEL it was scheduled from (HITL parity, no notify-route/env detour): return
	// the approval directive so the model relays it as an ask_user approval pause the
	// scheduled-task resume hook resolves — ask_user is the ONLY tool that pauses the
	// turn (amendment #51 / D-40), so the tool cannot pause directly.
	if status == "pending_approval" {
		immediate := scoring.RequiresImmediateAlert(tier, t.alertThreshold())
		return scheduledApprovalRequiredResult(created.ID, a.Kind, spec.summary(), string(tier), immediate), nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "scheduled task %s (kind=%s, %s, risk=%s, status=active)", created.ID, a.Kind, spec.summary(), tier)
	if !next.IsZero() {
		fmt.Fprintf(&b, "\nNext run at %s.", next.UTC().Format(time.RFC3339))
	}
	s := b.String()
	return ToolResult{Preview: s, Bytes: len(s)}, nil
}

func taskNotifyChoiceRequiredResult() ToolResult {
	payload := struct {
		Status   string              `json:"status"`
		Question string              `json:"question"`
		Options  []map[string]string `json:"options"`
		Message  string              `json:"message"`
	}{
		Status:   "delivery_choice_required",
		Question: "Where should the scheduled task deliver its result?",
		Options: []map[string]string{
			{"label": "No notification", "value": string(cron.RouteNone)},
			{"label": "This conversation", "value": string(cron.RouteStdout)},
			{"label": "Telegram", "value": string(cron.RouteTelegram)},
			{"label": "WhatsApp", "value": string(cron.RouteWhatsApp)},
			{"label": "Email", "value": string(cron.RouteEmail)},
		},
		Message: "No task was created. Call ask_user with kind=\"choice\", the question and options above. After the operator answers, call task action=\"schedule\" again with notify set to the selected option value.",
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"status":"delivery_choice_required"}`)
	}
	return ToolResult{Preview: string(raw), Bytes: len(raw)}
}

// scheduledApprovalRequiredResult builds the pending_approval directive (the
// shell_exec/gateway approval-required precedent): a scheduled task never fires until
// the operator approves it on the channel it was scheduled from. The model MUST relay
// this as an ask_user approval pause carrying the resume_context the scheduled-task
// resume hook decodes; on accept the hook flips the task to active. The task_id +
// authenticated origin conversation are the authorization (the hook owner-scopes the
// UPDATE), so no question-match challenge is needed here.
func scheduledApprovalRequiredResult(taskID, kind, summary, tier string, immediate bool) ToolResult {
	// The human-facing question masks the UUID to its first 8 chars (kind + summary already
	// identify the task) so the Sì/No prompt reads friendly instead of dumping a 36-char id;
	// the full task_id stays in the machine-facing resume_context below (the resume hook keys
	// on it). Bounded id + kind only — never the payload.
	question := fmt.Sprintf("Approve scheduled %s task %s (%s, risk=%s)? It will not fire until you approve.", kind, shortScheduledTaskID(taskID), summary, tier)
	payload := map[string]string{
		"status":   "pending_approval",
		"task_id":  taskID,
		"question": question,
		"message": "This scheduled task requires operator approval before it can fire. " +
			"Call ask_user with kind=\"approval\", question exactly equal to the question field, and " +
			"resume_context={\"type\":\"scheduled_task_approval\",\"task_id\":\"" + taskID + "\"}. " +
			"The task activates only after the operator accepts on their channel.",
	}
	if immediate {
		payload["immediate_alert"] = "true"
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		raw = []byte(`{"status":"pending_approval","task_id":"` + taskID + `"}`)
	}
	return ToolResult{Preview: string(raw), Bytes: len(raw)}
}

// shortScheduledTaskID renders the first 8 chars of a task id for the operator-facing approval
// prompt — a bounded, friendly handle instead of the raw 36-char UUID (the full id stays in the
// machine-facing resume_context). Parity with the sweep push's shortTelegramTaskID.
func shortScheduledTaskID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
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

// renderTaskList formats active + pending tasks, flagging awaiting-approval rows
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
