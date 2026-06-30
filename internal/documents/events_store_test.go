package documents

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

var _ interface {
	ListByJob(context.Context, string) ([]IngestionEvent, error)
} = (*PostgresIngestionEventStore)(nil)

func TestIngestionEventFromSQLDecodesDetail(t *testing.T) {
	row := sqlc.AuraIngestionEvents{
		ID:         42,
		EntityType: "ingestion_job",
		EntityID:   mustCatalogTestUUID(t, "10000000-0000-0000-0000-000000000001"),
		JobID:      mustCatalogTestUUID(t, "10000000-0000-0000-0000-000000000002"),
		FromStatus: pgtype.Text{String: "running", Valid: true},
		ToStatus:   pgtype.Text{String: "succeeded", Valid: true},
		EventType:  "ingestion_job.succeeded",
		Message:    "ok",
		Detail:     []byte(`{"stage":"accepted"}`),
		TraceID:    "trace-1",
		CreatedAt:  pgtype.Timestamptz{Time: time.Unix(1, 0), Valid: true},
	}

	event, err := ingestionEventFromSQL(row)
	if err != nil {
		t.Fatal(err)
	}
	if event.ID != 42 || event.EntityID != "10000000-0000-0000-0000-000000000001" || event.JobID != "10000000-0000-0000-0000-000000000002" {
		t.Fatalf("event identity = %#v", event)
	}
	if event.FromStatus != "running" || event.ToStatus != "succeeded" || event.EventType != "ingestion_job.succeeded" {
		t.Fatalf("event transition = %#v", event)
	}
	if !reflect.DeepEqual(event.Detail, map[string]any{"stage": "accepted"}) {
		t.Fatalf("event detail = %#v", event.Detail)
	}
}

func TestNewPostgresIngestionEventStoreWiresQueries(t *testing.T) {
	store := NewPostgresIngestionEventStore(nil)
	if store == nil || store.q == nil {
		t.Fatalf("NewPostgresIngestionEventStore = %#v", store)
	}
}
