package agui

// governance_write_scheduler.go is the GOV-03 write adapter over the SchedulerBoardProvider
// seam: the operator management verbs for a scheduled task — approve a gated task, run it
// now, cancel it, or reschedule/re-payload it. Every handler nil-checks the provider (503
// when unwired), resolves the task, enforces the system-kind guard (a system-seeded sweep is
// never operator-mutable → 403), makes ONE provider call, and projects JSON. The parent-mux
// mount behind RequireCapability(governance.write) is cmd/aura/serve_webui.go's job; every
// wire error passes through sanitizeErr. There is no business logic here — schedule grammar
// validation reuses the shipped cron engine (ParseSchedule + FirstFire), exactly like the
// model-facing `task` tool.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/cron"
)

// schedulerEditBodyCap bounds the edit request body: a schedule grammar + a short payload
// object comfortably fits 8 KiB (a reminder text / agent_job goal).
const schedulerEditBodyCap = 8 << 10

// registerGovernanceSchedulerWriteRoutes mounts the GOV-03 write verbs on the supplied mux
// using Go 1.22 method-pattern routing — SPECIFIC method+path siblings under the /api/
// carve-out. The {id}/approve and {id}/run action routes are more specific than the {id}
// cancel/edit routes, so longest-pattern precedence keeps them distinct.
func (s *Server) registerGovernanceSchedulerWriteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/governance/scheduler/{id}/approve", s.handleSchedulerApprove)
	mux.HandleFunc("POST /api/governance/scheduler/{id}/run", s.handleSchedulerRun)
	mux.HandleFunc("DELETE /api/governance/scheduler/{id}", s.handleSchedulerCancel)
	mux.HandleFunc("PATCH /api/governance/scheduler/{id}", s.handleSchedulerEdit)
}

// schedulerMutable resolves the {id} task and enforces the write preconditions shared by
// every verb: provider wired (else 503), valid uuid (else 404 via parseTaskID), task exists
// (else 404), and the kind is operator-manageable (else 403 — a system sweep is off-limits).
// It returns the resolved task, its id, and ok=false when it has already written a response.
func (s *Server) schedulerMutable(w http.ResponseWriter, r *http.Request) (cron.Task, string, bool) {
	if s.governance.Scheduler == nil {
		http.Error(w, "scheduler board not configured", http.StatusServiceUnavailable)
		return cron.Task{}, "", false
	}
	id, ok := parseTaskID(w, r)
	if !ok {
		return cron.Task{}, "", false
	}
	task, err := s.governance.Scheduler.GetTask(r.Context(), id)
	if err != nil {
		if errors.Is(err, cron.ErrTaskNotFound) {
			writeJSONStatus(w, http.StatusNotFound, map[string]string{"error": "task not found"})
			return cron.Task{}, "", false
		}
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return cron.Task{}, "", false
	}
	if !cron.IsUserManageableKind(task.Kind) {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": "system task is not operator-manageable"})
		return cron.Task{}, "", false
	}
	return task, id, true
}

// handleSchedulerApprove serves POST /api/governance/scheduler/{id}/approve: flips a
// pending_approval task to active (the cockpit parity of the on-channel HITL accept).
func (s *Server) handleSchedulerApprove(w http.ResponseWriter, r *http.Request) {
	_, id, ok := s.schedulerMutable(w, r)
	if !ok {
		return
	}
	if err := s.governance.Scheduler.ApproveTask(r.Context(), id); err != nil {
		s.writeSchedulerMutateErr(w, err, "task is not awaiting approval")
		return
	}
	writeJSON(w, map[string]string{"status": "active"})
}

// handleSchedulerRun serves POST /api/governance/scheduler/{id}/run: advances an active
// task's next fire to now so the next tick claims it.
func (s *Server) handleSchedulerRun(w http.ResponseWriter, r *http.Request) {
	_, id, ok := s.schedulerMutable(w, r)
	if !ok {
		return
	}
	if err := s.governance.Scheduler.RunTaskNow(r.Context(), id); err != nil {
		s.writeSchedulerMutateErr(w, err, "task is not active")
		return
	}
	writeJSON(w, map[string]string{"status": "queued"})
}

