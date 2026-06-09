package prompt

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestVolatileHintLines pins the #52/D-41 workspace hint and the current-time
// hint: both ride the SAME per-turn trailing message as the budget (after
// history, never in the cached prefix), and each can emit without step counts.
func TestVolatileHintLines(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()
	hist := seedHistory()

	both := b.Build(hist, reg, "openrouter", cfg, Budget{Used: 1, Remaining: 2, Workspace: "D:/ws/run-1"})
	last := both.Messages[len(both.Messages)-1]
	if want := "<budget>used=1 remaining=2</budget>\n<workspace>D:/ws/run-1</workspace>"; last.Content != want {
		t.Fatalf("budget+workspace block = %q, want %q", last.Content, want)
	}

	only := b.Build(hist, reg, "openrouter", cfg, Budget{Workspace: "D:/ws/run-1"})
	last = only.Messages[len(only.Messages)-1]
	if want := "<workspace>D:/ws/run-1</workspace>"; last.Content != want {
		t.Fatalf("workspace-only block = %q, want %q", last.Content, want)
	}

	withTime := b.Build(hist, reg, "openrouter", cfg, Budget{
		Used:        1,
		Remaining:   2,
		Workspace:   "D:/ws/run-1",
		CurrentTime: "2026-06-09T12:34:56+02:00",
		Today:       "2026-06-09",
	})
	last = withTime.Messages[len(withTime.Messages)-1]
	if want := "<budget>used=1 remaining=2</budget>\n<workspace>D:/ws/run-1</workspace>\n<current_time>2026-06-09T12:34:56+02:00</current_time>\n<today>2026-06-09</today>"; last.Content != want {
		t.Fatalf("budget+workspace+time block = %q, want %q", last.Content, want)
	}

	timeOnly := b.Build(hist, reg, "openrouter", cfg, Budget{
		CurrentTime: "2026-06-09T12:34:56+02:00",
		Today:       "2026-06-09",
	})
	last = timeOnly.Messages[len(timeOnly.Messages)-1]
	if want := "<current_time>2026-06-09T12:34:56+02:00</current_time>\n<today>2026-06-09</today>"; last.Content != want {
		t.Fatalf("time-only block = %q, want %q", last.Content, want)
	}
}

// TestBudgetBlockByteStable proves the cache-prefix integrity guard (T-07.1-02):
// the trailing <budget> hint changes every turn, yet messages[0] marshals
// byte-identically and the caller's history is never mutated (D-04/D-05).
func TestBudgetBlockByteStable(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()

	t.Run("messages[0] byte-identical while the budget block changes turn-to-turn", func(t *testing.T) {
		t.Parallel()
		hist := seedHistory()

		turn1 := b.Build(hist, reg, "openrouter", cfg, Budget{Used: 0, Remaining: 3})
		turn2 := b.Build(hist, reg, "openrouter", cfg, Budget{Used: 1, Remaining: 2})

		m0a, err := json.Marshal(turn1.Messages[0])
		if err != nil {
			t.Fatalf("marshal turn1 messages[0]: %v", err)
		}
		m0b, err := json.Marshal(turn2.Messages[0])
		if err != nil {
			t.Fatalf("marshal turn2 messages[0]: %v", err)
		}
		if string(m0a) != string(m0b) {
			t.Fatalf("messages[0] drifted across turns:\n turn1=%s\n turn2=%s", m0a, m0b)
		}

		last1 := turn1.Messages[len(turn1.Messages)-1]
		last2 := turn2.Messages[len(turn2.Messages)-1]
		if last1.Role != llm.RoleUser {
			t.Fatalf("turn1 trailing message role = %q, want %q", last1.Role, llm.RoleUser)
		}
		if last2.Role != llm.RoleUser {
			t.Fatalf("turn2 trailing message role = %q, want %q", last2.Role, llm.RoleUser)
		}
		if want := "<budget>used=0 remaining=3</budget>"; last1.Content != want {
			t.Fatalf("turn1 budget block = %q, want %q", last1.Content, want)
		}
		if want := "<budget>used=1 remaining=2</budget>"; last2.Content != want {
			t.Fatalf("turn2 budget block = %q, want %q", last2.Content, want)
		}
		if last1.Content == last2.Content {
			t.Fatalf("budget block did not track the counts: both turns = %q", last1.Content)
		}
	})

	t.Run("Build never mutates the caller's history slice", func(t *testing.T) {
		t.Parallel()
		hist := seedHistory()
		lenBefore := len(hist)
		tail := hist[len(hist)-1]

		req := b.Build(hist, reg, "openrouter", cfg, Budget{Used: 2, Remaining: 1})

		if len(hist) != lenBefore {
			t.Fatalf("Build grew the caller's slice: got len %d want %d", len(hist), lenBefore)
		}
		if !reflect.DeepEqual(hist[len(hist)-1], tail) {
			t.Fatalf("Build mutated the caller's slice tail: got %+v want %+v", hist[len(hist)-1], tail)
		}
		if len(req.Messages) != lenBefore+1 {
			t.Fatalf("assembled request length = %d, want %d (history + budget block)", len(req.Messages), lenBefore+1)
		}
	})

	t.Run("zero counts omit the budget block (backward-compatible default)", func(t *testing.T) {
		t.Parallel()
		hist := seedHistory()
		req := b.Build(hist, reg, "openrouter", cfg, Budget{})

		if len(req.Messages) != len(hist) {
			t.Fatalf("zero-counts build changed message count: got %d want %d", len(req.Messages), len(hist))
		}
		for i, m := range req.Messages {
			if m.Content == "<budget>used=0 remaining=0</budget>" {
				t.Fatalf("zero-counts build emitted a trailing budget block at index %d", i)
			}
		}
	})
}

// TestBudgetBlockFormatting locks the exact D-06 wire string across a table of
// count pairs so a format drift (spacing, ordering, tag) fails loudly.
func TestBudgetBlockFormatting(t *testing.T) {
	t.Parallel()
	b := NewPromptBuilder()
	reg := testRegistry()
	cfg := testConfig()

	cases := []struct {
		name      string
		used      int
		remaining int
		want      string
	}{
		{"first step", 0, 3, "<budget>used=0 remaining=3</budget>"},
		{"midway", 1, 2, "<budget>used=1 remaining=2</budget>"},
		{"last step", 2, 1, "<budget>used=2 remaining=1</budget>"},
		{"exhausted", 3, 0, "<budget>used=3 remaining=0</budget>"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			hist := seedHistory()
			req := b.Build(hist, reg, "openrouter", cfg, Budget{Used: tc.used, Remaining: tc.remaining})
			last := req.Messages[len(req.Messages)-1]
			if last.Content != tc.want {
				t.Fatalf("budget block = %q, want %q", last.Content, tc.want)
			}
		})
	}
}
