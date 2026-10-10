package agui

import (
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/google/uuid"
)

// governance_scheduler_read.go is the read half of the GOV-03 scheduler board: the task list,
// one task's run history, and their JSON projections. Split out of governance_api.go when the
// board gained its owner rule (prd.md §3, 2026-10-10): a member reads only their own tasks and runs,
// an admin reads every identity's.

// schedulerTaskRow is the safe wire projection for a scheduled task. It omits IdentityID and
// OriginConversationID, which are cross-identity linkage the board never renders.
//
// Payload is CARRIED, and it used to be stripped for the same "private prompt/context material"
// reason. That was wrong about whose privacy it protected: the board is the operator's own
// console, behind governance.read, showing the deployment's own schedule, and the payload is the
// text they dictated. Withholding it made the surface useless for the one person it is for —
// reported 2026-09-07: a reminder was unreadable until it fired on Telegram, and the edit dialog
// offered an empty box that would silently keep the old text, so changing it meant retyping it
// blind. json.RawMessage so the stored bytes reach the client unaltered and omitempty keeps the
// key off a task that carries none (a backup takes no payload).
type schedulerTaskRow struct {
	ID           string            `json:"ID"`
	Kind         cron.TaskKind     `json:"Kind"`
	ScheduleKind cron.ScheduleKind `json:"ScheduleKind"`
	CronExpr     string            `json:"CronExpr"`
	EveryMinutes int               `json:"EveryMinutes"`
	RunAt        time.Time         `json:"RunAt"`
	TZ           string            `json:"TZ"`
	StepBudget   int               `json:"StepBudget"`
	Status       string            `json:"Status"`
	NextRunAt    time.Time         `json:"NextRunAt"`
	NotifyRoute  string            `json:"NotifyRoute"`
	CreatedAt    time.Time         `json:"CreatedAt"`
	UpdatedAt    time.Time         `json:"UpdatedAt"`
	Payload      json.RawMessage   `json:"Payload,omitempty"`
	// Cancellable tells the board whether to offer delete and pause, so the rule that keeps the
	// database backup (cron.IsCancellableKind) is stated once, here, not mirrored in TS.
	Cancellable bool `json:"Cancellable"`
	// PausedReason is "operator" or "failures" on a paused task, absent otherwise.
	PausedReason string `json:"PausedReason,omitempty"`
	// ConsecutiveFailures is how many runs failed since the last success.
	ConsecutiveFailures int `json:"ConsecutiveFailures"`
}

// schedulerRunRow is the safe run-history projection. It omits PausedStateToken while
// preserving LastHeartbeatAt for the read-only operator board; operator-visible text
// fields are sanitized before serialization.
type schedulerRunRow struct {
	ID                string    `json:"ID"`
	TaskID            string    `json:"TaskID"`
	Status            string    `json:"Status"`
	StepBudget        int       `json:"StepBudget"`
	StartedAt         time.Time `json:"StartedAt"`
	LastHeartbeatAt   time.Time `json:"LastHeartbeatAt"`
	CompletedWithHash string    `json:"CompletedWithHash"`
	Summary           string    `json:"Summary"`
	LastError         string    `json:"LastError"`
	MissedSince       time.Time `json:"MissedSince"`
	CompletedAt       time.Time `json:"CompletedAt"`
}

// handleSchedulerList serves GET /api/governance/scheduler (GOV-03): the manageable
// tasks (active + pending_approval) ordered by next fire via ListManageableTasks, so the
// cockpit can approve a gated task on-screen. Read-only — mutates nothing. An unwired
// provider → 503; a backend error → sanitized 502; no tasks → 200 {tasks: []}.
func (s *Server) handleSchedulerList(w http.ResponseWriter, r *http.Request) {
	if s.governance.Scheduler == nil {
		http.Error(w, "scheduler board not configured", http.StatusServiceUnavailable)
		return
	}
	tasks, err := s.governance.Scheduler.ListManageableTasks(r.Context())
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	viewer := s.schedulerViewerOf(r)
	tasks = slices.DeleteFunc(tasks, func(task cron.Task) bool { return !viewer.sees(task) })
	writeJSON(w, map[string]any{"tasks": schedulerTaskRows(tasks)})
}

// handleSchedulerRuns serves GET /api/governance/scheduler/{id}/runs (GOV-03): the
// paginated, newest-first run history for one task. The {id} is uuid-guarded to a clean 404
// BEFORE any store call (a non-UUID id can never identify a task — R3), then ?limit (default
// 25) / ?offset (default 0) drive ListRunsForTask. Read-only — mutates nothing. An unwired
// provider → 503; a backend error → sanitized 502; a valid id with no runs → 200 {runs: []}.
func (s *Server) handleSchedulerRuns(w http.ResponseWriter, r *http.Request) {
	_, id, ok := s.schedulerTask(w, r)
	if !ok {
		return
	}
	limit := defaultRunHistoryLimit
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if n := parseLimit(raw); n > 0 {
			limit = n
		}
	}
	offset := defaultRunHistoryOffset
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if n := parseOffset(raw); n > 0 {
			offset = n
		}
	}
	runs, err := s.governance.Scheduler.ListRunsForTask(r.Context(), id, limit, offset)
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	if runs == nil {
		runs = []cron.Run{}
	}
	writeJSON(w, map[string]any{"runs": schedulerRunRows(runs)})
}

