package documents

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identityctx"
	"github.com/jackc/pgx/v5/pgxpool"
)

// IngestionEvent is an operator-visible lifecycle/progress event.
type IngestionEvent struct {
	ID                 int64          `json:"id"`
	IdentityID         string         `json:"identity_id"`
	EntityType         string         `json:"entity_type"`
	EntityID           string         `json:"entity_id"`
	JobID              string         `json:"job_id,omitempty"`
	FromStatus         string         `json:"from_status,omitempty"`
	ToStatus           string         `json:"to_status,omitempty"`
	EventType          string         `json:"event_type"`
	Message            string         `json:"message,omitempty"`
	Detail             map[string]any `json:"detail"`
	TraceID            string         `json:"trace_id,omitempty"`
	PipelineGeneration int64          `json:"pipeline_generation"`
	AttemptGeneration  int64          `json:"attempt_generation"`
	LeaseGeneration    int64          `json:"lease_generation"`
	CreatedAt          time.Time      `json:"created_at"`
}

// AppendIngestionEventRequest carries one event append request.
type AppendIngestionEventRequest struct {
	IdentityID         string
	EntityType         string
	EntityID           string
	JobID              string
	FromStatus         string
	ToStatus           string
	EventType          string
	Message            string
	Detail             map[string]any
	TraceID            string
	PipelineGeneration int64
	AttemptGeneration  int64
	LeaseGeneration    int64
}

// IngestionEventStore appends ingestion timeline events.
type IngestionEventStore interface {
	Append(context.Context, AppendIngestionEventRequest) (IngestionEvent, error)
}

// IngestionEventReader lists previously appended ingestion lifecycle events.
type IngestionEventReader interface {
	ListByJob(context.Context, string) ([]IngestionEvent, error)
}

// PostgresIngestionEventStore implements ingestion event storage with sqlc.
type PostgresIngestionEventStore struct {
	pool *pgxpool.Pool
}

// NewPostgresIngestionEventStore builds a Postgres-backed ingestion event store.
func NewPostgresIngestionEventStore(pool *pgxpool.Pool) *PostgresIngestionEventStore {
	return &PostgresIngestionEventStore{pool: pool}
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
	identityID, err := pgUUID("event identity id", req.IdentityID)
	if err != nil {
		return IngestionEvent{}, err
	}
	var row sqlc.AuraIngestionEvents
	err = s.withIdentity(ctx, req.IdentityID, func(q *sqlc.Queries) error {
		var queryErr error
		row, queryErr = q.AppendIngestionEvent(ctx, sqlc.AppendIngestionEventParams{
			IdentityID: identityID, EntityType: req.EntityType,
			EntityID:           entityID,
			JobID:              jobID,
			FromStatus:         pgText(req.FromStatus),
			ToStatus:           pgText(req.ToStatus),
			EventType:          req.EventType,
			Message:            req.Message,
			Detail:             detail,
			TraceID:            req.TraceID,
			PipelineGeneration: req.PipelineGeneration,
			AttemptGeneration:  req.AttemptGeneration,
			LeaseGeneration:    req.LeaseGeneration,
		})
		return queryErr
	})
	if err != nil {
		return IngestionEvent{}, err
	}
	return ingestionEventFromSQL(row)
}

// ListByJob returns ingestion events for one durable ingestion job in timeline order.
func (s *PostgresIngestionEventStore) ListByJob(ctx context.Context, jobID string) ([]IngestionEvent, error) {
	pgJobID, err := pgUUID("event job id", jobID)
	if err != nil {
		return nil, err
	}
	identityID := identityctx.IdentityID(ctx)
	pgIdentityID, err := pgUUID("event identity id", identityID)
	if err != nil {
		return nil, err
	}
	var rows []sqlc.AuraIngestionEvents
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var queryErr error
		rows, queryErr = q.ListIngestionEventsByJob(ctx, sqlc.ListIngestionEventsByJobParams{
			IdentityID: pgIdentityID, JobID: pgJobID,
		})
		return queryErr
	})
	if err != nil {
		return nil, err
	}
	out := make([]IngestionEvent, 0, len(rows))
	for _, row := range rows {
		event, err := ingestionEventFromSQL(row)
		if err != nil {
			return nil, err
		}
		out = append(out, event)
	}
	return out, nil
}

func (s *PostgresIngestionEventStore) withIdentity(ctx context.Context, identityID string, fn func(*sqlc.Queries) error) error {
	if s == nil || s.pool == nil {
		return fmt.Errorf("ingestion event store is not configured")
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, fn)
}

func ingestionEventFromSQL(row sqlc.AuraIngestionEvents) (IngestionEvent, error) {
	detail, err := ingestionEventDetailFromJSON(row.Detail)
	if err != nil {
		return IngestionEvent{}, err
	}
	return IngestionEvent{
		ID:                 row.ID,
		IdentityID:         uuidString(row.IdentityID),
		EntityType:         row.EntityType,
		EntityID:           uuidString(row.EntityID),
		JobID:              uuidString(row.JobID),
		FromStatus:         textString(row.FromStatus),
		ToStatus:           textString(row.ToStatus),
		EventType:          row.EventType,
		Message:            row.Message,
		Detail:             detail,
		TraceID:            row.TraceID,
		PipelineGeneration: row.PipelineGeneration,
		AttemptGeneration:  row.AttemptGeneration,
		LeaseGeneration:    row.LeaseGeneration,
		CreatedAt:          timeValue(row.CreatedAt),
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
