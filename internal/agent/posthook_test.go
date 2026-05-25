package agent

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/agent/tools/attempts"
	"github.com/aura/aura/internal/db/migrations"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/testutil"
)

type fakePostTurnFailureReader struct {
	failures []attempts.ToolAttempt
	err      error
	calls    int
}

func (f *fakePostTurnFailureReader) RecentThreadFailures(context.Context, string, string, int) ([]attempts.ToolAttempt, error) {
	f.calls++
	return append([]attempts.ToolAttempt(nil), f.failures...), f.err
}

type panicPostTurnHook struct{}

func (panicPostTurnHook) Apply(context.Context, TurnRecord, *memoryindex.Store) []error {
	panic("boom")
}

type recordingPostTurnHook struct {
	ch chan TurnRecord
}

func (h recordingPostTurnHook) Apply(_ context.Context, turn TurnRecord, _ *memoryindex.Store) []error {
	h.ch <- turn
	return nil
}

func openPostHookStore(t *testing.T) *memoryindex.Store {
	t.Helper()
	db := testutil.OpenTestDB(t, migrations.Run)
	store, err := memoryindex.NewStore(db)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return store
}

func fetchOperationalDocs(t *testing.T, store *memoryindex.Store) []memoryindex.Document {
	t.Helper()
	docs, err := store.FetchRecentOperational(context.Background(), 20)
	if err != nil {
		t.Fatalf("FetchRecentOperational: %v", err)
	}
	return docs
}

func attempt(runID, toolName, class string) attempts.ToolAttempt {
	return attempts.ToolAttempt{
		RunID:     runID,
		ToolName:  toolName,
		ToolKind:  "native",
		Outcome:   "recoverable",
		Class:     class,
		Reason:    "timeout",
		StartedAt: time.Now().UTC(),
		EndedAt:   time.Now().UTC(),
	}
}

func TestHeuristicPostTurnHookNFailureCases(t *testing.T) {
	tests := []struct {
		name      string
		threshold int
		failures  []attempts.ToolAttempt
		readerErr error
		wantDocs  int
		wantErr   bool
		wantCalls int
	}{
		{
			name:      "single failure below default threshold",
			failures:  []attempts.ToolAttempt{attempt("run-1", "web_fetch", "io")},
			wantDocs:  0,
			wantCalls: 1,
		},
		{
			name:      "two same tool and class writes one lesson",
			failures:  []attempts.ToolAttempt{attempt("run-1", "web_fetch", "io"), attempt("run-2", "web_fetch", "io")},
			wantDocs:  1,
			wantCalls: 1,
		},
		{
			name:      "threshold override requires three",
			threshold: 3,
			failures:  []attempts.ToolAttempt{attempt("run-1", "web_fetch", "io"), attempt("run-2", "web_fetch", "io")},
			wantDocs:  0,
			wantCalls: 1,
		},
		{
			name:      "threshold override satisfied by three",
			threshold: 3,
			failures:  []attempts.ToolAttempt{attempt("run-1", "web_fetch", "io"), attempt("run-2", "web_fetch", "io"), attempt("run-3", "web_fetch", "io")},
			wantDocs:  1,
			wantCalls: 1,
		},
		{
			name:      "different classes do not combine",
			failures:  []attempts.ToolAttempt{attempt("run-1", "web_fetch", "io"), attempt("run-2", "web_fetch", "validation")},
			wantDocs:  0,
			wantCalls: 1,
		},
		{
			name:      "empty class is skipped",
			failures:  []attempts.ToolAttempt{attempt("run-1", "web_fetch", ""), attempt("run-2", "web_fetch", "")},
			wantDocs:  0,
			wantCalls: 1,
		},
		{
			name:      "reader error is reported",
			readerErr: errors.New("db down"),
			wantDocs:  0,
			wantErr:   true,
			wantCalls: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := openPostHookStore(t)
			reader := &fakePostTurnFailureReader{failures: tt.failures, err: tt.readerErr}
			hook := HeuristicPostTurnHook{
				FailureReader:  reader,
				NFailThreshold: tt.threshold,
			}
			errs := hook.Apply(context.Background(), TurnRecord{RunID: "run-now", ThreadID: "thread-1"}, store)
			if (len(errs) > 0) != tt.wantErr {
				t.Fatalf("errs = %v, wantErr=%v", errs, tt.wantErr)
			}
			if reader.calls != tt.wantCalls {
				t.Fatalf("reader calls = %d, want %d", reader.calls, tt.wantCalls)
			}
			if got := len(fetchOperationalDocs(t, store)); got != tt.wantDocs {
				t.Fatalf("operational docs = %d, want %d", got, tt.wantDocs)
			}
		})
	}
}

