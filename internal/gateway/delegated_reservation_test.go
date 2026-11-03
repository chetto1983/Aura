package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/agent/tools"
	"github.com/chetto1983/aura/internal/config"
	"github.com/chetto1983/aura/internal/idempotency"
	"github.com/chetto1983/aura/internal/toolinvocations"
)

// delegatedLifecycleContext is the ctx a SWARM WORKER's dispatch runs under: an
// acquired operation with its claim, the reservation key the gateway opened the
// `start` row under, and the marker saying no Runner observes this stream.
func delegatedLifecycleContext(t *testing.T, delegated bool) context.Context {
	t.Helper()
	ctx := gatewayOperationContext(t, "delegated-key", json.RawMessage(`{"command":"ls"}`))
	ctx, err := idempotency.WithClaimToken(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	ctx = WithReservationKey(ctx, testKey(), "shell_exec", json.RawMessage(`{"command":"ls"}`), time.Now())
	if delegated {
		ctx = WithDelegatedDispatch(ctx)
	}
	return ctx
}

func endRows(events []toolinvocations.Event) []toolinvocations.Event {
	var out []toolinvocations.Event
	for _, e := range events {
		if e.Event == toolinvocations.EventEnd {
			out = append(out, e)
		}
	}
	return out
}

// TestFinishCallClosesADelegatedMutation pins the fix for the defect
// spike 099 caught live: the gateway opens a `start` row wherever it reserves, but
// the `end` is written by the Runner from the turn's event frames — and a swarm
// worker's frames never reach a Runner, because runChild consumes worker.Run(ic)
// itself. Every worker tool call therefore opened a reservation nothing could
// close, and 30 minutes later the reconciler stamped the SUCCEEDED call as
// "crash-orphaned ... indeterminate outcome" in an append-only ledger. Measured at
// 5/5 workers against 0/3 parent calls.
//
// The rule taken from LibreChat (D-00): whoever opens a record closes it, so no
// component ever has to find and close a row another one opened.
func TestFinishCallClosesADelegatedMutation(t *testing.T) {
	t.Parallel()

	ledger := &fakeStore{}
	registry := &fakeOperationRegistry{}
	g := New(config.ProfileSingleUserHardened, ledger)
	g.SetOperationRegistry(registry)

	result := tools.ToolResult{Preview: "total 4\n", Bytes: 8}
	ctx := delegatedLifecycleContext(t, true)
	// Execution owns the audit outcome; operation transitions must not preempt it
	// with an incomplete first-writer row or write the same terminal event twice.
	g.FinishCall(ctx, result, nil)
	if err := g.CompleteOperation(ctx, result); err != nil {
		t.Fatalf("CompleteOperation: %v", err)
	}

	ends := endRows(ledger.calls())
	if len(ends) != 1 {
		t.Fatalf("delegated completion wrote %d end row(s), want 1 — the reservation stays orphaned and the reconciler will mark this succeeded call indeterminate", len(ends))
	}
	if ends[0].Status != "ok" {
		t.Errorf("end status = %q, want ok", ends[0].Status)
	}
	if ends[0].ConversationID != testKey().ConversationID || ends[0].ToolCallID != testKey().ToolCallID {
		t.Errorf("end row key = %s/%s, want the reservation's %s/%s",
			ends[0].ConversationID, ends[0].ToolCallID, testKey().ConversationID, testKey().ToolCallID)
	}
}

// TestFinishCallLeavesAnObservedDispatchToTheRunner is the other half of the
// rule: a parent's dispatch IS observed by the Runner, which writes a richer end
// (exit code, duration, sidecar path). A second writer there would win the
// ON CONFLICT DO NOTHING race and replace that row with a poorer one, so the
// gateway must stay out of it. Parent calls measured clean at 3 starts / 3 ends.
func TestFinishCallLeavesAnObservedDispatchToTheRunner(t *testing.T) {
	t.Parallel()

	ledger := &fakeStore{}
	registry := &fakeOperationRegistry{}
	g := New(config.ProfileSingleUserHardened, ledger)
	g.SetOperationRegistry(registry)

	ctx := delegatedLifecycleContext(t, false)
	g.FinishCall(ctx, tools.ToolResult{Preview: "x"}, nil)
	if err := g.CompleteOperation(ctx, tools.ToolResult{Preview: "x"}); err != nil {
		t.Fatalf("CompleteOperation: %v", err)
	}
	if got := len(endRows(ledger.calls())); got != 0 {
		t.Fatalf("undelegated completion wrote %d end row(s), want 0 — it would race the Runner's richer end", got)
	}
}

// TestFinishCallClosesAnIndeterminateDelegatedMutation covers the failure leg:
// a worker call that ends ambiguously must still close its own row, or it orphans
// exactly like the success leg did.
func TestFinishCallClosesAnIndeterminateDelegatedMutation(t *testing.T) {
	t.Parallel()

	ledger := &fakeStore{}
	registry := &fakeOperationRegistry{}
	g := New(config.ProfileSingleUserHardened, ledger)
	g.SetOperationRegistry(registry)

	ctx := delegatedLifecycleContext(t, true)
	g.FinishCall(ctx, tools.ToolResult{}, errors.New("transport interrupted"))
	if err := g.MarkOperationIndeterminate(ctx); err != nil {
		t.Fatalf("MarkOperationIndeterminate: %v", err)
	}
	ends := endRows(ledger.calls())
	if len(ends) != 1 {
		t.Fatalf("delegated indeterminate wrote %d end row(s), want 1", len(ends))
	}
	if ends[0].Status != "error" {
		t.Errorf("end status = %q, want error", ends[0].Status)
	}
}

// TestDelegatedCloseIsInertWithoutALedger mirrors the nil-safety the rest of the
// gateway's terminal hooks already have: a gateway composed without a store records
// nothing and must not panic doing it.
func TestDelegatedCloseIsInertWithoutALedger(t *testing.T) {
	t.Parallel()

	registry := &fakeOperationRegistry{}
	g := New(config.ProfileSingleUserHardened, nil)
	g.SetOperationRegistry(registry)
	g.FinishCall(delegatedLifecycleContext(t, true), tools.ToolResult{Preview: "x"}, nil)

	if err := g.CompleteOperation(delegatedLifecycleContext(t, true), tools.ToolResult{Preview: "x"}); err != nil {
		t.Fatalf("CompleteOperation without a ledger: %v", err)
	}
	if len(registry.complete) != 1 {
		t.Fatalf("the registry completion must still happen: %d call(s)", len(registry.complete))
	}
}

// TestDelegatedCloseWithoutAReservationKeyDoesNotInventOne pins the fail-loud path:
// a delegated dispatch that reached a terminal hook with no recorded key cannot
// address its row, and guessing one would write an `end` onto some other call's
// slot. It writes nothing and warns, leaving the reconciler as the backstop it
// already was.
func TestDelegatedCloseWithoutAReservationKeyDoesNotInventOne(t *testing.T) {
	t.Parallel()

	ledger := &fakeStore{}
	registry := &fakeOperationRegistry{}
	g := New(config.ProfileSingleUserHardened, ledger)
	g.SetOperationRegistry(registry)

	ctx := gatewayOperationContext(t, "keyless", json.RawMessage(`{"command":"ls"}`))
	ctx, err := idempotency.WithClaimToken(ctx, 7)
	if err != nil {
		t.Fatal(err)
	}
	ctx = WithDelegatedDispatch(ctx)
	g.FinishCall(ctx, tools.ToolResult{Preview: "x"}, nil)
	if err := g.CompleteOperation(ctx, tools.ToolResult{Preview: "x"}); err != nil {
		t.Fatalf("CompleteOperation: %v", err)
	}
	if got := len(endRows(ledger.calls())); got != 0 {
		t.Fatalf("wrote %d end row(s) with no key to address them", got)
	}
}

// TestDelegatedCloseSurvivesALedgerFailure holds the ledger to its documented
// contract: it is operational observability, not a permission system. The mutating
// effect has already happened by the time the row is written, so a failed insert
// must not turn a successful mutation into a reported error.
func TestDelegatedCloseSurvivesALedgerFailure(t *testing.T) {
	t.Parallel()

	ledger := &fakeStore{err: errors.New("pool exhausted")}
	registry := &fakeOperationRegistry{}
	g := New(config.ProfileSingleUserHardened, ledger)
	g.SetOperationRegistry(registry)
	g.FinishCall(delegatedLifecycleContext(t, true), tools.ToolResult{Preview: "x"}, nil)

	if err := g.CompleteOperation(delegatedLifecycleContext(t, true), tools.ToolResult{Preview: "x"}); err != nil {
		t.Fatalf("a failed audit row must not fail the completed mutation: %v", err)
	}
}

// readOnlyCallContext is the ctx a read-only call runs under after Decide recorded its
// decision fact: the reservation key, no operation claim, and the delegated marker when no
// Runner observes the stream.
func readOnlyCallContext(delegated bool) context.Context {
	ctx := WithReservationKey(context.Background(), testKey(), "web_search", json.RawMessage(`{"query":"AAPL"}`), time.Now())
	if delegated {
		ctx = WithDelegatedDispatch(ctx)
	}
	return ctx
}

// TestFinishCallClosesADelegatedDecisionFact is the read-only half of spike 099's rule. Under
// a strict profile Decide opens a `start` decision fact for every read-only call and leaves
// its `end` to the Runner, so a call no Runner observes (a swarm worker, a scheduled goal)
// stayed open: measured on the lab VM on 2026-10-14, 12 of one goal's calls read as live runs
// until the reconciler stamped them indeterminate.
func TestFinishCallClosesADelegatedDecisionFact(t *testing.T) {
	t.Parallel()

	ledger := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, ledger)
	g.FinishCall(readOnlyCallContext(true), tools.ToolResult{Preview: "3 results", Bytes: 9}, nil)

	ends := endRows(ledger.calls())
	if len(ends) != 1 {
		t.Fatalf("a delegated read-only call wrote %d end row(s), want 1", len(ends))
	}
	if ends[0].Status != "ok" || ends[0].ResultPreview != "3 results" || ends[0].ToolName != "web_search" {
		t.Errorf("end = status %q preview %q tool %q, want ok / 3 results / web_search",
			ends[0].Status, ends[0].ResultPreview, ends[0].ToolName)
	}
	if ends[0].ToolCallID != testKey().ToolCallID {
		t.Errorf("end tool_call_id = %q, want the decision fact's %q", ends[0].ToolCallID, testKey().ToolCallID)
	}
}

