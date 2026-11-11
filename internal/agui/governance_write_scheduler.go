package agui

// governance_write_scheduler.go is the GOV-03 write adapter over the SchedulerBoardProvider
// seam: the operator management verbs for a scheduled task — approve a gated task, run it
// now, pause or resume it, cancel it, or reschedule/re-payload it. Every handler resolves the
// task the caller may manage (schedulerTask: their own, or any for an admin; another
// identity's task answers 404 like a missing one), enforces the system-kind guard (a
// system-seeded sweep is never operator-mutable → 403, and neither backup is ever paused
// or cancelled → 403), makes ONE provider call, and projects JSON. The parent-mux mount behind
// RequireCapability(governance.write) is cmd/aura/serve_webui.go's job; every wire error passes
// through sanitizeErr. There is no business logic here — schedule grammar
// validation reuses the shipped cron engine (ParseSchedule + FirstFire), exactly like the
// model-facing `task` tool.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/chetto1983/aura/internal/cron"
	"github.com/chetto1983/aura/internal/identityctx"
)

// schedulerEditBodyCap bounds the edit request body: a schedule grammar + a short payload
// object comfortably fits 8 KiB (a reminder text / agent_job goal).
const schedulerEditBodyCap = 8 << 10

// registerGovernanceSchedulerWriteRoutes mounts the GOV-03 write verbs on the supplied mux
// using Go 1.22 method-pattern routing — SPECIFIC method+path siblings under the /api/
// carve-out. The {id}/approve, {id}/run, {id}/pause and {id}/resume action routes are more
// specific than the {id} cancel/edit routes, so longest-pattern precedence keeps them distinct.
func (s *Server) registerGovernanceSchedulerWriteRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/governance/scheduler/{id}/approve", s.handleSchedulerApprove)
	mux.HandleFunc("POST /api/governance/scheduler/{id}/run", s.handleSchedulerRun)
	mux.HandleFunc("POST /api/governance/scheduler/{id}/pause", s.handleSchedulerPause)
	mux.HandleFunc("POST /api/governance/scheduler/{id}/resume", s.handleSchedulerResume)
	mux.HandleFunc("DELETE /api/governance/scheduler/{id}", s.handleSchedulerCancel)
	mux.HandleFunc("PATCH /api/governance/scheduler/{id}", s.handleSchedulerEdit)
}

// schedulerMutable resolves the {id} task the caller may manage (schedulerTask: their own, or any
// for an admin) and refuses a system-seeded sweep with 403: those are never operator-mutable.
// It returns the resolved task, its id, and ok=false when it has already written a response.
func (s *Server) schedulerMutable(w http.ResponseWriter, r *http.Request) (cron.Task, string, bool) {
	task, id, ok := s.schedulerTask(w, r)
	if !ok {
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
	task, id, ok := s.schedulerMutable(w, r)
	if !ok {
		return
	}
	// A message is approved by reviewing its text, which this route never shows.
	if task.Kind == cron.KindMessage {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "approve it from the message card"})
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
	task, id, ok := s.schedulerMutable(w, r)
	if !ok || refuseMessageChange(w, task) {
		return
	}
	if err := s.governance.Scheduler.RunTaskNow(r.Context(), id); err != nil {
		s.writeSchedulerMutateErr(w, err, "task is not active")
		return
	}
	writeJSON(w, map[string]string{"status": "queued"})
}

// handleSchedulerPause serves POST /api/governance/scheduler/{id}/pause: stops an active task
// from firing until it is resumed. A backup is never paused (403).
func (s *Server) handleSchedulerPause(w http.ResponseWriter, r *http.Request) {
	task, id, ok := s.schedulerMutable(w, r)
	if !ok {
		return
	}
	if !cron.IsPausableKind(task.Kind) {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": "this task cannot be paused"})
		return
	}
	if err := s.governance.Scheduler.PauseTask(r.Context(), id); err != nil {
		s.writeSchedulerMutateErr(w, err, "task is not active")
		return
	}
	writeJSON(w, map[string]string{"status": "paused"})
}

