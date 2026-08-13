package agent

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// newDedupCall builds a minimal llm.ToolCall for the tables below.
func newDedupCall(id, name, args string) llm.ToolCall {
	var call llm.ToolCall
	call.ID = id
	call.Type = "function"
	call.Function.Name = name
	call.Function.Arguments = args
	return call
}

func dedupCallIDs(calls []llm.ToolCall) []string {
	ids := make([]string, len(calls))
	for i, c := range calls {
		ids[i] = c.ID
	}
	return ids
}

func TestUniquifyToolCallIDs(t *testing.T) {
	tests := []struct {
		name    string
		in      []llm.ToolCall
		wantIDs []string
	}{
		{
			name:    "nil input returns nil, not a panic",
			in:      nil,
			wantIDs: nil,
		},
		{
			name:    "empty input returns empty",
			in:      []llm.ToolCall{},
			wantIDs: []string{},
		},
		{
			name:    "single call returned unchanged",
			in:      []llm.ToolCall{newDedupCall("abc", "fs_read", `{}`)},
			wantIDs: []string{"abc"},
		},
		{
			name: "pair collision: second occurrence becomes _d2",
			in: []llm.ToolCall{
				newDedupCall("abc", "fs_read", `{"path":"a"}`),
				newDedupCall("abc", "fs_write", `{"path":"b"}`),
			},
			wantIDs: []string{"abc", "abc_d2"},
		},
		{
			name: "triple collision increments the suffix",
			in: []llm.ToolCall{
				newDedupCall("abc", "fs_read", `{"path":"a"}`),
				newDedupCall("abc", "fs_write", `{"path":"b"}`),
				newDedupCall("abc", "shell_exec", `{"cmd":"c"}`),
			},
			wantIDs: []string{"abc", "abc_d2", "abc_d3"},
		},
		{
			// PROBE EDGE (collision transitivity, D-13): the repaired id for the
			// second "abc" would itself collide with the THIRD call's own original
			// id "abc_d2". A one-pass suffix counter that stops at the first free
			// number is wrong here — the third call must keep incrementing past
			// "abc_d2" until it finds a truly free id.
			name: "repaired id colliding with a later original keeps incrementing",
			in: []llm.ToolCall{
				newDedupCall("abc", "fs_read", `{"path":"a"}`),
				newDedupCall("abc", "fs_write", `{"path":"b"}`),
				newDedupCall("abc_d2", "shell_exec", `{"cmd":"c"}`),
			},
			wantIDs: []string{"abc", "abc_d2", "abc_d2_d2"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := uniquifyToolCallIDs(tc.in)
			if len(got) != len(tc.wantIDs) {
				t.Fatalf("length mismatch: got %d, want %d", len(got), len(tc.wantIDs))
			}
			gotIDs := dedupCallIDs(got)
			for i := range gotIDs {
				if gotIDs[i] != tc.wantIDs[i] {
					t.Fatalf("id[%d] = %q, want %q (all got: %v)", i, gotIDs[i], tc.wantIDs[i], gotIDs)
				}
			}
			// Every id in the repaired batch must be distinct — the whole point
			// of the repair.
			seen := make(map[string]struct{}, len(gotIDs))
			for _, id := range gotIDs {
				if _, dup := seen[id]; dup {
					t.Fatalf("output ids are not distinct: %v", gotIDs)
				}
				seen[id] = struct{}{}
			}
		})
	}
}

// TestUniquifyToolCallIDsIsDeterministic is the dedicated determinism probe
// (D-13): the same input run twice must produce byte-identical output ids.
// This is distinct from, and not satisfied by, a no-randomness-source grep —
// a pure function can still be accidentally order-dependent (e.g. ranging a
// map) without using any randomness API.
func TestUniquifyToolCallIDsIsDeterministic(t *testing.T) {
	in := []llm.ToolCall{
		newDedupCall("abc", "fs_read", `{"path":"a"}`),
		newDedupCall("abc", "fs_write", `{"path":"b"}`),
		newDedupCall("", "shell_exec", `{"cmd":"ls"}`),
	}
	in2 := make([]llm.ToolCall, len(in))
	copy(in2, in)

	first := uniquifyToolCallIDs(in)
	second := uniquifyToolCallIDs(in2)

	if len(first) != len(second) {
		t.Fatalf("length differs across runs: %d vs %d", len(first), len(second))
	}
	for i := range first {
		if first[i].ID != second[i].ID {
			t.Fatalf("id[%d] differs across runs: %q vs %q", i, first[i].ID, second[i].ID)
		}
	}
}

// TestUniquifyToolCallIDsCoalescesBlankIDs proves the blank-id fallback
// (D-13): a blank id becomes call_<first 12 hex chars of
// sha256("name:args:index")>, and the index in the hash input is what lets
// two otherwise-identical blank-id calls at different positions diverge.
func TestUniquifyToolCallIDsCoalescesBlankIDs(t *testing.T) {
	in := []llm.ToolCall{
		newDedupCall("", "web_search", `{"q":"same"}`),
		newDedupCall("", "web_search", `{"q":"same"}`),
	}
	got := uniquifyToolCallIDs(in)
	if len(got) != 2 {
		t.Fatalf("want 2 calls back, got %d", len(got))
	}
	for i, c := range got {
		if c.ID == "" {
			t.Fatalf("call %d still has a blank id", i)
		}
		if len(c.ID) != len("call_")+12 {
			t.Fatalf("call %d id %q has unexpected length %d, want %d", i, c.ID, len(c.ID), len("call_")+12)
		}
		if c.ID[:len("call_")] != "call_" {
			t.Fatalf("call %d id %q does not start with the call_ prefix", i, c.ID)
		}
	}
	if got[0].ID == got[1].ID {
		t.Fatalf("two blank-id calls with identical name+args at different indices must diverge, both got %q", got[0].ID)
	}

	// The formula itself: call_ + first 12 hex chars of sha256("name:args:index").
	// Computed independently via the standard library, not by calling the
	// function under test on itself.
	wantFirst := deterministicallyExpectedBlankID(t, "web_search", `{"q":"same"}`, 0)
	if got[0].ID != wantFirst {
		t.Fatalf("call 0 id = %q, want %q (formula mismatch)", got[0].ID, wantFirst)
	}
	wantSecond := deterministicallyExpectedBlankID(t, "web_search", `{"q":"same"}`, 1)
	if got[1].ID != wantSecond {
		t.Fatalf("call 1 id = %q, want %q (formula mismatch)", got[1].ID, wantSecond)
	}
}

// deterministicallyExpectedBlankID reproduces the documented formula
// independently (crypto/sha256 + encoding/hex from the standard library, not
// the package's own helper) so the test proves the SPECIFIED formula, not
// just "some deterministic value."
func deterministicallyExpectedBlankID(t *testing.T, name, args string, index int) string {
	t.Helper()
	sum := sha256.Sum256(fmt.Appendf(nil, "%s:%s:%d", name, args, index))
	return "call_" + hex.EncodeToString(sum[:])[:12]
}
