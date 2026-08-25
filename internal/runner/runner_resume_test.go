package runner

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/llm"
)

func setPendingDecisionPolicy(t *testing.T, pause *fakePauseStore, token string, decisions ...string) {
	t.Helper()
	raw, err := json.Marshal(map[string]any{"allowed_decisions": decisions})
	if err != nil {
		t.Fatal(err)
	}
	pause.mu.Lock()
	defer pause.mu.Unlock()
	pending, ok := pause.byToken[token]
	if !ok {
		t.Fatalf("pending token %s not found", token)
	}
	pending.ResumeContext = raw
}

// runner_resume_test.go pins the HTTP-resolve bridge invariant the APRV-02 approval
// adapter (internal/agui/approvals_api.go) depends on: SubmitAnswers maps each of the
// three verbs (accept | decline | cancel) to the correct effect, and — the load-bearing
// "deny ≠ accept" guard (Pitfall 4 / T-25-08) — decline injects declinedContent, NOT
// the operator-supplied content. These use the package's in-memory fakes (the runner
// test convention — there is no db_integration tier in this package; the real-store
// round-trip is covered by the agui approvals db_integration suite + live_e2e).

// seedSinglePause drives a one-pause round and returns the convID + the pause token.
func seedSinglePause(t *testing.T, r *Runner, conv *fakeConvStore) (string, string) {
	t.Helper()
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, new("ask me"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, err := r.PendingFor(ctx, convID)
	if err != nil || len(pending) == 0 {
		t.Fatalf("expected a pending pause, got %d (err=%v)", len(pending), err)
	}
	return convID, pending[0].Token
}

// TestSubmitAnswers_AcceptInjectsOperatorContent: the accept verb injects the
// operator-supplied content verbatim as the RoleTool answer (the happy path the
// approval adapter forwards as {action:"accept", content:"<text>"}).
func TestSubmitAnswers_AcceptInjectsOperatorContent(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-1", "Which city?", "clarification")),
		agenttest.ToolCallTurn(textResponseCall("call-2", "Got it.")),
	)
	r, conv, _ := newTestRunner(t, client)
	convID, token := seedSinglePause(t, r, conv)

	if _, err := r.SubmitAnswers(context.Background(), map[string]ResponseInput{
		token: {Action: askuser.ActionAccept, Content: "Rome"},
	}); err != nil {
		t.Fatalf("SubmitAnswers(accept): %v", err)
	}
	if !historyHasToolContent(t, conv, convID, "Rome") {
		t.Fatal("accept must inject the operator content (\"Rome\") as the RoleTool answer")
	}
}

// TestSubmitAnswers_DeclineInjectsDeclinedContent is the deny≠accept invariant: a
// decline maps to declinedContent ("user declined to answer"), NEVER the operator
// text, so the agent continues INFORMED of the refusal (Claude-Code "deny" semantics,
// D-05). Asserts the injected content is declinedContent and the operator text is absent.
func TestSubmitAnswerRejectsEmptyAcceptBeforeCommit(t *testing.T) {
	for _, input := range []struct {
		name    string
		content string
	}{
		{name: "empty", content: ""},
		{name: "whitespace", content: " \t\r\n"},
	} {
		t.Run(input.name, func(t *testing.T) {
			tests := []struct {
				name string
				run  func(*Runner, string) error
			}{
				{name: "single", run: func(r *Runner, token string) error {
					_, err := r.SubmitAnswer(context.Background(), token, ResponseInput{Action: askuser.ActionAccept, Content: input.content})
					return err
				}},
				{name: "batch", run: func(r *Runner, token string) error {
					_, err := r.SubmitAnswers(context.Background(), map[string]ResponseInput{
						token: {Action: askuser.ActionAccept, Content: input.content},
					})
					return err
				}},
			}
			for _, tc := range tests {
				t.Run(tc.name, func(t *testing.T) {
					r, conv, pause := newTestRunner(t, agenttest.NewFakeClient(
						agenttest.ToolCallTurn(askUserCall("call-empty", "Approve?", "approval")),
					))
					convID, token := seedSinglePause(t, r, conv)
					err := tc.run(r, token)
					if !errors.Is(err, askuser.ErrInvalidAnswer) {
						t.Fatalf("empty accept: want ErrInvalidAnswer before commit, got %v", err)
					}
					if got := pause.unresolvedCount(convID); got != 1 {
						t.Fatalf("empty accept claimed the pause: unresolved = %d, want 1", got)
					}
				})
			}
		})
	}
}

func TestResumeRejectsDisallowedDecision(t *testing.T) {
	r, conv, pause := newTestRunner(t, agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-policy", "Proceed?", "approval")),
	))
	convID, token := seedSinglePause(t, r, conv)
	setPendingDecisionPolicy(t, pause, token, askuser.ActionDecline)

	if _, err := r.SubmitAnswer(context.Background(), token, ResponseInput{
		Action: askuser.ActionAccept, Content: "yes",
	}); !errors.Is(err, ErrResumeDecisionNotAllowed) {
		t.Fatalf("disallowed accept: want ErrResumeDecisionNotAllowed, got %v", err)
	}
	if got := pause.unresolvedCount(convID); got != 1 {
		t.Fatalf("disallowed decision claimed the pause: unresolved = %d, want 1", got)
	}
	if _, err := r.SubmitAnswer(context.Background(), token, ResponseInput{
		Action: askuser.ActionDecline,
	}); err != nil {
		t.Fatalf("corrected retry must succeed: %v", err)
	}
	if got := pause.unresolvedCount(convID); got != 0 {
		t.Fatalf("corrected retry left %d unresolved pauses, want 0", got)
	}
}

