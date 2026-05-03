package api

import (
	"database/sql"
	"errors"
	"net/http"
	"strconv"
	"time"

	"github.com/aura/aura/internal/swarm"
)

func handleSwarmRunList(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.Swarm == nil {
			writeJSON(w, deps.Logger, http.StatusOK, []SwarmRunSummary{})
			return
		}
		limit := 50
		if raw := r.URL.Query().Get("limit"); raw != "" {
			n, err := strconv.Atoi(raw)
			if err != nil || n <= 0 {
				writeError(w, deps.Logger, http.StatusBadRequest, "invalid limit")
				return
			}
			limit = n
		}
		runs, err := deps.Swarm.ListRuns(r.Context(), limit)
		if err != nil {
			deps.Logger.Warn("api: list swarm runs", "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list swarm runs")
			return
		}
		out := make([]SwarmRunSummary, 0, len(runs))
		for _, run := range runs {
			tasks, err := deps.Swarm.ListTasks(r.Context(), run.ID)
			if err != nil {
				deps.Logger.Warn("api: list swarm run tasks", "run_id", run.ID, "error", err)
				writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list swarm run tasks")
				return
			}
			out = append(out, swarmRunSummaryDTO(run, tasks))
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func handleSwarmRunGet(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !swarmRunIDRe.MatchString(id) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid swarm run id")
			return
		}
		if deps.Swarm == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "swarm run not found")
			return
		}
		run, err := deps.Swarm.GetRun(r.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, deps.Logger, http.StatusNotFound, "swarm run not found")
				return
			}
			deps.Logger.Warn("api: get swarm run", "run_id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read swarm run")
			return
		}
		tasks, err := deps.Swarm.ListTasks(r.Context(), id)
		if err != nil {
			deps.Logger.Warn("api: list swarm tasks", "run_id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to list swarm tasks")
			return
		}
		out := SwarmRunDetail{
			SwarmRunSummary: swarmRunSummaryDTO(*run, tasks),
			Tasks:           swarmTaskDTOs(tasks),
		}
		writeJSON(w, deps.Logger, http.StatusOK, out)
	}
}

func handleSwarmTaskGet(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if !swarmTaskIDRe.MatchString(id) {
			writeError(w, deps.Logger, http.StatusBadRequest, "invalid swarm task id")
			return
		}
		if deps.Swarm == nil {
			writeError(w, deps.Logger, http.StatusNotFound, "swarm task not found")
			return
		}
		task, err := deps.Swarm.GetTask(r.Context(), id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				writeError(w, deps.Logger, http.StatusNotFound, "swarm task not found")
				return
			}
			deps.Logger.Warn("api: get swarm task", "task_id", id, "error", err)
			writeError(w, deps.Logger, http.StatusInternalServerError, "failed to read swarm task")
			return
		}
		writeJSON(w, deps.Logger, http.StatusOK, swarmTaskDTO(*task))
	}
}

func swarmRunSummaryDTO(run swarm.Run, tasks []swarm.Task) SwarmRunSummary {
	out := SwarmRunSummary{
		ID:          run.ID,
		Goal:        run.Goal,
		Status:      string(run.Status),
		CreatedBy:   run.CreatedBy,
		CreatedAt:   run.CreatedAt.UTC(),
		UpdatedAt:   run.UpdatedAt.UTC(),
		CompletedAt: utcTimePtr(run.CompletedAt),
		LastError:   run.LastError,
	}
	for _, task := range tasks {
		out.TaskCounts.Total++
		switch task.Status {
		case swarm.TaskPending:
			out.TaskCounts.Pending++
		case swarm.TaskRunning:
			out.TaskCounts.Running++
		case swarm.TaskCompleted:
			out.TaskCounts.Completed++
		case swarm.TaskFailed:
			out.TaskCounts.Failed++
		}
		out.Metrics.LLMCalls += task.LLMCalls
		out.Metrics.ToolCalls += task.ToolCalls
		out.Metrics.TokensPrompt += task.TokensPrompt
		out.Metrics.TokensCompletion += task.TokensCompletion
		out.Metrics.TokensTotal += task.TokensTotal
		out.Metrics.TaskElapsedMS += task.ElapsedMS
	}
	if run.CompletedAt != nil {
		out.Metrics.WallMS = run.CompletedAt.Sub(run.CreatedAt).Milliseconds()
	}
	if out.Metrics.WallMS > 0 {
		out.Metrics.Speedup = float64(out.Metrics.TaskElapsedMS) / float64(out.Metrics.WallMS)
	}
	return out
}

func swarmTaskDTOs(tasks []swarm.Task) []SwarmTask {
	out := make([]SwarmTask, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, swarmTaskDTO(task))
	}
	return out
}

func swarmTaskDTO(task swarm.Task) SwarmTask {
	return SwarmTask{
		ID:               task.ID,
		RunID:            task.RunID,
		ParentID:         task.ParentID,
		Role:             task.Role,
		Subject:          task.Subject,
		Status:           string(task.Status),
		Depth:            task.Depth,
		Attempts:         task.Attempts,
		ToolAllowlist:    task.ToolAllowlist,
		BlockedBy:        task.BlockedBy,
		Result:           task.Result,
		LastError:        task.LastError,
		LLMCalls:         task.LLMCalls,
		ToolCalls:        task.ToolCalls,
		TokensPrompt:     task.TokensPrompt,
		TokensCompletion: task.TokensCompletion,
		TokensTotal:      task.TokensTotal,
		ElapsedMS:        task.ElapsedMS,
		CreatedAt:        task.CreatedAt.UTC(),
		StartedAt:        utcTimePtr(task.StartedAt),
		CompletedAt:      utcTimePtr(task.CompletedAt),
	}
}

func utcTimePtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	utc := t.UTC()
	return &utc
}
