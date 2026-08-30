package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/identityctx"
)

// swarm_status_test.go is daemon-free coverage for swarm_status (51-11 Task 4,
// SWARM-10's missing leg): the not-found branch, the tail_events clamp, the
// empty-tail branch, elapsed_sec's exact truncation rule, and the untrusted
// provenance envelope. The adapter that actually reads a durable job row and a
// transcript (cmd/aura/swarm_status_adapter.go) is out of this package's reach
// by design (D-02 closed shape) -- this file exercises the tool's own contract
// against a fake reader.

// fakeSwarmStatusReader records the arguments WorkerStatus was called with and
// returns a scripted result, so a test can assert both what the tool asked for
// and what it did with the answer.
type fakeSwarmStatusReader struct {
	statuses []SwarmWorkerStatus
	err      error

	gotConversationID string
	gotChildID        string
	gotTailEvents     int
}

func (f *fakeSwarmStatusReader) WorkerStatus(_ context.Context, conversationID, childID string, tailEvents int) ([]SwarmWorkerStatus, error) {
	f.gotConversationID, f.gotChildID, f.gotTailEvents = conversationID, childID, tailEvents
	if f.err != nil {
		return nil, f.err
	}
	return f.statuses, nil
}

// swarmStatusTestContext mirrors toolTestContext (document_search_test.go) but
// also binds an identity -- swarm_status's own Execute refuses an identity-less
// call before ever reaching the reader, unlike document_search's owner-falls-
// back-to-local posture.
func swarmStatusTestContext(t *testing.T) context.Context {
	t.Helper()
	ctx := WithToolCallContext(t.Context(), "conv-1", "toolcall", t.TempDir(), 4096)
	return identityctx.WithIdentityID(ctx, "identity-1")
}

func swarmStatusPayload(t *testing.T, result ToolResult) []SwarmWorkerStatus {
	t.Helper()
	var payload []SwarmWorkerStatus
	if err := json.Unmarshal([]byte(result.Preview), &payload); err != nil {
		t.Fatalf("result is not a JSON array (%q): %v", result.Preview, err)
	}
	return payload
}

