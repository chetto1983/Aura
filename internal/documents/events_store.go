package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
)

// IngestionEvent is an operator-visible lifecycle/progress event.
type IngestionEvent struct {
	ID         int64          `json:"id"`
	EntityType string         `json:"entity_type"`
	EntityID   string         `json:"entity_id"`
	JobID      string         `json:"job_id,omitempty"`
	FromStatus string         `json:"from_status,omitempty"`
	ToStatus   string         `json:"to_status,omitempty"`
	EventType  string         `json:"event_type"`
	Message    string         `json:"message,omitempty"`
	Detail     map[string]any `json:"detail"`
	TraceID    string         `json:"trace_id,omitempty"`
	CreatedAt  time.Time      `json:"created_at,omitempty"`
}

// AppendIngestionEventRequest carries one event append request.
type AppendIngestionEventRequest struct {
	EntityType string
	EntityID   string
	JobID      string
	FromStatus string
	ToStatus   string
	EventType  string
	Message    string
	Detail     map[string]any
	TraceID    string
}

// IngestionEventStore appends ingestion timeline events.
type IngestionEventStore interface {
	Append(context.Context, AppendIngestionEventRequest) (IngestionEvent, error)
}

// PostgresIngestionEventStore implements ingestion event storage with sqlc.
type PostgresIngestionEventStore struct {
	q *sqlc.Queries
}

// NewPostgresIngestionEventStore builds a Postgres-backed ingestion event store.
func NewPostgresIngestionEventStore(db sqlc.DBTX) *PostgresIngestionEventStore {
	return &PostgresIngestionEventStore{q: sqlc.New(db)}
}

// Append persists one ingestion event.
func (s *PostgresIngestionEventStore) Append(ctx context.Context, req AppendIngestionEventRequest) (IngestionEvent, error) {
	entityID, err := pgUUID("event entity id", req.EntityID)
	if err != nil {
		return IngestionEvent{}, err
	}
	jobID, err := optionalUUIDFromString("event job id", req.JobID)
	if err != nil {
		return IngestionEvent{}, err
	}
	detail, err := ingestionEventDetailJSON(req.Detail)
	if err != nil {
		return IngestionEvent{}, err
	}
	row, err := s.q.AppendIngestionEvent(ctx, sqlc.AppendIngestionEventParams{
		EntityType: req.EntityType,
		EntityID:   entityID,
		JobID:      jobID,
		FromStatus: pgText(req.FromStatus),
		ToStatus:   pgText(req.ToStatus),
		EventType:  req.EventType,
		Message:    req.Message,
		Detail:     detail,
		TraceID:    req.TraceID,
	})
	if err != nil {
		return IngestionEvent{}, err
	}
	return ingestionEventFromSQL(row)
}

func ingestionEventFromSQL(row sqlc.AuraIngestionEvents) (IngestionEvent, error) {
	detail, err := ingestionEventDetailFromJSON(row.Detail)
	if err != nil {
		return IngestionEvent{}, err
	}
	return IngestionEvent{
		ID:         row.ID,
		EntityType: row.EntityType,
		EntityID:   uuidString(row.EntityID),
		JobID:      uuidString(row.JobID),
		FromStatus: textString(row.FromStatus),
		ToStatus:   textString(row.ToStatus),
		EventType:  row.EventType,
		Message:    row.Message,
		Detail:     detail,
		TraceID:    row.TraceID,
		CreatedAt:  timeValue(row.CreatedAt),
	}, nil
}

func ingestionEventDetailJSON(detail map[string]any) ([]byte, error) {
	if detail == nil {
		detail = map[string]any{}
	}
	out, err := json.Marshal(detail)
	if err != nil {
		return nil, fmt.Errorf("ingestion event detail: %w", err)
	}
	return out, nil
}

func ingestionEventDetailFromJSON(data []byte) (map[string]any, error) {
	if len(data) == 0 {
		return map[string]any{}, nil
	}
	var detail map[string]any
	if err := json.Unmarshal(data, &detail); err != nil {
		return nil, fmt.Errorf("ingestion event detail: %w", err)
	}
	if detail == nil {
		return map[string]any{}, nil
	}
	return detail, nil
}
