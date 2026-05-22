package runs

import (
	"context"
	"errors"
	"fmt"
)

func (s *Store) EnqueueOutbox(ctx context.Context, params EnqueueOutboxParams) (OutboxItem, error) {
	if params.RunID == "" {
		return OutboxItem{}, errors.New("runs: outbox run id is required")
	}
	if params.Target == "" {
		return OutboxItem{}, errors.New("runs: outbox target is required")
	}
	if params.IdempotencyKey == "" {
		return OutboxItem{}, errors.New("runs: outbox idempotency key is required")
	}
	now := params.CreatedAt
	if now.IsZero() {
		now = s.now()
	}
	payloadJSON, err := outboxPayloadJSON(params)
	if err != nil {
		return OutboxItem{}, err
	}
	item := OutboxItem{
		ID:             firstNonEmpty(params.ID, newID("out")),
		RunID:          params.RunID,
		EventID:        params.EventID,
		Target:         params.Target,
		IdempotencyKey: params.IdempotencyKey,
		PayloadJSON:    payloadJSON,
		Status:         firstNonEmpty(params.Status, "pending"),
		NextAttemptAt:  params.NextAttemptAt,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	if _, err := s.db.ExecContext(ctx, `
INSERT INTO run_outbox (
  id, run_id, event_id, target, idempotency_key, payload_json, status, attempts,
  next_attempt_at, last_error, created_at, updated_at
) VALUES (?, ?, ?, ?, ?, ?, ?, 0, ?, '', ?, ?)`,
		item.ID,
		item.RunID,
		item.EventID,
		item.Target,
		item.IdempotencyKey,
		item.PayloadJSON,
		item.Status,
		formatOptionalTime(item.NextAttemptAt),
		formatTime(item.CreatedAt),
		formatTime(item.UpdatedAt),
	); err != nil {
		return OutboxItem{}, fmt.Errorf("runs: enqueue outbox: %w", err)
	}
	return item, nil
}

func outboxPayloadJSON(params EnqueueOutboxParams) (string, error) {
	if params.PayloadJSON != "" {
		return params.PayloadJSON, nil
	}
	return encodeJSON(params.Payload, "{}")
}