// TestSwarmStatusListsAllWorkersOfTheConversation: no child_id lists every
// worker, and the reader is called with the conversation id resolved from the
// tool-call context's session id.
func TestSwarmStatusListsAllWorkersOfTheConversation(t *testing.T) {
	reader := &fakeSwarmStatusReader{statuses: []SwarmWorkerStatus{
		{ChildID: "w1", Goal: "goal one", Status: "running"},
		{ChildID: "w2", Goal: "goal two", Status: "succeeded"},
	}}
	tool := &SwarmStatus{Reader: reader}

	result, err := tool.Execute(swarmStatusTestContext(t), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	payload := swarmStatusPayload(t, result)
	if len(payload) != 2 {
		t.Fatalf("payload = %#v, want 2 entries", payload)
	}
	if reader.gotConversationID != "conv-1" {
		t.Fatalf("reader conversation id = %q, want conv-1 (from the tool-call session)", reader.gotConversationID)
	}
	if reader.gotChildID != "" {
		t.Fatalf("reader child id = %q, want empty for a listing call", reader.gotChildID)
	}
}

// TestSwarmStatusReturnsExactlyOneEntryForAKnownChildID.
func TestSwarmStatusReturnsExactlyOneEntryForAKnownChildID(t *testing.T) {
	reader := &fakeSwarmStatusReader{statuses: []SwarmWorkerStatus{
		{ChildID: "w1", Goal: "goal one", Status: "running"},
	}}
	tool := &SwarmStatus{Reader: reader}

	result, err := tool.Execute(swarmStatusTestContext(t), json.RawMessage(`{"child_id":"w1"}`))
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	payload := swarmStatusPayload(t, result)
	if len(payload) != 1 || payload[0].ChildID != "w1" {
		t.Fatalf("payload = %#v, want exactly the w1 entry", payload)
	}
	if reader.gotChildID != "w1" {
		t.Fatalf("reader child id = %q, want w1", reader.gotChildID)
	}
}

// TestSwarmStatusUnknownChildIDIsModelReadableNotFound: an unknown/foreign
// child_id is a NAMED not-found message, never a Go error, never an empty
// success that reads as "the worker has nothing to report" (this task's own
// behavior spec).
func TestSwarmStatusUnknownChildIDIsModelReadableNotFound(t *testing.T) {
	tool := &SwarmStatus{Reader: &fakeSwarmStatusReader{statuses: nil}}

	result, err := tool.Execute(swarmStatusTestContext(t), json.RawMessage(`{"child_id":"ghost-worker"}`))
	if err != nil {
		t.Fatalf("Execute = %v, want a model-readable string, not a Go error", err)
	}
	if !strings.Contains(result.Preview, "ghost-worker") {
		t.Fatalf("result = %q, want it to name the unknown child_id", result.Preview)
	}
	if strings.HasPrefix(strings.TrimSpace(result.Preview), "[") {
		t.Fatalf("result = %q, want a prose not-found message, not an empty JSON array", result.Preview)
	}
}

// TestSwarmStatusEmptyListingIsAPlainEmptyArray: no child_id, no workers -- a
// plain empty array, never the not-found message (that branch is reserved for
// an EXPLICIT unknown child_id).
func TestSwarmStatusEmptyListingIsAPlainEmptyArray(t *testing.T) {
	tool := &SwarmStatus{Reader: &fakeSwarmStatusReader{statuses: nil}}

	result, err := tool.Execute(swarmStatusTestContext(t), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	payload := swarmStatusPayload(t, result)
	if len(payload) != 0 {
		t.Fatalf("payload = %#v, want empty", payload)
	}
}

// TestSwarmStatusEmptyTailIsNotAnError: a worker with no transcript yet
// returns an entry with an empty tail and no last_event_at, not an error.
func TestSwarmStatusEmptyTailIsNotAnError(t *testing.T) {
	reader := &fakeSwarmStatusReader{statuses: []SwarmWorkerStatus{
		{ChildID: "w1", Goal: "goal", Status: "queued"},
	}}
	tool := &SwarmStatus{Reader: reader}

	result, err := tool.Execute(swarmStatusTestContext(t), json.RawMessage(`{"child_id":"w1"}`))
	if err != nil {
		t.Fatalf("Execute = %v", err)
	}
	payload := swarmStatusPayload(t, result)
	if len(payload) != 1 {
		t.Fatalf("payload = %#v, want 1 entry", payload)
	}
	if len(payload[0].Tail) != 0 || payload[0].LastEventAt != "" {
		t.Fatalf("entry = %#v, want an empty tail and no last_event_at", payload[0])
	}
}

// TestSwarmStatusClampsTailEvents covers the declared bounds: 0/negative fall
// back to the default, values within range pass through, and anything above
// the max is clamped down to it -- never rejected.
func TestSwarmStatusClampsTailEvents(t *testing.T) {
	cases := []struct {
		requested int
		want      int
	}{
		{requested: 0, want: swarmStatusDefaultTailEvents},
		{requested: -5, want: swarmStatusDefaultTailEvents},
		{requested: 1, want: 1},
		{requested: 100, want: 100},
		{requested: 1000, want: swarmStatusMaxTailEvents},
	}
	for _, tc := range cases {
		reader := &fakeSwarmStatusReader{}
		tool := &SwarmStatus{Reader: reader}
		args, err := json.Marshal(swarmStatusArgs{TailEvents: tc.requested})
		if err != nil {
			t.Fatalf("marshal args: %v", err)
		}
		if _, err := tool.Execute(swarmStatusTestContext(t), args); err != nil {
			t.Fatalf("Execute(tail_events=%d) = %v", tc.requested, err)
		}
		if reader.gotTailEvents != tc.want {
			t.Errorf("tail_events %d -> reader saw %d, want %d", tc.requested, reader.gotTailEvents, tc.want)
		}
	}
}

// TestSwarmStatusResultCarriesUntrustedProvenance: the tail is a worker's own
// output crossing back into the parent's context and must be enveloped as
// untrusted, matching runner_adapter.go's own swarm_spawn line -- true for
// both the real result and the not-found message.
func TestSwarmStatusResultCarriesUntrustedProvenance(t *testing.T) {
	t.Run("real result", func(t *testing.T) {
		reader := &fakeSwarmStatusReader{statuses: []SwarmWorkerStatus{{ChildID: "w1"}}}
		tool := &SwarmStatus{Reader: reader}
		result, err := tool.Execute(swarmStatusTestContext(t), json.RawMessage(`{}`))
		if err != nil {
			t.Fatalf("Execute = %v", err)
		}
		if result.Provenance == nil || result.Provenance.Source != "swarm" || result.Provenance.Trust != TrustUntrusted {
			t.Fatalf("provenance = %#v", result.Provenance)
		}
	})
	t.Run("not-found message", func(t *testing.T) {
		tool := &SwarmStatus{Reader: &fakeSwarmStatusReader{}}
		result, err := tool.Execute(swarmStatusTestContext(t), json.RawMessage(`{"child_id":"ghost"}`))
		if err != nil {
			t.Fatalf("Execute = %v", err)
		}
		if result.Provenance == nil || result.Provenance.Trust != TrustUntrusted {
			t.Fatalf("provenance = %#v", result.Provenance)
		}
	})
}

// TestSwarmStatusMissingConversationContext / TestSwarmStatusMissingIdentity:
// a call dispatched outside an active turn degrades to a model-readable inline
// error, never a Go error and never a panic.
func TestSwarmStatusMissingConversationContext(t *testing.T) {
	tool := &SwarmStatus{Reader: &fakeSwarmStatusReader{}}
	result, err := tool.Execute(identityctx.WithIdentityID(t.Context(), "identity-1"), json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute = %v, want a model-readable string, not a Go error", err)
	}
	if !strings.Contains(result.Preview, "conversation") {
		t.Fatalf("result = %q, want it to name the missing conversation context", result.Preview)
	}
	if result.Provenance == nil || result.Provenance.Trust != TrustUntrusted {
		t.Fatalf("provenance = %#v, want untrusted", result.Provenance)
	}
}

func TestSwarmStatusMissingIdentity(t *testing.T) {
	tool := &SwarmStatus{Reader: &fakeSwarmStatusReader{}}
	ctx := WithToolCallContext(t.Context(), "conv-1", "toolcall", t.TempDir(), 4096)
	result, err := tool.Execute(ctx, json.RawMessage(`{}`))
	if err != nil {
		t.Fatalf("Execute = %v, want a model-readable string, not a Go error", err)
	}
	if !strings.Contains(result.Preview, "identity") {
		t.Fatalf("result = %q, want it to name the missing identity context", result.Preview)
	}
	if result.Provenance == nil || result.Provenance.Trust != TrustUntrusted {
		t.Fatalf("provenance = %#v, want untrusted", result.Provenance)
	}
}

// TestSwarmStatusReaderErrorIsAHardError: an infrastructure failure from the
// reader (a DB error) IS a real Go error -- unlike an unknown child_id, this
// is not a domain rejection the model can self-correct from.
func TestSwarmStatusReaderErrorIsAHardError(t *testing.T) {
	tool := &SwarmStatus{Reader: &fakeSwarmStatusReader{err: errors.New("db down")}}
	if _, err := tool.Execute(swarmStatusTestContext(t), json.RawMessage(`{}`)); err == nil {
		t.Fatal("Execute with a failing reader = nil error, want the infrastructure failure surfaced")
	}
}

// TestSwarmStatusNoReaderIsAWiringError mirrors swarm_spawn's own "a missing
// runner is a real Go error" contract.
func TestSwarmStatusNoReaderIsAWiringError(t *testing.T) {
	tool := &SwarmStatus{}
	if _, err := tool.Execute(swarmStatusTestContext(t), json.RawMessage(`{}`)); err == nil {
		t.Fatal("Execute with no Reader configured = nil error, want a wiring error")
	}
}

// TestSwarmStatusElapsedTruncates pins SwarmWorkerElapsedSeconds' own rule
// (acceptance criteria's own named case): 4.9 seconds reports 4, truncated
// toward zero, never rounded.
func TestSwarmStatusElapsedTruncates(t *testing.T) {
	start := time.Date(2026, 8, 29, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		end  time.Time
		want int64
	}{
		{name: "4.9s truncates to 4", end: start.Add(4900 * time.Millisecond), want: 4},
		{name: "exactly 5s", end: start.Add(5 * time.Second), want: 5},
		{name: "0.1s truncates to 0", end: start.Add(100 * time.Millisecond), want: 0},
		{name: "zero duration", end: start, want: 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SwarmWorkerElapsedSeconds(start, tc.end); got != tc.want {
				t.Errorf("SwarmWorkerElapsedSeconds = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestCapSwarmStatusDetailTruncatesOnARuneBoundary: the 200-rune cap truncates
// multibyte text without splitting a rune, mirroring delegation_card.go's own
// capRunes discipline.
func TestCapSwarmStatusDetailTruncatesOnARuneBoundary(t *testing.T) {
	short := "a short detail"
	if got := CapSwarmStatusDetail(short); got != short {
		t.Fatalf("CapSwarmStatusDetail(short) = %q, want it unchanged", got)
	}
	long := strings.Repeat("é", 250) // multibyte, well past the 200-rune cap
	got := CapSwarmStatusDetail(long)
	if n := len([]rune(got)); n != 200 {
		t.Fatalf("CapSwarmStatusDetail(long) rune count = %d, want 200", n)
	}
}

// TestSwarmStatusSpecIsDeferred pins the manifest posture: a big schema/
// description tool stays out of the default manifest until tool_search loads it.
func TestSwarmStatusSpecIsDeferred(t *testing.T) {
	spec := (&SwarmStatus{}).Spec()
	if !spec.Deferred {
		t.Fatal("SwarmStatus.Spec().Deferred = false, want true")
	}
	if spec.Mutating {
		t.Fatal("SwarmStatus.Spec().Mutating = true, want false (a pure read)")
	}
	if spec.Summary == "" || spec.Description == "" {
		t.Fatal("Summary and Description must both be non-empty")
	}
}

func TestSwarmStatusSpecRequiresFactBasedOperatorAnswer(t *testing.T) {
	spec := (&SwarmStatus{}).Spec()
	for surface, text := range map[string]string{
		"summary":     spec.Summary,
		"description": spec.Description,
	} {
		for _, field := range []string{"child_id", "status", "elapsed_sec", "recent activity"} {
			if !strings.Contains(text, field) {
				t.Errorf("%s does not require %q in the operator answer", surface, field)
			}
		}
	}
}
