package runs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) CreateOrGetRun(ctx context.Context, params CreateRunParams) (Run, bool, error) {
	if params.IdempotencyKey != "" {
		if runID, ok, err := s.runIDForIdempotencyKey(ctx, "inbound", params.IdempotencyKey); err != nil {
			return Run{}, false, err
		} else if ok {
			run, err := s.GetRun(ctx, runID)
			return run, false, err
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Run{}, false, fmt.Errorf("runs: begin create run: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	run, err := s.insertRun(ctx, tx, params)
	if err != nil {
		return Run{}, false, err
	}
	if params.IdempotencyKey != "" {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO run_idempotency_keys (scope, key, run_id, created_at) VALUES (?, ?, ?, ?)`,
			"inbound",
			params.IdempotencyKey,
			run.ID,
			formatTime(run.StartedAt),
		); err != nil {
			return Run{}, false, fmt.Errorf("runs: record inbound idempotency key: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return Run{}, false, fmt.Errorf("runs: commit create run: %w", err)
	}
	return run, true, nil
}

func (s *Store) GetRun(ctx context.Context, runID string) (Run, error) {
	var run Run
	if err := s.db.QueryRowContext(ctx, `
SELECT id, parent_run_id, thread_id, principal_id, actor_id, channel, status, model, started_at, updated_at,
       completed_at, cancelled_at, last_error, current_seq, idempotency_key, correlation_id,
       trace_id, span_id, final_text_preview, stats_json, metadata_json
FROM runs
WHERE id = ?`, runID).Scan(runScanDest(&run)...); err != nil {
		return Run{}, fmt.Errorf("runs: get run %q: %w", runID, err)
	}
	return run, nil
}

func (s *Store) runIDForIdempotencyKey(ctx context.Context, scope, key string) (string, bool, error) {
	var runID string
	err := s.db.QueryRowContext(ctx, `SELECT run_id FROM run_idempotency_keys WHERE scope = ? AND key = ?`, scope, key).Scan(&runID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("runs: lookup idempotency key: %w", err)
	}
	return runID, true, nil
}

func (s *Store) insertRun(ctx context.Context, tx *sql.Tx, params CreateRunParams) (Run, error) {
	if params.Channel == "" {
		return Run{}, errors.New("runs: channel is required")
	}
	if params.Status == "" {
		return Run{}, errors.New("runs: status is required")
	}
	startedAt := params.StartedAt
	if startedAt.IsZero() {
		startedAt = s.now()
	}
	metadataJSON, err := encodeJSON(params.Metadata, "{}")
	if err != nil {
		return Run{}, fmt.Errorf("runs: encode run metadata: %w", err)
	}
	run := Run{
		ID:             firstNonEmpty(params.ID, newID("run")),
		ParentRunID:    params.ParentRunID,
		ThreadID:       params.ThreadID,
		PrincipalID:    params.PrincipalID,
		ActorID:        params.ActorID,
		Channel:        params.Channel,
		Status:         params.Status,
		Model:          params.Model,
		StartedAt:      startedAt,
		UpdatedAt:      startedAt,
		IdempotencyKey: params.IdempotencyKey,
		CorrelationID:  params.CorrelationID,
		TraceID:        params.TraceID,
		SpanID:         params.SpanID,
		StatsJSON:      "{}",
		MetadataJSON:   metadataJSON,
	}
	if _, err := tx.ExecContext(ctx, `
INSERT INTO runs (
  id, parent_run_id, thread_id, principal_id, actor_id, channel, status, model, started_at,
  updated_at, current_seq, idempotency_key, correlation_id, trace_id, span_id,
  stats_json, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?, ?, ?, ?)`,
		run.ID,
		run.ParentRunID,
		run.ThreadID,
		run.PrincipalID,
		run.ActorID,
		run.Channel,
		run.Status,
		run.Model,
		formatTime(run.StartedAt),
		formatTime(run.UpdatedAt),
		run.IdempotencyKey,
		run.CorrelationID,
		run.TraceID,
		run.SpanID,
		run.StatsJSON,
		run.MetadataJSON,
	); err != nil {
		return Run{}, fmt.Errorf("runs: insert run: %w", err)
	}
	return run, nil
}
