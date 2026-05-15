package runs

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/testutil"
)

func openStore(t *testing.T) (*sql.DB, *Store) {
	t.Helper()
	db := testutil.OpenTestDB(t, nil)
	if err := migrations.Run(context.Background(), db); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	store, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return db, store
}

func TestCreateOrGetRunDedupesInboundIdempotencyKey(t *testing.T) {
	_, store := openStore(t)
	ctx := context.Background()
	started := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)

	first, created, err := store.CreateOrGetRun(ctx, CreateRunParams{
		ID:             "run-first",
		IdempotencyKey: "telegram:update:123",
		ThreadID:       "thread-1",
		PrincipalID:    "principal-1",
		Channel:        "telegram",
		Status:         "running",
		StartedAt:      started,
		Metadata:       map[string]any{"source": "test"},
	})
	if err != nil {
		t.Fatalf("first CreateOrGetRun: %v", err)
	}
	if !created {
		t.Fatal("first CreateOrGetRun created=false")
	}

	second, created, err := store.CreateOrGetRun(ctx, CreateRunParams{
		ID:             "run-second",
		IdempotencyKey: "telegram:update:123",
		ThreadID:       "thread-2",
		PrincipalID:    "principal-2",
		Channel:        "telegram",
		Status:         "running",
		StartedAt:      started.Add(time.Minute),
	})
	if err != nil {
		t.Fatalf("second CreateOrGetRun: %v", err)
	}
	if created {
		t.Fatal("second CreateOrGetRun created=true")
	}
	if second.ID != first.ID {
		t.Fatalf("deduped run id = %q, want %q", second.ID, first.ID)
	}
	if second.ThreadID != "thread-1" {
		t.Fatalf("deduped thread = %q, want original thread-1", second.ThreadID)
	}
}

func TestAppendEventAdvancesPerRunSeqAndSnapshot(t *testing.T) {
	_, store := openStore(t)
	ctx := context.Background()
	run := createRun(t, store, "run-seq")

	started, err := store.AppendEvent(ctx, AppendEventParams{
		ID:      "event-started",
		RunID:   run.ID,
		Type:    "run_started",
		Payload: map[string]any{"thread_id": "thread-1"},
	})
	if err != nil {
		t.Fatalf("append run_started: %v", err)
	}
	doneAt := time.Date(2026, 5, 15, 8, 30, 0, 0, time.UTC)
	done, err := store.AppendEvent(ctx, AppendEventParams{
		ID:               "event-done",
		RunID:            run.ID,
		Type:             "done",
		RunStatus:        "completed",
		CompletedAt:      &doneAt,
		FinalTextPreview: "done",
		Stats:            map[string]any{"tokens_total": 42},
	})
	if err != nil {
		t.Fatalf("append done: %v", err)
	}
	if started.Seq != 1 || done.Seq != 2 {
		t.Fatalf("seqs = %d,%d; want 1,2", started.Seq, done.Seq)
	}

	got, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if got.CurrentSeq != 2 {
		t.Fatalf("CurrentSeq = %d, want 2", got.CurrentSeq)
	}
	if got.Status != "completed" {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(doneAt) {
		t.Fatalf("CompletedAt = %v, want %v", got.CompletedAt, doneAt)
	}
	if got.FinalTextPreview != "done" {
		t.Fatalf("FinalTextPreview = %q", got.FinalTextPreview)
	}
	var stats map[string]any
	if err := json.Unmarshal([]byte(got.StatsJSON), &stats); err != nil {
		t.Fatalf("decode stats json: %v", err)
	}
	if stats["tokens_total"] != float64(42) {
		t.Fatalf("tokens_total = %v, want 42", stats["tokens_total"])
	}
}

func TestEventsReplayInRunOrder(t *testing.T) {
	_, store := openStore(t)
	ctx := context.Background()
	run := createRun(t, store, "run-replay")

	for _, typ := range []string{"run_started", "tool_start", "tool_end", "done"} {
		if _, err := store.AppendEvent(ctx, AppendEventParams{RunID: run.ID, Type: typ}); err != nil {
			t.Fatalf("append %s: %v", typ, err)
		}
	}

	events, err := store.Events(ctx, run.ID)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("len(events) = %d, want 4", len(events))
	}
	for i, event := range events {
		wantSeq := int64(i + 1)
		if event.Seq != wantSeq {
			t.Fatalf("event[%d].Seq = %d, want %d", i, event.Seq, wantSeq)
		}
	}
}

