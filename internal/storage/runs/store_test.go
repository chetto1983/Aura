package runs

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

func TestAppendEventRecordsRunOrigin(t *testing.T) {
	_, store := openStore(t)
	ctx := context.Background()
	started := time.Date(2026, 5, 15, 8, 0, 0, 0, time.UTC)

	cases := []struct {
		name        string
		id          string
		parentRunID string
		channel     string
		override    string
		want        string
	}{
		{name: "user web run", id: "run-origin-user", channel: "web", want: RunOriginUser},
		{name: "subagent parent", id: "run-origin-child", parentRunID: "run-parent", channel: "web", want: RunOriginSubagent},
		{name: "swarm run", id: "run-origin-swarm", channel: "swarm", want: RunOriginSubagent},
		{name: "scheduler run", id: "run-origin-cron", channel: "cron", want: RunOriginScheduler},
		{name: "source ingest run", id: "run-origin-source", channel: "source_ingest", want: RunOriginSourceIngest},
		{name: "explicit override", id: "run-origin-override", channel: "web", override: RunOriginScheduler, want: RunOriginScheduler},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			run, created, err := store.CreateOrGetRun(ctx, CreateRunParams{
				ID:          tc.id,
				ParentRunID: tc.parentRunID,
				ThreadID:    "thread-" + tc.id,
				Channel:     tc.channel,
				Status:      "running",
				StartedAt:   started,
			})
			if err != nil {
				t.Fatalf("CreateOrGetRun: %v", err)
			}
			if !created {
				t.Fatal("CreateOrGetRun created=false")
			}
			event, err := store.AppendEvent(ctx, AppendEventParams{
				RunID:     run.ID,
				Type:      "run_started",
				RunOrigin: tc.override,
			})
			if err != nil {
				t.Fatalf("AppendEvent: %v", err)
			}
			if event.RunOrigin != tc.want {
				t.Fatalf("event.RunOrigin = %q, want %q", event.RunOrigin, tc.want)
			}
			stored, err := store.GetEvent(ctx, event.ID)
			if err != nil {
				t.Fatalf("GetEvent: %v", err)
			}
			if stored.RunOrigin != tc.want {
				t.Fatalf("stored.RunOrigin = %q, want %q", stored.RunOrigin, tc.want)
			}
		})
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

func TestQuestionLifecycleRecordsRequestAndAnswer(t *testing.T) {
	_, store := openStore(t)
	ctx := context.Background()
	run := createRun(t, store, "run-question")
	requestedAt := time.Date(2026, 5, 16, 8, 0, 0, 0, time.UTC)
	event, err := store.AppendEvent(ctx, AppendEventParams{
		ID:        "evt-question",
		RunID:     run.ID,
		Type:      "question_requested",
		ActorID:   "actor-1",
		CreatedAt: requestedAt,
		Payload: map[string]any{
			"question_id":      "evt-question",
			"kind":             "approval",
			"question_preview": "Approve durable memory write?",
			"options_count":    5,
		},
		RunStatus: "waiting_for_user",
	})
	if err != nil {
		t.Fatalf("AppendEvent question_requested: %v", err)
	}
	question, err := store.RecordQuestionRequested(ctx, RecordQuestionRequestedParams{
		ID:           event.ID,
		RunID:        run.ID,
		EventID:      event.ID,
		ThreadID:     "thread-1",
		ActorID:      "actor-1",
		Channel:      "telegram",
		Kind:         "approval",
		QuestionText: "Approve durable memory write?",
		Options:      []string{"approve_once", "deny", "cancel"},
		RequestedAt:  requestedAt,
		Producer:     map[string]any{"tool": "ask_user", "tool_call_id": "ask-1"},
	})
	if err != nil {
		t.Fatalf("RecordQuestionRequested: %v", err)
	}
	if question.Status != QuestionStatusWaiting || question.Kind != "approval" {
		t.Fatalf("question status/kind = %s/%s", question.Status, question.Kind)
	}
	pending, ok, err := store.LatestPendingQuestion(ctx, "thread-1", "telegram")
	if err != nil {
		t.Fatalf("LatestPendingQuestion: %v", err)
	}
	if !ok || pending.ID != event.ID {
		t.Fatalf("pending = %+v ok=%v, want %s", pending, ok, event.ID)
	}

	answerRun := createRun(t, store, "run-answer")
	answerEvent, err := store.AppendEvent(ctx, AppendEventParams{
		ID:          "evt-answer",
		RunID:       answerRun.ID,
		Type:        "question_answered",
		CausationID: question.ID,
		Payload: map[string]any{
			"question_id":           question.ID,
			"selected_option_count": 1,
			"has_free_text":         true,
		},
	})
	if err != nil {
		t.Fatalf("AppendEvent question_answered: %v", err)
	}
	answered, err := store.RecordQuestionAnswered(ctx, RecordQuestionAnsweredParams{
		ID:                question.ID,
		AnswerRunID:       answerRun.ID,
		AnswerEventID:     answerEvent.ID,
		ThreadID:          "thread-1",
		Channel:           "telegram",
		ActorID:           "actor-1",
		SelectedOptionIDs: []string{"1"},
		FreeText:          "approve_once",
		AnsweredMessageID: "tg-msg-2",
		AnsweredAt:        answerEvent.CreatedAt,
	})
	if err != nil {
		t.Fatalf("RecordQuestionAnswered: %v", err)
	}
	if answered.Status != QuestionStatusAnswered {
		t.Fatalf("answered status = %q", answered.Status)
	}
	if answered.AnswerRunID != answerRun.ID || answered.AnswerEventID != answerEvent.ID {
		t.Fatalf("answer linkage = %s/%s", answered.AnswerRunID, answered.AnswerEventID)
	}
	if answered.AnswerPreview != "approve_once" {
		t.Fatalf("answer preview = %q", answered.AnswerPreview)
	}
	if _, ok, err := store.LatestPendingQuestion(ctx, "thread-1", "telegram"); err != nil || ok {
		t.Fatalf("pending after answer ok=%v err=%v", ok, err)
	}
}

