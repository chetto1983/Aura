package runner

import (
	"context"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/agent/agenttest"
	"github.com/chetto1983/aura/internal/askuser"
	"github.com/chetto1983/aura/internal/llm"
)

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
	if _, err := drain(r.Turn(ctx, convID, userPtr("ask me"))); err != nil {
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
	if _, err := drain(r.Turn(ctx, convID, userPtr("go"))); err != nil {
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