// schedulerPayload passes the stored payload through when it is valid JSON, and drops it
// otherwise. Dropping rather than forwarding is deliberate: the field is typed
// json.RawMessage, so malformed bytes would make the WHOLE response unencodable and cost the
// board every row to save one.
func schedulerPayload(raw []byte) json.RawMessage {
	if len(raw) == 0 || !json.Valid(raw) {
		return nil
	}
	return json.RawMessage(append([]byte(nil), raw...))
}

func schedulerTaskRows(tasks []cron.Task) []schedulerTaskRow {
	out := make([]schedulerTaskRow, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, schedulerTaskRow{
			ID:           task.ID,
			Kind:         task.Kind,
			ScheduleKind: task.ScheduleKind,
			CronExpr:     task.CronExpr,
			EveryMinutes: task.EveryMinutes,
			RunAt:        task.RunAt,
			TZ:           task.TZ,
			StepBudget:   task.StepBudget,
			Status:       task.Status,
			NextRunAt:    task.NextRunAt,
			NotifyRoute:  task.NotifyRoute,
			CreatedAt:    task.CreatedAt,
			UpdatedAt:    task.UpdatedAt,
			Payload:      schedulerPayload(task.Payload),
			Cancellable:  cron.IsCancellableKind(task.Kind),

			PausedReason:        task.PausedReason,
			ConsecutiveFailures: task.ConsecutiveFailures,
		})
	}
	return out
}

func schedulerRunRows(runs []cron.Run) []schedulerRunRow {
	out := make([]schedulerRunRow, 0, len(runs))
	for _, run := range runs {
		out = append(out, schedulerRunRow{
			ID:                run.ID,
			TaskID:            run.TaskID,
			Status:            run.Status,
			StepBudget:        run.StepBudget,
			StartedAt:         run.StartedAt,
			LastHeartbeatAt:   run.LastHeartbeatAt,
			CompletedWithHash: run.CompletedWithHash,
			Summary:           SanitizeString(run.Summary),
			LastError:         SanitizeString(run.LastError),
			MissedSince:       run.MissedSince,
			CompletedAt:       run.CompletedAt,
		})
	}
	return out
}

// parseTaskID resolves the {id} path param to a clean 404 BEFORE any store call: a
// non-UUID id can never identify an existing scheduler task, so it writes the 404 (R3)
// rather than leaking the store's parse error as a 500. Mirrors parseConvID
// (conversations_api.go) for the scheduler subtree.
func parseTaskID(w http.ResponseWriter, r *http.Request) (string, bool) {
	id := r.PathValue("id")
	if _, err := uuid.Parse(id); err != nil {
		http.Error(w, "scheduler task not found", http.StatusNotFound)
		return "", false
	}
	return id, true
}

// parseOffset reads the ?offset= query value, falling back to 0 when it is absent,
// non-numeric, or negative. The store clamps both limit and offset to a safe int32 range.
func parseOffset(raw string) int {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// schedulerViewer is who reads or manages the board. Every identity holds governance.write
// (D-01), so the scheduler routes alone let a member list, rewrite and run any identity's task,
// the backup included, and a run executes as the task's owner (prd.md §3, 2026-10-10, measured in
// docs/superpowers/verification/2026-10-10-member-scheduler-and-skills.md). An admin sees every
// task; anyone else only their own.
type schedulerViewer struct {
	admin    bool
	identity string
}

func (s *Server) schedulerViewerOf(r *http.Request) schedulerViewer {
	identity, _ := principalIdentityID(r)
	return schedulerViewer{admin: s.callerIsAdmin(r), identity: identity}
}

func (v schedulerViewer) sees(task cron.Task) bool {
	return v.admin || (v.identity != "" && task.IdentityID == v.identity)
}

// schedulerTask resolves the {id} task the caller may see, writing the response itself when it
// may not: 503 unwired, 404 for a malformed id, a missing task or another identity's task (the
// two are indistinguishable on purpose), 502 for a store error.
func (s *Server) schedulerTask(w http.ResponseWriter, r *http.Request) (cron.Task, string, bool) {
	if s.governance.Scheduler == nil {
		http.Error(w, "scheduler board not configured", http.StatusServiceUnavailable)
		return cron.Task{}, "", false
	}
	id, ok := parseTaskID(w, r)
	if !ok {
		return cron.Task{}, "", false
	}
	task, err := s.governance.Scheduler.GetTask(r.Context(), id)
	if errors.Is(err, cron.ErrTaskNotFound) || (err == nil && !s.schedulerViewerOf(r).sees(task)) {
		writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return cron.Task{}, "", false
	}
	if err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return cron.Task{}, "", false
	}
	return task, id, true
}