func TestQuestionAnswerRejectsDuplicateAndWrongChannel(t *testing.T) {
	_, store := openStore(t)
	ctx := context.Background()
	run := createRun(t, store, "run-question-2")
	event, err := store.AppendEvent(ctx, AppendEventParams{ID: "evt-question-2", RunID: run.ID, Type: "question_requested"})
	if err != nil {
		t.Fatalf("AppendEvent: %v", err)
	}
	if _, err := store.RecordQuestionRequested(ctx, RecordQuestionRequestedParams{
		ID:           event.ID,
		RunID:        run.ID,
		EventID:      event.ID,
		ThreadID:     "thread-2",
		Channel:      "telegram",
		QuestionText: "Which project?",
	}); err != nil {
		t.Fatalf("RecordQuestionRequested: %v", err)
	}
	answerRun := createRun(t, store, "run-answer-2")
	answerEvent, err := store.AppendEvent(ctx, AppendEventParams{ID: "evt-answer-2", RunID: answerRun.ID, Type: "question_answered"})
	if err != nil {
		t.Fatalf("AppendEvent answer: %v", err)
	}
	if _, err := store.RecordQuestionAnswered(ctx, RecordQuestionAnsweredParams{
		ID:            event.ID,
		AnswerRunID:   answerRun.ID,
		AnswerEventID: answerEvent.ID,
		Channel:       "web",
	}); !errors.Is(err, ErrQuestionChannelMismatch) {
		t.Fatalf("wrong-channel error = %v, want ErrQuestionChannelMismatch", err)
	}
	if _, err := store.RecordQuestionAnswered(ctx, RecordQuestionAnsweredParams{
		ID:            event.ID,
		AnswerRunID:   answerRun.ID,
		AnswerEventID: answerEvent.ID,
		Channel:       "telegram",
	}); err != nil {
		t.Fatalf("RecordQuestionAnswered first: %v", err)
	}
	if _, err := store.RecordQuestionAnswered(ctx, RecordQuestionAnsweredParams{
		ID:            event.ID,
		AnswerRunID:   answerRun.ID,
		AnswerEventID: answerEvent.ID,
		Channel:       "telegram",
	}); !errors.Is(err, ErrQuestionNotWaiting) {
		t.Fatalf("duplicate answer error = %v, want ErrQuestionNotWaiting", err)
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
