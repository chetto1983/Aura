package prompt

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// fixtureHistory returns a deterministic three-message history (no clock/UUID).
func fixtureHistory() []llm.Message {
	return []llm.Message{
		{Role: llm.RoleSystem, Content: "system prefix"},
		{Role: llm.RoleUser, Content: "hello"},
		{Role: llm.RoleAssistant, Content: "hi there"},
	}
}

func TestPrefixHash(t *testing.T) {
	t.Parallel()

	t.Run("deterministic over repeated calls", func(t *testing.T) {
		t.Parallel()
		msgs := fixtureHistory()
		h1, err := PrefixHash(msgs, []int{0})
		if err != nil {
			t.Fatalf("PrefixHash: %v", err)
		}
		h2, err := PrefixHash(msgs, []int{0})
		if err != nil {
			t.Fatalf("PrefixHash: %v", err)
		}
		if h1 != h2 {
			t.Fatalf("non-deterministic: %q != %q", h1, h2)
		}
	})

	t.Run("unchanged when history grows", func(t *testing.T) {
		t.Parallel()
		base := []llm.Message{{Role: llm.RoleSystem, Content: "system prefix"}}
		hBase, err := PrefixHash(base, []int{0})
		if err != nil {
			t.Fatalf("PrefixHash base: %v", err)
		}
		grown := append([]llm.Message(nil), base...)
		grown = append(grown,
			llm.Message{Role: llm.RoleUser, Content: "later turn"},
			llm.Message{Role: llm.RoleAssistant, Content: "later answer"},
		)
		hGrown, err := PrefixHash(grown, []int{0})
		if err != nil {
			t.Fatalf("PrefixHash grown: %v", err)
		}
		if hBase != hGrown {
			t.Fatalf("index 0 hash changed under history growth: %q != %q", hBase, hGrown)
		}
	})

	t.Run("forward-compat index set skips absent indices", func(t *testing.T) {
		t.Parallel()
		msgs := []llm.Message{{Role: llm.RoleSystem, Content: "system prefix"}}
		hZero, err := PrefixHash(msgs, []int{0})
		if err != nil {
			t.Fatalf("PrefixHash {0}: %v", err)
		}
		hForward, err := PrefixHash(msgs, []int{0, 1, 2})
		if err != nil {
			t.Fatalf("PrefixHash {0,1,2}: %v", err)
		}
		if hZero != hForward {
			t.Fatalf("{0,1,2} over a single-message history must equal {0}: %q != %q", hForward, hZero)
		}
	})

	t.Run("canonical key order yields identical hash", func(t *testing.T) {
		t.Parallel()
		// Same logical content; canonicaljson sorts struct-marshalled keys, so two
		// equal Messages always serialize identically regardless of field-set order.
		a := []llm.Message{{Role: llm.RoleSystem, Content: "x", Name: "n"}}
		b := []llm.Message{{Name: "n", Content: "x", Role: llm.RoleSystem}}
		ha, err := PrefixHash(a, []int{0})
		if err != nil {
			t.Fatalf("PrefixHash a: %v", err)
		}
		hb, err := PrefixHash(b, []int{0})
		if err != nil {
			t.Fatalf("PrefixHash b: %v", err)
		}
		if ha != hb {
			t.Fatalf("logically-equal messages hashed differently: %q != %q", ha, hb)
		}
	})

	t.Run("content change changes the hash", func(t *testing.T) {
		t.Parallel()
		a := []llm.Message{{Role: llm.RoleSystem, Content: "original"}}
		b := []llm.Message{{Role: llm.RoleSystem, Content: "mutated"}}
		ha, err := PrefixHash(a, []int{0})
		if err != nil {
			t.Fatalf("PrefixHash a: %v", err)
		}
		hb, err := PrefixHash(b, []int{0})
		if err != nil {
			t.Fatalf("PrefixHash b: %v", err)
		}
		if ha == hb {
			t.Fatalf("content change did not change the hash (insensitive)")
		}
	})

	t.Run("empty index set is a stable empty-digest", func(t *testing.T) {
		t.Parallel()
		msgs := fixtureHistory()
		h1, err := PrefixHash(msgs, nil)
		if err != nil {
			t.Fatalf("PrefixHash nil: %v", err)
		}
		h2, err := PrefixHash(msgs, []int{})
		if err != nil {
			t.Fatalf("PrefixHash empty: %v", err)
		}
		if h1 != h2 {
			t.Fatalf("nil vs empty index set diverged: %q != %q", h1, h2)
		}
	})
}