// TestHeuristicPostTurnHookIgnoresUserMessageProse asserts the
// 2026-05-25 contract: the hook NEVER classifies a turn as
// "user_negative_feedback" by parsing UserMessage prose. The previous
// userNegativePattern regex (`(?i)^\s*(no|non|stop|smetti|sbagliato|wrong|fermati)\b`)
// was removed per feedback_no_regex_for_nlp. Negative-feedback learning
// re-enters this hook only when a structured signal lands (reaction
// emoji or /stop slash command); until then, the n_failure trigger is
// the only operational-lesson path that fires.
func TestHeuristicPostTurnHookIgnoresUserMessageProse(t *testing.T) {
	cases := []TurnRecord{
		{RunID: "run-a", UserMessage: "no, sbagliato", PreviousTurnHadToolCall: true, PreviousToolNames: []string{"web_fetch"}},
		{RunID: "run-b", UserMessage: "wrong result", PreviousTurnHadToolCall: true, PreviousToolNames: []string{"web_fetch"}},
		{RunID: "run-c", UserMessage: "stop fermati", PreviousTurnHadToolCall: true, PreviousToolNames: []string{"web_fetch"}},
		{RunID: "run-d", UserMessage: "ok continua", PreviousTurnHadToolCall: true, PreviousToolNames: []string{"web_fetch"}},
	}
	for _, turn := range cases {
		t.Run(turn.UserMessage, func(t *testing.T) {
			store := openPostHookStore(t)
			hook := HeuristicPostTurnHook{}
			errs := hook.Apply(context.Background(), turn, store)
			if len(errs) > 0 {
				t.Fatalf("Apply errors = %v", errs)
			}
			if got := len(fetchOperationalDocs(t, store)); got != 0 {
				t.Fatalf("operational docs = %d, want 0 (prose-based negative trigger removed)", got)
			}
		})
	}
}

func TestHeuristicPostTurnHookPayloadShape(t *testing.T) {
	store := openPostHookStore(t)
	reader := &fakePostTurnFailureReader{failures: []attempts.ToolAttempt{
		attempt("run-b", "web_fetch", "io"),
		attempt("run-a", "web_fetch", "io"),
	}}
	hook := HeuristicPostTurnHook{FailureReader: reader}

	errs := hook.Apply(context.Background(), TurnRecord{RunID: "run-now", ThreadID: "thread-1"}, store)
	if len(errs) > 0 {
		t.Fatalf("Apply errors = %v", errs)
	}
	docs := fetchOperationalDocs(t, store)
	if len(docs) != 1 {
		t.Fatalf("docs = %d, want 1", len(docs))
	}
	var payload postTurnLessonPayload
	if err := json.Unmarshal([]byte(docs[0].Body), &payload); err != nil {
		t.Fatalf("payload JSON: %v\nbody=%s", err, docs[0].Body)
	}
	if payload.Tool != "web_fetch" || payload.Cause != "error_class:io" || payload.Scope != "tool:web_fetch" {
		t.Fatalf("payload = %+v", payload)
	}
	if got := strings.Join(payload.EvidenceRunIDs, ","); got != "run-a,run-b" {
		t.Fatalf("evidence_run_ids = %q, want run-a,run-b", got)
	}
}

func TestApplyPostTurnHooksRecoversPanic(t *testing.T) {
	store := openPostHookStore(t)
	errs := ApplyPostTurnHooks(context.Background(), []PostTurnHook{panicPostTurnHook{}}, TurnRecord{RunID: "run-1"}, store, nil)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "panic recovered") {
		t.Fatalf("errs = %v, want recovered panic", errs)
	}
}