func TestFinishCallRecordsAFailedDelegatedCall(t *testing.T) {
	t.Parallel()

	ledger := &fakeStore{}
	g := New(config.ProfileSingleUserHardened, ledger)
	g.FinishCall(readOnlyCallContext(true), tools.ToolResult{}, errors.New("upstream timed out"))

	ends := endRows(ledger.calls())
	if len(ends) != 1 || ends[0].Status != "error" || ends[0].Error != "upstream timed out" {
		t.Fatalf("ends = %+v, want one error row carrying the tool's error", ends)
	}
}

func TestFinishCallRecordsCancellationMetadataAsALedgerError(t *testing.T) {
	t.Parallel()
	ledger := &fakeStore{}
	meta := tools.ToolResultMeta{"cancelled": true}
	New(config.ProfileSingleUserHardened, ledger).FinishCall(readOnlyCallContext(true), tools.ToolResult{Meta: &meta}, nil)
	ends := endRows(ledger.calls())
	if len(ends) != 1 || ends[0].Status != "error" || ends[0].Error != "context canceled" {
		t.Fatalf("ends = %+v, want a canceled tool result represented by the ledger's accepted error status", ends)
	}
}

// TestFinishCallLeavesWhatItDidNotOpen covers the two calls the hook must not touch: a
// parent's, whose richer end the Runner writes, and any call under a profile where Decide
// wrote no start at all.
func TestFinishCallLeavesWhatItDidNotOpen(t *testing.T) {
	t.Parallel()

	observed := &fakeStore{}
	New(config.ProfileSingleUserHardened, observed).FinishCall(readOnlyCallContext(false), tools.ToolResult{Preview: "x"}, nil)
	if got := len(observed.calls()); got != 0 {
		t.Errorf("a Runner-observed call got %d row(s) from the gateway, want 0", got)
	}

	dev := &fakeStore{}
	New(config.ProfileDev, dev).FinishCall(readOnlyCallContext(true), tools.ToolResult{Preview: "x"}, nil)
	if got := len(dev.calls()); got != 0 {
		t.Errorf("a dev-profile call got %d row(s), want 0: the gateway opened none", got)
	}
}

// A scheduled job's dispatch is unobserved without being a worker's, and it is the
// unobserved half that obliges the gateway to close the row.
func TestFinishCallClosesAnUnobservedDecisionFact(t *testing.T) {
	t.Parallel()

	ledger := &fakeStore{}
	ctx := tools.WithUnobservedDispatch(WithReservationKey(context.Background(), testKey(), "web_search", nil, time.Now()))
	New(config.ProfileSingleUserHardened, ledger).FinishCall(ctx, tools.ToolResult{Preview: "x"}, nil)

	ends := endRows(ledger.calls())
	if len(ends) != 1 || ends[0].Status != "ok" {
		t.Fatalf("ends = %+v, want one ok row for an unobserved dispatch", ends)
	}
}
