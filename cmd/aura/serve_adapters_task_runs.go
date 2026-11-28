package main

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/agent/tools"
)

// ListTaskRuns reads the runs of a task the calling identity owns, newest first. A task of
// another identity reads as one with no runs, exactly as an unknown id does, so the history
// action never tells one owner what another's tasks produced. No status filter: a cancelled
// task's runs are what an operator asks about after replacing it (measured 2026-11-28).
func (s *cronTaskStore) ListTaskRuns(ctx context.Context, id string, limit int) ([]tools.TaskRun, error) {
	identityID, err := taskIdentity(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx, `
		SELECT r.started_at, r.status, COALESCE(r.summary, ''), COALESCE(r.last_error, '')
		FROM aura.agent_job_runs AS r
		JOIN aura.scheduler_tasks AS t ON t.id = r.task_id
		WHERE t.id = $1::uuid
		  AND t.identity_id = $2
		ORDER BY r.started_at DESC
		LIMIT $3`, id, identityID, limit)
	if err != nil {
		return nil, fmt.Errorf("list runs of task %s: %w", id, err)
	}
	defer rows.Close()

	var out []tools.TaskRun
	for rows.Next() {
		var r tools.TaskRun
		if err := rows.Scan(&r.StartedAt, &r.Status, &r.Summary, &r.LastError); err != nil {
			return nil, fmt.Errorf("scan run of task %s: %w", id, err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list runs of task %s: %w", id, err)
	}
	return out, nil
}