func TestPostTurnRecordFromStateDetectsPreviousToolTurn(t *testing.T) {
	state := newAgentState([]llm.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "call-1", Name: "web_fetch"}}},
		{Role: "tool", ToolCallID: "call-1", Content: "Error: timeout"},
		{Role: "assistant", Content: "sorry"},
		{Role: "user", Content: "no wrong"},
		{Role: "assistant", Content: "ok"},
	})
	turn := postTurnRecordFromState(TurnRecord{}, state, "run-fallback")
	if turn.RunID != "run-fallback" || turn.UserMessage != "no wrong" {
		t.Fatalf("turn = %+v", turn)
	}
	if !turn.PreviousTurnHadToolCall || len(turn.PreviousToolNames) != 1 || turn.PreviousToolNames[0] != "web_fetch" {
		t.Fatalf("previous tool detection failed: %+v", turn)
	}
}

func TestPostTurnRecordFromStateCapturesOperationalWrites(t *testing.T) {
	state := newAgentState([]llm.Message{
		{Role: "user", Content: "learn this"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{
			ID:   "call-op",
			Name: "propose_patch",
			Arguments: map[string]any{
				"action":      "operational",
				"tool_name":   "web_fetch",
				"error_class": "timeout",
				"lesson":      "Retry with a smaller page.",
				"priority":    "high",
			},
		}}},
		{Role: "tool", ToolCallID: "call-op", Content: `propose_patch: operational lesson for "web_fetch" accepted and stored immediately`},
	})
	turn := postTurnRecordFromState(TurnRecord{}, state, "run-fallback")
	if len(turn.OperationalWrites) != 1 {
		t.Fatalf("OperationalWrites = %+v, want one", turn.OperationalWrites)
	}
	got := turn.OperationalWrites[0]
	if got.ID != operationalLessonID("web_fetch", "timeout", "Retry with a smaller page.") ||
		got.ToolName != "web_fetch" ||
		got.ErrorClass != "timeout" ||
		got.Text != "Retry with a smaller page." ||
		got.Priority != memoryindex.PriorityHigh {
		t.Fatalf("captured write = %+v", got)
	}
}

func TestRunFiresPostTurnHooksAfterTurn(t *testing.T) {
	store := openPostHookStore(t)
	ch := make(chan TurnRecord, 1)
	state := &fakeRTState{messages: []llm.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", ToolCalls: []llm.ToolCall{{ID: "old-call", Name: "web_fetch"}}},
		{Role: "tool", ToolCallID: "old-call", Content: "Error: timeout"},
		{Role: "assistant", Content: "sorry"},
		{Role: "user", Content: "no wrong"},
	}}
	_, err := Run(context.Background(), Invocation{
		Client:   &fakeRTClient{response: ChatResponse{Response: llm.Response{Content: "ok"}}},
		Executor: ToolExecutorFunc(func(context.Context, []llm.ToolCall) ExecutionSummary { return ExecutionSummary{} }),
		State:    state,
		Tools:    []llm.ToolDefinition{{Name: "web_fetch"}},
		Options:  Options{MaxIterations: 1},
		PostTurn: PostTurnConfig{
			Store: store,
			Hooks: []PostTurnHook{recordingPostTurnHook{ch: ch}},
			Record: TurnRecord{
				RunID:    "run-durable",
				ThreadID: "thread-1",
			},
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	select {
	case turn := <-ch:
		if turn.RunID != "run-durable" || turn.ThreadID != "thread-1" || turn.UserMessage != "no wrong" {
			t.Fatalf("turn = %+v", turn)
		}
		if !turn.PreviousTurnHadToolCall || len(turn.PreviousToolNames) != 1 || turn.PreviousToolNames[0] != "web_fetch" {
			t.Fatalf("previous tool turn not captured: %+v", turn)
		}
	case <-time.After(time.Second):
		t.Fatal("post-turn hook did not fire")
	}
}
