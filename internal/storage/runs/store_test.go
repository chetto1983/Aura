package runs

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/identity"
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

func TestCreateRunAndEventsPersistActorID(t *testing.T) {
	db, store := openStore(t)
	ctx := context.Background()
	run, created, err := store.CreateOrGetRun(ctx, CreateRunParams{
		ID:          "run-actor",
		ThreadID:    "thread-actor",
		PrincipalID: "principal-actor",
		ActorID:     "actor-session-1",
		Channel:     "web",
		Status:      "running",
		StartedAt:   time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("CreateOrGetRun: %v", err)
	}
	if !created {
		t.Fatal("CreateOrGetRun created=false")
	}
	if run.ActorID != "actor-session-1" {
		t.Fatalf("run ActorID = %q", run.ActorID)
	}

	event, err := store.AppendEvent(ctx, AppendEventParams{
		RunID: run.ID,
		Type:  "run_started",
	})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if event.ActorID != "actor-session-1" {
		t.Fatalf("event ActorID = %q", event.ActorID)
	}

	stored, err := store.GetRun(ctx, run.ID)
	if err != nil {
		t.Fatalf("GetRun: %v", err)
	}
	if stored.ActorID != "actor-session-1" {
		t.Fatalf("stored ActorID = %q", stored.ActorID)
	}
	assertRunScalar(t, db, `SELECT COUNT(*) FROM runs WHERE id = ? AND actor_id = ?`, 1, run.ID, "actor-session-1")
	assertRunScalar(t, db, `SELECT COUNT(*) FROM run_events WHERE run_id = ? AND actor_id = ?`, 1, run.ID, "actor-session-1")
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

func TestRecordAuthorizationDenial(t *testing.T) {
	db, store := openStore(t)
	ctx := context.Background()
	run := createRun(t, store, "run-authz-denial")
	createdAt := time.Date(2026, 5, 15, 9, 30, 0, 0, time.UTC)

	decision := identity.AuthorizationDecision{
		ID:         "authz-deny-1",
		ActorID:    "actor-1",
		Capability: identity.CapabilityToolExecute,
		Resource:   identity.ResourceRef{Type: "tool", ID: "fake"},
		Decision:   identity.DecisionDeny,
		Reason:     "missing_grant",
		RunID:      run.ID,
		CreatedAt:  createdAt,
	}
	if err := store.RecordAuthorizationDenial(ctx, decision); err != nil {
		t.Fatalf("RecordAuthorizationDenial: %v", err)
	}

	var eventID, payloadJSON, redaction string
	if err := db.QueryRow(`
SELECT id, payload_json, redaction_level
FROM run_events
WHERE run_id = ? AND type = ? AND actor_id = ?
`, run.ID, EventAuthorizationDenied, decision.ActorID).Scan(&eventID, &payloadJSON, &redaction); err != nil {
		t.Fatalf("query authorization_denied run event: %v", err)
	}
	if redaction != RedactionMetadata {
		t.Fatalf("run event redaction = %q, want %q", redaction, RedactionMetadata)
	}
	assertPayloadField(t, payloadJSON, "decision_id", decision.ID)
	assertPayloadField(t, payloadJSON, "capability", string(decision.Capability))
	assertPayloadField(t, payloadJSON, "resource_id", decision.Resource.ID)

	var auditPayload, auditRedaction string
	if err := db.QueryRow(`
SELECT payload_json, redaction_level
FROM audit_events
WHERE run_id = ? AND event_id = ? AND type = ? AND actor_id = ? AND target_id = ?
`, run.ID, eventID, EventAuthorizationDenied, decision.ActorID, decision.ID).Scan(&auditPayload, &auditRedaction); err != nil {
		t.Fatalf("query authorization_denied audit event: %v", err)
	}
	if auditRedaction != RedactionMetadata {
		t.Fatalf("audit redaction = %q, want %q", auditRedaction, RedactionMetadata)
	}
	assertPayloadField(t, auditPayload, "reason", decision.Reason)
	assertNoPayloadText(t, db, "secret prompt")

	if err := store.RecordAuthorizationDenial(ctx, decision); err != nil {
		t.Fatalf("RecordAuthorizationDenial duplicate: %v", err)
	}
	assertRunScalar(t, db, `SELECT COUNT(*) FROM run_events WHERE run_id = ? AND type = ?`, 1, run.ID, EventAuthorizationDenied)
	assertRunScalar(t, db, `SELECT COUNT(*) FROM audit_events WHERE run_id = ? AND type = ?`, 1, run.ID, EventAuthorizationDenied)
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
		ActorID:        "actor:run:" + id,
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

func assertPayloadField(t *testing.T, payloadJSON, key string, want any) {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatalf("decode payload %s: %v", payloadJSON, err)
	}
	if got := payload[key]; got != want {
		t.Fatalf("payload[%s] = %#v, want %#v", key, got, want)
	}
}

func assertNoPayloadText(t *testing.T, db *sql.DB, needle string) {
	t.Helper()
	var count int
	if err := db.QueryRow(`
SELECT COUNT(*)
FROM run_events
WHERE payload_json LIKE ?
`, "%"+needle+"%").Scan(&count); err != nil {
		t.Fatalf("query run payload text: %v", err)
	}
	if count != 0 {
		t.Fatalf("run payload contains %q", needle)
	}
}

func assertRunScalar(t *testing.T, db *sql.DB, query string, want int, args ...any) {
	t.Helper()
	var got int
	if err := db.QueryRow(query, args...).Scan(&got); err != nil {
		t.Fatalf("query scalar %q: %v", query, err)
	}
	if got != want {
		t.Fatalf("scalar %q = %d, want %d", query, got, want)
	}
}
