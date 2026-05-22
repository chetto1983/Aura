package runs

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

func (s *Store) AppendEvent(ctx context.Context, params AppendEventParams) (Event, error) {
	if params.RunID == "" {
		return Event{}, errors.New("runs: event run id is required")
	}
	if params.Type == "" {
		return Event{}, errors.New("runs: event type is required")
	}
	if params.IdempotencyKey != "" {
		if eventID, ok, err := s.eventIDForIdempotencyKey(ctx, "event", params.IdempotencyKey); err != nil {
			return Event{}, err
		} else if ok {
			return s.GetEvent(ctx, eventID)
		}
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Event{}, fmt.Errorf("runs: begin append event: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := params.CreatedAt
	if now.IsZero() {
		now = s.now()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE runs SET current_seq = current_seq + 1, updated_at = ? WHERE id = ?`, formatTime(now), params.RunID); err != nil {
		return Event{}, fmt.Errorf("runs: advance run seq: %w", err)
	}

	var run Run
	if err := tx.QueryRowContext(ctx, `
SELECT id, parent_run_id, thread_id, principal_id, actor_id, channel, status, model, started_at, updated_at,
       completed_at, cancelled_at, last_error, current_seq, idempotency_key, correlation_id,
       trace_id, span_id, final_text_preview, stats_json, metadata_json
FROM runs
WHERE id = ?`, params.RunID).Scan(runScanDest(&run)...); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Event{}, fmt.Errorf("runs: append event for missing run %q", params.RunID)
		}
		return Event{}, fmt.Errorf("runs: read run after seq advance: %w", err)
	}

	payloadJSON, err := eventPayloadJSON(params)
	if err != nil {
		return Event{}, err
	}
	schemaVersion := params.SchemaVersion
	if schemaVersion == 0 {
		schemaVersion = DefaultSchemaVersion
	}
	redactionLevel := params.RedactionLevel
	if redactionLevel == "" {
		redactionLevel = RedactionMetadata
	}
	event := Event{
		ID:             firstNonEmpty(params.ID, newID("evt")),
		RunID:          params.RunID,
		ParentRunID:    run.ParentRunID,
		Seq:            run.CurrentSeq,
		Type:           params.Type,
		SchemaVersion:  schemaVersion,
		ActorID:        firstNonEmpty(params.ActorID, run.ActorID),
		CausationID:    params.CausationID,
		CorrelationID:  firstNonEmpty(params.CorrelationID, run.CorrelationID),
		IdempotencyKey: params.IdempotencyKey,
		RunOrigin:      deriveRunOrigin(params.RunOrigin, run),
		PayloadJSON:    payloadJSON,
		RedactionLevel: redactionLevel,
		CreatedAt:      now,
	}

	if _, err := tx.ExecContext(ctx, `
INSERT INTO run_events (
  id, run_id, parent_run_id, seq, type, schema_version, actor_id, causation_id,
  correlation_id, idempotency_key, run_origin, payload_json, redaction_level, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ID,
		event.RunID,
		event.ParentRunID,
		event.Seq,
		event.Type,
		event.SchemaVersion,
		event.ActorID,
		event.CausationID,
		event.CorrelationID,
		event.IdempotencyKey,
		event.RunOrigin,
		event.PayloadJSON,
		event.RedactionLevel,
		formatTime(event.CreatedAt),
	); err != nil {
		return Event{}, fmt.Errorf("runs: insert run event: %w", err)
	}
	if params.IdempotencyKey != "" {
		if _, err := tx.ExecContext(
			ctx,
			`INSERT INTO run_idempotency_keys (scope, key, run_id, event_id, created_at) VALUES (?, ?, ?, ?, ?)`,
			"event",
			params.IdempotencyKey,
			params.RunID,
			event.ID,
			formatTime(now),
		); err != nil {
			return Event{}, fmt.Errorf("runs: record event idempotency key: %w", err)
		}
	}
	if err := s.applySnapshotUpdate(ctx, tx, params, now); err != nil {
		return Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return Event{}, fmt.Errorf("runs: commit append event: %w", err)
	}
	return event, nil
}

func (s *Store) GetEvent(ctx context.Context, eventID string) (Event, error) {
	var event Event
	if err := s.db.QueryRowContext(ctx, `
SELECT id, run_id, parent_run_id, seq, type, schema_version, actor_id, causation_id,
       correlation_id, idempotency_key, run_origin, payload_json, redaction_level, created_at
FROM run_events
WHERE id = ?`, eventID).Scan(eventScanDest(&event)...); err != nil {
		return Event{}, fmt.Errorf("runs: get event %q: %w", eventID, err)
	}
	return event, nil
}

func (s *Store) Events(ctx context.Context, runID string) ([]Event, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, run_id, parent_run_id, seq, type, schema_version, actor_id, causation_id,
       correlation_id, idempotency_key, run_origin, payload_json, redaction_level, created_at
FROM run_events
WHERE run_id = ?
ORDER BY seq`, runID)
	if err != nil {
		return nil, fmt.Errorf("runs: query events: %w", err)
	}
	defer func() { _ = rows.Close() }()

	var events []Event
	for rows.Next() {
		var event Event
		if err := rows.Scan(eventScanDest(&event)...); err != nil {
			return nil, fmt.Errorf("runs: scan event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("runs: iterate events: %w", err)
	}
	return events, nil
}

func (s *Store) eventIDForIdempotencyKey(ctx context.Context, scope, key string) (string, bool, error) {
	var eventID string
	err := s.db.QueryRowContext(ctx, `SELECT event_id FROM run_idempotency_keys WHERE scope = ? AND key = ?`, scope, key).Scan(&eventID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("runs: lookup event idempotency key: %w", err)
	}
	return eventID, eventID != "", nil
}

func (s *Store) applySnapshotUpdate(ctx context.Context, tx *sql.Tx, params AppendEventParams, now time.Time) error {
	sets := []string{"updated_at = ?"}
	args := []any{formatTime(now)}
	if params.RunStatus != "" {
		sets = append(sets, "status = ?")
		args = append(args, params.RunStatus)
	}
	if params.CompletedAt != nil {
		sets = append(sets, "completed_at = ?")
		args = append(args, formatTime(*params.CompletedAt))
	}
	if params.CancelledAt != nil {
		sets = append(sets, "cancelled_at = ?")
		args = append(args, formatTime(*params.CancelledAt))
	}
	if params.LastError != "" {
		sets = append(sets, "last_error = ?")
		args = append(args, params.LastError)
	}
	if params.FinalTextPreview != "" {
		sets = append(sets, "final_text_preview = ?")
		args = append(args, params.FinalTextPreview)
	}
	if params.Stats != nil {
		statsJSON, err := encodeJSON(params.Stats, "{}")
		if err != nil {
			return fmt.Errorf("runs: encode stats: %w", err)
		}
		sets = append(sets, "stats_json = ?")
		args = append(args, statsJSON)
	}
	if params.Metadata != nil {
		metadataJSON, err := encodeJSON(params.Metadata, "{}")
		if err != nil {
			return fmt.Errorf("runs: encode metadata: %w", err)
		}
		sets = append(sets, "metadata_json = ?")
		args = append(args, metadataJSON)
	}
	args = append(args, params.RunID)
	query := "UPDATE runs SET " + strings.Join(sets, ", ") + " WHERE id = ?"
	if _, err := tx.ExecContext(ctx, query, args...); err != nil {
		return fmt.Errorf("runs: update run snapshot: %w", err)
	}
	return nil
}

func eventPayloadJSON(params AppendEventParams) (string, error) {
	if params.PayloadJSON != "" {
		return params.PayloadJSON, nil
	}
	return encodeJSON(params.Payload, "{}")
}
