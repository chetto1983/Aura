package prompt

import (
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// TestPrefixHashSkipsInvalidIndices closes the out-of-range / negative index
// skip arm of PrefixHash. The forward-compat contract is that indices at or
// beyond len(msgs) — and defensively, negative ones — are skipped rather than
// panicking, so {0,1,2} over a one-message history hashes the same as {0}, and a
// negative index contributes nothing.
func TestPrefixHashSkipsInvalidIndices(t *testing.T) {
	t.Parallel()
	msgs := []llm.Message{{Role: llm.RoleSystem, Content: "system prefix"}}

	want, err := PrefixHash(msgs, []int{0})
	if err != nil {
		t.Fatalf("PrefixHash {0}: %v", err)
	}

	cases := []struct {
		name    string
		indices []int
	}{
		{"trailing out-of-range indices skipped", []int{0, 1, 2, 99}},
		{"leading negative index skipped", []int{-1, 0}},
		{"interleaved negative and out-of-range skipped", []int{-3, 0, 7}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := PrefixHash(msgs, tc.indices)
			if err != nil {
				t.Fatalf("PrefixHash(%v): %v", tc.indices, err)
			}
			if got != want {
				t.Fatalf("PrefixHash(%v) = %q, want %q (invalid indices must be skipped, not folded in)", tc.indices, got, want)
			}
		})
	}
}

// TestPrefixHashAllInvalidIndicesIsEmptyDigest proves that a set with only
// skip-eligible indices produces the same stable empty-input digest as the
// no-index call — the hash of zero contributing messages.
func TestPrefixHashAllInvalidIndicesIsEmptyDigest(t *testing.T) {
	t.Parallel()
	msgs := []llm.Message{{Role: llm.RoleSystem, Content: "system prefix"}}

	empty, err := PrefixHash(msgs, nil)
	if err != nil {
		t.Fatalf("PrefixHash nil: %v", err)
	}
	allInvalid, err := PrefixHash(msgs, []int{-1, 5, 6})
	if err != nil {
		t.Fatalf("PrefixHash all-invalid: %v", err)
	}
	if allInvalid != empty {
		t.Fatalf("all-invalid index set = %q, want empty-digest %q", allInvalid, empty)
	}
}