func TestValidationRejectsBeforeMarkResumed(t *testing.T) {
	r, _, pause := newTestRunner(t, agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-policy-a", "A?", "approval"),
			askUserCall("call-policy-b", "B?", "approval"),
		),
	))
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, new("ask twice"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, err := r.PendingFor(ctx, convID)
	if err != nil || len(pending) != 2 {
		t.Fatalf("want 2 pending pauses, got %d (err=%v)", len(pending), err)
	}
	setPendingDecisionPolicy(t, pause, pending[1].Token, askuser.ActionDecline)

	badBatch := map[string]ResponseInput{
		pending[0].Token: {Action: askuser.ActionAccept, Content: "answer-a"},
		pending[1].Token: {Action: askuser.ActionAccept, Content: "answer-b"},
	}
	if _, err := r.SubmitAnswers(ctx, badBatch); !errors.Is(err, ErrResumeDecisionNotAllowed) {
		t.Fatalf("bad batch: want ErrResumeDecisionNotAllowed, got %v", err)
	}
	if got := pause.unresolvedCount(convID); got != 2 {
		t.Fatalf("bad batch partially claimed pauses: unresolved = %d, want 2", got)
	}

	badBatch[pending[1].Token] = ResponseInput{Action: askuser.ActionDecline}
	if _, err := r.SubmitAnswers(ctx, badBatch); err != nil {
		t.Fatalf("corrected batch retry must succeed: %v", err)
	}
	if got := pause.unresolvedCount(convID); got != 0 {
		t.Fatalf("corrected batch left %d unresolved pauses, want 0", got)
	}
}

func TestIdempotencyInvariantUnchanged(t *testing.T) {
	r, conv, _ := newTestRunner(t, agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-idempotency", "Proceed?", "approval")),
	))
	_, token := seedSinglePause(t, r, conv)
	answer := ResponseInput{Action: askuser.ActionAccept, Content: "yes"}
	if _, err := r.SubmitAnswer(context.Background(), token, answer); err != nil {
		t.Fatalf("first resume: %v", err)
	}
	if _, err := r.SubmitAnswer(context.Background(), token, answer); !errors.Is(err, askuser.ErrPauseNotFound) {
		t.Fatalf("duplicate resume: want ErrPauseNotFound, got %v", err)
	}
}

func TestSubmitAnswers_DeclineInjectsDeclinedContent(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(askUserCall("call-1", "Proceed?", "approval")),
		agenttest.ToolCallTurn(textResponseCall("call-2", "Understood, skipping.")),
	)
	r, conv, _ := newTestRunner(t, client)
	convID, token := seedSinglePause(t, r, conv)

	// The operator typed something into the box, but a DECLINE must discard it.
	const operatorText = "do-not-leak-this-as-an-accepted-answer"
	if _, err := r.SubmitAnswers(context.Background(), map[string]ResponseInput{
		token: {Action: askuser.ActionDecline, Content: operatorText},
	}); err != nil {
		t.Fatalf("SubmitAnswers(decline): %v", err)
	}
	if !historyHasToolContent(t, conv, convID, declinedContent) {
		t.Fatalf("decline must inject declinedContent (%q) as the RoleTool answer", declinedContent)
	}
	if historyHasToolContent(t, conv, convID, operatorText) {
		t.Fatalf("decline must NOT inject the operator text %q (deny ≠ accept, T-25-08)", operatorText)
	}
}

// TestSubmitAnswers_CancelAutoResolves: the cancel verb aborts the run via the
// auto-resolve path, leaving zero unresolved pendings (D-05 cancel semantics).
func TestSubmitAnswers_CancelAutoResolves(t *testing.T) {
	client := agenttest.NewFakeClient(
		agenttest.ToolCallTurn(
			askUserCall("call-1", "A?", "clarification"),
			askUserCall("call-2", "B?", "clarification"),
		),
	)
	r, _, pause := newTestRunner(t, client)
	convID := newConvID(t)
	ctx := context.Background()
	mustCreate(t, r, convID)
	if _, err := drain(r.Turn(ctx, convID, new("go"))); err != nil {
		t.Fatalf("turn: %v", err)
	}
	pending, _ := pause.ListPending(ctx, convID)
	if len(pending) == 0 {
		t.Fatal("expected pendings to cancel")
	}
	remaining, err := r.SubmitAnswers(ctx, map[string]ResponseInput{
		pending[0].Token: {Action: askuser.ActionCancel},
	})
	if err != nil {
		t.Fatalf("SubmitAnswers(cancel): %v", err)
	}
	if remaining != 0 {
		t.Fatalf("cancel must auto-resolve all pendings, remaining=%d", remaining)
	}
	if pause.unresolvedCount(convID) != 0 {
		t.Fatalf("cancel must leave 0 unresolved, got %d", pause.unresolvedCount(convID))
	}
}

// TestSubmitAnswers_UnknownTokenIsPauseNotFound: a single-entry resolve of an unknown
// token surfaces ErrPauseNotFound so the HTTP adapter maps it to 404 (APRV-03 — a
// terminated pending is never silently lost).
func TestSubmitAnswers_UnknownTokenIsPauseNotFound(t *testing.T) {
	r, _, _ := newTestRunner(t, agenttest.NewFakeClient())
	_, err := r.SubmitAnswers(context.Background(), map[string]ResponseInput{
		newConvID(t): {Action: askuser.ActionAccept, Content: "x"},
	})
	if err == nil {
		t.Fatal("SubmitAnswers(unknown token): want an error, got nil")
	}
}

// historyHasToolContent reports whether the conversation history has a RoleTool turn
// whose content contains needle.
func historyHasToolContent(t *testing.T, conv *fakeConvStore, convID, needle string) bool {
	t.Helper()
	hist, err := conv.LoadHistory(context.Background(), convID)
	if err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	for _, m := range hist {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, needle) {
			return true
		}
	}
	return false
}
