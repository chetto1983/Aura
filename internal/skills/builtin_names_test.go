package skills

import (
	"slices"
	"testing"
)

// builtin_names_test.go pins the question "is this skill Aura's own?", asked by every surface
// that must not offer a lifecycle verb on one.
//
// A builtin is not house policy an operator chose to publish: it is product code, embedded in
// the binary, and MaterializeBuiltins rewrites it at the next boot whenever the on-disk bytes
// differ from the embedded ones. So archiving or deleting one is not a decision the cockpit can
// carry out — it is a change that undoes itself, silently, one restart later. Measured
// 2026-09-07: with the house verbs restored to an operator, the cockpit offered Archive and
// Delete on all three builtins, and both would have been no-ops with a convincing animation.

// TestBuiltinNamesAreTheEmbeddedSet is the source of truth: the answer comes from the embedded
// tree, so a builtin added or removed in internal/skills/embed changes this without anybody
// remembering to edit a list.
func TestBuiltinNamesAreTheEmbeddedSet(t *testing.T) {
	t.Parallel()
	got := BuiltinNames()

	for _, want := range []string{"find-skills-aura", "memory-aura", "skill-creator"} {
		if !slices.Contains(got, want) {
			t.Fatalf("BuiltinNames() = %v, want it to carry %q", got, want)
		}
	}
	if !slices.IsSorted(got) {
		t.Fatalf("BuiltinNames() = %v, want a stable sorted answer", got)
	}
}

// TestIsBuiltinAnswersForTheVerbs is what the cockpit and the write path actually call.
func TestIsBuiltinAnswersForTheVerbs(t *testing.T) {
	t.Parallel()
	for _, name := range BuiltinNames() {
		if !IsBuiltin(name) {
			t.Fatalf("IsBuiltin(%q) = false for a name BuiltinNames returned", name)
		}
	}
	for _, name := range []string{"brainstorm", "", "Find-Skills-Aura", "find-skills-aura/x"} {
		if IsBuiltin(name) {
			t.Fatalf("IsBuiltin(%q) = true, want false — only an exact embedded name is Aura's own", name)
		}
	}
}
