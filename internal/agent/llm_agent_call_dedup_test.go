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

func TestDedupeSameMessageCalls(t *testing.T) {
	tests := []struct {
		name string
		in   []llm.ToolCall
		want []llm.ToolCall
	}{
		{
			name: "nil input returned unchanged",
			in:   nil,
			want: nil,
		},
		{
			name: "empty input returned unchanged",
			in:   []llm.ToolCall{},
			want: []llm.ToolCall{},
		},
		{
			name: "single call returned unchanged",
			in:   []llm.ToolCall{newDedupCall("1", "fs_read", `{"path":"a"}`)},
			want: []llm.ToolCall{newDedupCall("1", "fs_read", `{"path":"a"}`)},
		},
		{
			name: "byte-identical repeat: only the first survives",
			in: []llm.ToolCall{
				newDedupCall("1", "fs_read", `{"path":"a"}`),
				newDedupCall("2", "fs_read", `{"path":"a"}`),
			},
			want: []llm.ToolCall{newDedupCall("1", "fs_read", `{"path":"a"}`)},
		},
		{
			// Proves canonicalization is applied, not a raw byte comparison.
			name: "same name, reordered-but-equivalent JSON args: only the first survives",
			in: []llm.ToolCall{
				newDedupCall("1", "fs_read", `{"path":"a","mode":"r"}`),
				newDedupCall("2", "fs_read", `{"mode":"r","path":"a"}`),
			},
			want: []llm.ToolCall{newDedupCall("1", "fs_read", `{"path":"a","mode":"r"}`)},
		},
		{
			name: "same name, genuinely different args: both survive",
			in: []llm.ToolCall{
				newDedupCall("1", "fs_read", `{"path":"a"}`),
				newDedupCall("2", "fs_read", `{"path":"b"}`),
			},
			want: []llm.ToolCall{
				newDedupCall("1", "fs_read", `{"path":"a"}`),
				newDedupCall("2", "fs_read", `{"path":"b"}`),
			},
		},
		{
			name: "different names, identical args: both survive",
			in: []llm.ToolCall{
				newDedupCall("1", "fs_read", `{"path":"a"}`),
				newDedupCall("2", "fs_stat", `{"path":"a"}`),
			},
			want: []llm.ToolCall{
				newDedupCall("1", "fs_read", `{"path":"a"}`),
				newDedupCall("2", "fs_stat", `{"path":"a"}`),
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := dedupeSameMessageCalls(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("length = %d, want %d (got ids: %v)", len(got), len(tc.want), dedupCallIDs(got))
			}
			for i := range got {
				if got[i].ID != tc.want[i].ID || got[i].Function.Name != tc.want[i].Function.Name ||
					got[i].Function.Arguments != tc.want[i].Function.Arguments {
					t.Fatalf("call %d = %+v, want %+v", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestDedupeSameMessageCallsPreservesOrder asserts positionally, not as a
// set: given [A, B, A', C] where A' repeats A, the output must be exactly
// [A, B, C] in that order. Reordering a batch changes the sequence the
// provider sees and the order side effects apply in.
func TestDedupeSameMessageCallsPreservesOrder(t *testing.T) {
	a := newDedupCall("a1", "fs_read", `{"path":"a"}`)
	b := newDedupCall("b1", "fs_write", `{"path":"b"}`)
	aRepeat := newDedupCall("a2", "fs_read", `{"path":"a"}`)
	c := newDedupCall("c1", "shell_exec", `{"cmd":"ls"}`)

	got := dedupeSameMessageCalls([]llm.ToolCall{a, b, aRepeat, c})
	wantIDs := []string{"a1", "b1", "c1"}
	gotIDs := dedupCallIDs(got)
	if len(gotIDs) != len(wantIDs) {
		t.Fatalf("got %v, want ids in order %v", gotIDs, wantIDs)
	}
	for i := range wantIDs {
		if gotIDs[i] != wantIDs[i] {
			t.Fatalf("position %d: got id %q, want %q (full: %v)", i, gotIDs[i], wantIDs[i], gotIDs)
		}
	}
}

// TestDedupeSameMessageCallsAcrossSeparateInvocationsBothSurvive proves
// cross-round behavior is explicitly NOT this function's concern (D-09/D-12
// boundary): the SAME (name, args) passed in two SEPARATE calls to
// dedupeSameMessageCalls (simulating two different rounds, each dispatched
// independently) both survive, one per call — this function has no memory
// across invocations.
func TestDedupeSameMessageCallsAcrossSeparateInvocationsBothSurvive(t *testing.T) {
	call := newDedupCall("1", "fs_read", `{"path":"a"}`)

	round1 := dedupeSameMessageCalls([]llm.ToolCall{call})
	round2 := dedupeSameMessageCalls([]llm.ToolCall{call})

	if len(round1) != 1 || len(round2) != 1 {
		t.Fatalf("each independent round call must survive on its own: round1=%v round2=%v",
			dedupCallIDs(round1), dedupCallIDs(round2))
	}
}

// TestDedupeSameMessageCallsIgnoresIDs proves ids are irrelevant to the
// decision (D-12): identical (name, arguments) with DIFFERENT ids still
// collapse to one, and the SAME id with different arguments does not.
func TestDedupeSameMessageCallsIgnoresIDs(t *testing.T) {
	t.Run("identical name+args, different ids: collapse to one", func(t *testing.T) {
		got := dedupeSameMessageCalls([]llm.ToolCall{
			newDedupCall("id-A", "fs_read", `{"path":"a"}`),
			newDedupCall("id-B", "fs_read", `{"path":"a"}`),
		})
		if len(got) != 1 {
			t.Fatalf("want 1 surviving call, got %d: %v", len(got), dedupCallIDs(got))
		}
	})
	t.Run("same id, different arguments: both survive", func(t *testing.T) {
		got := dedupeSameMessageCalls([]llm.ToolCall{
			newDedupCall("same-id", "fs_read", `{"path":"a"}`),
			newDedupCall("same-id", "fs_read", `{"path":"b"}`),
		})
		if len(got) != 2 {
			t.Fatalf("want 2 surviving calls (different args, same id must not collapse), got %d", len(got))
		}
	})
}
