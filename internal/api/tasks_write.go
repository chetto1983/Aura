package api

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/aura/aura/internal/scheduler"
)

// UpsertTaskRequest is the JSON body for POST /tasks. Exactly one of `at`
// (RFC3339 UTC) or `daily` (HH:MM in the bot's local TZ) must be set —
// the constraints mirror the LLM-facing schedule_task tool, minus the
// reminder-from-user-context shortcut. Reminders posted via the API
// require recipient_id explicitly.
type UpsertTaskRequest struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Payload     string `json:"payload,omitempty"`
	RecipientID string `json:"recipient_id,omitempty"`
	At          string `json:"at,omitempty"`    // RFC3339 UTC
	Daily       string `json:"daily,omitempty"` // HH:MM, local TZ
}

func handleTaskUpsert(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req UpsertTaskRequest
		if err := decodeJSONBody(r, &req); err != nil {
			if errors.Is(err, io.EOF) {
				writeError(w, deps.Logger, http.StatusBadRequest, "request body required")
				return
			}
			writeError(w, deps.Logger, http.StatusBadRequest, "parse json: "+err.Error())
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		req.At = strings.TrimSpace(req.At)
		req.Daily = strings.TrimSpace(req.Daily)
		if !taskNameRe.MatchString(req.Name) {
			writeError(w, deps.Logger, http.StatusBadRequest, "name must be 1-64 chars [A-Za-z0-9_.-]")
			return
		}
		kind := scheduler.TaskKind(strings.TrimSpace(req.Kind))
		switch kind {
		case scheduler.KindReminder, scheduler.KindWikiMaintenance:
		default:
			writeError(w, deps.Logger, http.StatusBadRequest, "kind must be reminder or wiki_maintenance")
			return
		}
		if (req.At == "") == (req.Daily == "") {
			writeError(w, deps.Logger, http.StatusBadRequest, "set exactly one of at (RFC3339 UTC) or daily (HH:MM)")
			return
		}
		if kind == scheduler.KindReminder && strings.TrimSpace(req.RecipientID) == "" {
			writeError(w, deps.Logger, http.StatusBadRequest, "reminder kind requires recipient_id")
			return
		}

		loc := deps.Location
		if loc == nil {
			loc = time.Local
		}
		now := time.Now().UTC()

		task := &scheduler.Task{
			Name:        req.Name,
			Kind:        kind,
			Payload:     req.Payload,
			RecipientID: strings.TrimSpace(req.RecipientID),
			Status:      scheduler.StatusActive,
		}

		if req.At != "" {
			ts, err := time.Parse(time.RFC3339, req.At)
			if err != nil {
				writeError(w, deps.Logger, http.StatusBadRequest, "parse at: "+err.Error())
				return
			}
			ts = ts.UTC()
			if !ts.After(now) {
				writeError(w, deps.Logger, http.StatusBadRequest, "at must be in the future")
				return
			}
			task.ScheduleKind = scheduler.ScheduleAt
			task.ScheduleAt = ts
			task.NextRunAt = ts
		} else {
			next, err := scheduler.NextDailyRun(req.Daily, loc, now)
			if err != nil {
				writeError(w, deps.Logger, http.StatusBadRequest, err.Error())
				return
			}
			task.ScheduleKind = scheduler.ScheduleDaily
			task.ScheduleDaily = req.Daily
			task.NextRunAt = next
		}

		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		saved, err := deps.Scheduler.Upsert(ctx, task)
		if err != nil {
			deps.Logger.Warn("api: upsert task", "name", req.Name, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "upsert failed: "+err.Error())
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, taskDTO(saved))
	}
}

func handleTaskCancel(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := r.PathValue("name")
		if !taskNameRe.MatchString(name) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid task name")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		ok, err := deps.Scheduler.Cancel(ctx, name)
		if err != nil {
			deps.Logger.Warn("api: cancel task", "name", name, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "cancel failed: "+err.Error())
			return
		}
		if !ok {
			// Either the task doesn't exist or it's already in a terminal
			// status. Fetch and disambiguate so the UI can show a useful
			// message ("already cancelled" vs "no such task").
			rec, gerr := deps.Scheduler.GetByName(ctx, name)
			if gerr != nil {
				if errors.Is(gerr, sql.ErrNoRows) {
					writeError(w, deps.Logger, http.StatusNotFound, "task not found")
					return
				}
				writeError(w, deps.Logger, http.StatusInternalServerError, "lookup failed: "+gerr.Error())
				return
			}
			writeError(w, deps.Logger, http.StatusConflict,
				"task already in terminal status: "+string(rec.Status))
			return
		}
		rec, err := deps.Scheduler.GetByName(ctx, name)
		if err != nil {
			deps.Logger.Warn("api: cancel reread", "name", name, "error", err)
			writeJSON(w, deps.Logger, http.StatusOK, map[string]any{"ok": true, "name": name})
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, taskDTO(rec))
	}
}