// handleSchedulerResume serves POST /api/governance/scheduler/{id}/resume: reactivates a paused
// task at its next fire from now. A one-shot whose time has passed cannot resume → 409.
func (s *Server) handleSchedulerResume(w http.ResponseWriter, r *http.Request) {
	_, id, ok := s.schedulerMutable(w, r)
	if !ok {
		return
	}
	next, err := s.governance.Scheduler.ResumeTask(r.Context(), id, time.Now())
	if errors.Is(err, cron.ErrResumePastOneShot) {
		writeJSONStatus(w, http.StatusConflict, map[string]string{"error": cron.ErrResumePastOneShot.Error()})
		return
	}
	if err != nil {
		s.writeSchedulerMutateErr(w, err, "task is not paused")
		return
	}
	writeJSON(w, map[string]string{"status": "active", "next_run_at": next.UTC().Format(time.RFC3339)})
}

// handleSchedulerCancel serves DELETE /api/governance/scheduler/{id}: soft-cancels a task
// (status='cancelled'). Cancelling a manageable task is idempotent; a backup is never
// cancellable (403).
func (s *Server) handleSchedulerCancel(w http.ResponseWriter, r *http.Request) {
	task, id, ok := s.schedulerMutable(w, r)
	if !ok {
		return
	}
	if !cron.IsCancellableKind(task.Kind) {
		writeJSONStatus(w, http.StatusForbidden, map[string]string{"error": "a backup cannot be cancelled"})
		return
	}
	// The draft is declined before the task is cancelled (decision 15). A failed decline
	// cancels nothing, so the operator's retry declines, then cancels.
	if task.Kind == cron.KindMessage {
		if s.messageCanceller == nil {
			writeJSONStatus(w, http.StatusServiceUnavailable, map[string]string{"error": "scheduled messages are not configured"})
			return
		}
		if err := s.messageCanceller.DeclineForTask(r.Context(), task.IdentityID, id); err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
			return
		}
	}
	if task.Status == "pending_approval" && task.OriginConversationID != "" && s.approvalExpirer != nil {
		answer := fmt.Sprintf("Scheduled %s task %s was cancelled by the operator; it will not run.", task.Kind, shortID(id))
		ownerCtx := identityctx.WithIdentityID(r.Context(), task.IdentityID)
		if _, err := s.approvalExpirer.ExpireApprovalPause(ownerCtx, task.OriginConversationID, id, answer); err != nil {
			writeJSONStatus(w, http.StatusBadGateway, map[string]string{"error": sanitizeErr(err)})
			return
		}
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
	if !ok || refuseMessageChange(w, task) {
		return
	}
	body, err := decodeSchedulerEditBody(w, r)
	if err != nil {
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

func decodeSchedulerEditBody(w http.ResponseWriter, r *http.Request) (schedulerEditBody, error) {
	var body schedulerEditBody
	err := strictDecodeJSON(w, r, &body, decodeOpts{maxBytes: schedulerEditBodyCap})
	return body, err
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

// ScheduledMessageCanceller declines a message task's draft; *messagedrafts.Store satisfies it.
type ScheduledMessageCanceller interface {
	DeclineForTask(ctx context.Context, ownerID, taskID string) error
}

// SetScheduledMessageCanceller wires the draft decline a message task's cancel runs first.
func (s *Server) SetScheduledMessageCanceller(c ScheduledMessageCanceller) { s.messageCanceller = c }

// refuseMessageChange answers a run-now or an edit of a message task: its text was reviewed for
// its time, so it is cancelled and scheduled again, never moved or rewritten.
func refuseMessageChange(w http.ResponseWriter, task cron.Task) bool {
	if task.Kind != cron.KindMessage {
		return false
	}
	writeJSONStatus(w, http.StatusConflict, map[string]string{"error": "a scheduled message cannot be run now or edited; cancel it and schedule a new one"})
	return true
}

// ApprovalPauseExpirer closes a task's pending approval when the task is cancelled; the runner
// satisfies it. The result is a cron.PauseClosed; the board needs only the error.
type ApprovalPauseExpirer interface {
	ExpireApprovalPause(ctx context.Context, conversationID, taskID, answer string) (cron.PauseClosed, error)
}

// SetApprovalPauseExpirer wires the approval close a pending task's cancel runs first.
func (s *Server) SetApprovalPauseExpirer(e ApprovalPauseExpirer) { s.approvalExpirer = e }

func shortID(id string) string {
	if len(id) > 8 {
		return id[:8]
	}
	return id
}