// handleSchedulerCancel serves DELETE /api/governance/scheduler/{id}: soft-cancels a task
// (status='cancelled'). Cancelling a manageable task is idempotent.
func (s *Server) handleSchedulerCancel(w http.ResponseWriter, r *http.Request) {
	_, id, ok := s.schedulerMutable(w, r)
	if !ok {
		return
	}
	if err := s.governance.Scheduler.CancelTask(r.Context(), id); err != nil {
		writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
		return
	}
	writeJSON(w, map[string]string{"status": "cancelled"})
}

// schedulerEditBody is the reschedule/re-payload request. The schedule grammar mirrors the
// `task` tool (at|every|cron); an omitted payload keeps the task's current one.
type schedulerEditBody struct {
	ScheduleKind string          `json:"schedule_kind"`
	Cron         string          `json:"cron"`
	At           string          `json:"at"`
	EveryMinutes int             `json:"every_minutes"`
	TZ           string          `json:"tz"`
	Payload      json.RawMessage `json:"payload"`
	Notify       string          `json:"notify"`
}

// handleSchedulerEdit serves PATCH /api/governance/scheduler/{id}: revalidates the schedule
// grammar via the shipped cron engine, recomputes the first fire, and rewrites the task's
// schedule + payload + notify route. A bad grammar → 400; a task no longer editable → 409.
func (s *Server) handleSchedulerEdit(w http.ResponseWriter, r *http.Request) {
	task, id, ok := s.schedulerMutable(w, r)
	if !ok {
		return
	}
	var body schedulerEditBody
	dec := json.NewDecoder(io.LimitReader(r.Body, schedulerEditBodyCap))
	if err := dec.Decode(&body); err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	// Reject an unknown route instead of storing it: the notifier degrades anything it
	// does not recognise to stdout, so a typo in the cockpit dropdown would be accepted
	// with a 200 and then silently deliver nowhere the operator is looking.
	if !cron.ValidNotifyRoute(body.Notify) {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": "invalid notify route"})
		return
	}
	spec, next, err := resolveEditSchedule(body)
	if err != nil {
		writeJSONStatus(w, http.StatusBadRequest, map[string]string{"error": sanitizeErr(err)})
		return
	}
	payload := []byte(body.Payload)
	if len(payload) == 0 {
		payload = task.Payload // an omitted payload keeps the current one (schedule-only edit).
	}
	if err := s.governance.Scheduler.UpdateTask(r.Context(), id, cron.UpdateTaskParams{
		Spec:        spec,
		NextRunAt:   next,
		Payload:     payload,
		NotifyRoute: body.Notify,
	}); err != nil {
		s.writeSchedulerMutateErr(w, err, "task is not editable")
		return
	}
	writeJSON(w, map[string]string{
		"status":      task.Status,
		"next_run_at": next.UTC().Format(time.RFC3339),
	})
}

// resolveEditSchedule validates the at|every|cron grammar via the shipped cron engine and
// computes the first fire — the byte-identical validation the `task` tool runs at schedule
// time (ParseSchedule gates the cron expr; FirstFire is DST-safe).
func resolveEditSchedule(b schedulerEditBody) (cron.ScheduleSpec, time.Time, error) {
	var at time.Time
	if b.ScheduleKind == "at" {
		parsed, err := time.Parse(time.RFC3339, b.At)
		if err != nil {
			return cron.ScheduleSpec{}, time.Time{}, fmt.Errorf("at must be RFC-3339: %w", err)
		}
		at = parsed
	}
	spec, err := cron.ParseSchedule(b.ScheduleKind, b.Cron, b.EveryMinutes, at, b.TZ)
	if err != nil {
		return cron.ScheduleSpec{}, time.Time{}, err
	}
	next, err := cron.FirstFire(spec, time.Now())
	if err != nil {
		return cron.ScheduleSpec{}, time.Time{}, err
	}
	return spec, next, nil
}

// writeSchedulerMutateErr maps a store mutate failure: ErrTaskNotFound AFTER the
// schedulerMutable guard means the task exists but its status no longer permits the verb
// (e.g. approve on an already-active task) → 409 with the verb-specific message; anything
// else is a sanitized 502.
func (s *Server) writeSchedulerMutateErr(w http.ResponseWriter, err error, conflictMsg string) {
	if errors.Is(err, cron.ErrTaskNotFound) {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": conflictMsg})
		return
	}
	writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
}
