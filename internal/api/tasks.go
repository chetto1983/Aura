package api

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/aura/aura/internal/scheduler"
)

func handleTaskList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		statusFilter := scheduler.Status(r.URL.Query().Get("status"))
		if statusFilter != "" && !validTaskStatus(statusFilter) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid status")
			return
		}
		records, err := deps.Scheduler.List(r.Context(), statusFilter)
		if err != nil {
			deps.Logger.Warn("api: list tasks", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list tasks")
			return
		}
		out := make([]Task, 0, len(records))
		for _, rec := range records {
			out = append(out, taskDTO(rec))
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func handleTaskGet(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !taskNameRe.MatchString(name) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid task name")
			return
		}
		rec, err := deps.Scheduler.GetByName(r.Context(), name)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, deps.Logger, http.StatusNotFound, "task not found")
				return
			}
			deps.Logger.Warn("api: get task", "name", name, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read task")
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, taskDTO(rec))
	}
}

func taskDTO(t *scheduler.Task) Task {
	dto := Task{
		Name:                 t.Name,
		Kind:                 string(t.Kind),
		Payload:              t.Payload,
		RecipientID:          t.RecipientID,
		ScheduleKind:         string(t.ScheduleKind),
		ScheduleDaily:        t.ScheduleDaily,
		ScheduleWeekdays:     t.ScheduleWeekdays,
		ScheduleEveryMinutes: t.ScheduleEveryMinutes,
		NextRunAt:            t.NextRunAt.UTC(),
		LastError:            t.LastError,
		LastOutput:           t.LastOutput,
		LastMetricsJSON:      t.LastMetricsJSON,
		WakeSignature:        t.WakeSignature,
		Status:               string(t.Status),
		CreatedAt:            t.CreatedAt.UTC(),
		UpdatedAt:            t.UpdatedAt.UTC(),
	}
	if !t.ScheduleAt.IsZero() {
		at := t.ScheduleAt.UTC()
		dto.ScheduleAt = &at
	}
	if !t.LastRunAt.IsZero() {
		lr := t.LastRunAt.UTC()
		dto.LastRunAt = &lr
	}
	return dto
}

func validTaskStatus(s scheduler.Status) bool {
	switch s {
	case scheduler.StatusActive, scheduler.StatusDone, scheduler.StatusCancelled, scheduler.StatusFailed:
		return true
	}
	return false
}