func TestAppendEventDedupesEventIdempotencyKey(t *testing.T) {
	_, store := openStore(t)
	ctx := context.Background()
	run := createRun(t, store, "run-event-idem")

	first, err := store.AppendEvent(ctx, AppendEventParams{
		ID:             "event-one",
		RunID:          run.ID,
		Type:           "tool_start",
		IdempotencyKey: "tool:call-1:start",
	})
	if err != nil {
		t.Fatalf("first AppendEvent: %v", err)
	}
	second, err := store.AppendEvent(ctx, AppendEventParams{
		ID:             "event-two",
		RunID:          run.ID,
		Type:           "tool_start",
		IdempotencyKey: "tool:call-1:start",
	})
	if err != nil {
		t.Fatalf("second AppendEvent: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("deduped event id = %q, want %q", second.ID, first.ID)
	}

	events, err := store.Events(ctx, run.ID)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
}

func TestOutboxRequiresAndDedupesDeliveryIdempotency(t *testing.T) {
	db, store := openStore(t)
	ctx := context.Background()
	run := createRun(t, store, "run-outbox")
	event, err := store.AppendEvent(ctx, AppendEventParams{RunID: run.ID, Type: "done"})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := store.EnqueueOutbox(ctx, EnqueueOutboxParams{
		ID:             "outbox-one",
		RunID:          run.ID,
		EventID:        event.ID,
		Target:         "telegram:chat-1",
		IdempotencyKey: "deliver:done:1",
		Payload:        map[string]any{"kind": "done"},
	}); err != nil {
		t.Fatalf("EnqueueOutbox: %v", err)
	}
	if _, err := store.EnqueueOutbox(ctx, EnqueueOutboxParams{
		ID:             "outbox-two",
		RunID:          run.ID,
		EventID:        event.ID,
		Target:         "telegram:chat-1",
		IdempotencyKey: "deliver:done:1",
	}); err == nil {
		t.Fatal("duplicate outbox idempotency key error = nil")
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM run_outbox WHERE run_id = ?`, run.ID).Scan(&count); err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if count != 1 {
		t.Fatalf("outbox count = %d, want 1", count)
	}
}

func TestTerminalRunStateSurvivesStoreReopen(t *testing.T) {
	db, store := openStore(t)
	ctx := context.Background()
	run := createRun(t, store, "run-terminal")
	completedAt := time.Date(2026, 5, 15, 9, 0, 0, 0, time.UTC)
	if _, err := store.AppendEvent(ctx, AppendEventParams{
		RunID:       run.ID,
		Type:        "done",
		RunStatus:   "completed",
		CompletedAt: &completedAt,
	}); err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}

	reopened, err := NewStore(db)
	if err != nil {
		t.Fatalf("NewStore reopened: %v", err)
	}
	got, err := reopened.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun reopened: %v", err)
	}
	if got.Status != "completed" {
		t.Fatalf("Status = %q, want completed", got.Status)
	}
	if got.CompletedAt == nil || !got.CompletedAt.Equal(completedAt) {
		t.Fatalf("CompletedAt = %v, want %v", got.CompletedAt, completedAt)
	}
}

func createRun(t *testing.T, store *Store, id string) Run {
	t.Helper()
	run, created, err := store.CreateOrGetRun(context.Background(), CreateRunParams{
		ID:             id,
		ParentRunID:    "parent-run",
		ThreadID:       "thread-1",
		PrincipalID:    "principal-1",
		Channel:        "web",
		Status:         "running",
		IdempotencyKey: "inbound:" + id,
		CorrelationID:  "corr-1",
		StartedAt:      time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRun: %v", err)
	}
	if !created {
		t.Fatalf("CreateOrGetRun(%s) created=false", id)
	}
	return run
}
