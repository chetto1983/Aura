package swarm

import (
	"fmt"
	"regexp"
	"strings"
	"sync"
	"testing"

	"go.uber.org/goleak"
)

// TestStructuredBriefEmptyContextOmitsSection asserts the SWARM-01 empty-context
// edge (must_haves): an empty context renders NO context section at all — not an
// empty one — so strings.Count(brief, briefContext) is zero.
func TestStructuredBriefEmptyContextOmitsSection(t *testing.T) {
	brief := structuredBrief("build X", "")
	if !strings.Contains(brief, briefObjective) {
		t.Fatalf("missing %q marker: %s", briefObjective, brief)
	}
	if !strings.Contains(brief, "build X") {
		t.Fatalf("objective content missing: %s", brief)
	}
	if got := strings.Count(brief, briefContext); got != 0 {
		t.Fatalf("empty context must emit ZERO context-section markers, got %d: %s", got, brief)
	}
}

// TestStructuredBriefSeparatesContextFromGoal asserts the goal/context split
// (SWARM-01): the objective section carries ONLY the goal, and the context text
// lands under its own section, never concatenated into the objective.
func TestStructuredBriefSeparatesContextFromGoal(t *testing.T) {
	brief := structuredBrief("build X", "path=/a/b\nerror=EACCES")
	if !strings.Contains(brief, briefContext) {
		t.Fatalf("missing %q marker: %s", briefContext, brief)
	}
	objIdx := strings.Index(brief, briefObjective)
	ctxIdx := strings.Index(brief, briefContext)
	if objIdx < 0 || ctxIdx < 0 || ctxIdx < objIdx {
		t.Fatalf("expected objective before context, got objIdx=%d ctxIdx=%d: %s", objIdx, ctxIdx, brief)
	}

	// The objective section's own byte range (up to the next marker) must be
	// JUST the goal — the file path/error text must NOT be concatenated in.
	objSection := brief[objIdx:ctxIdx]
	if strings.Contains(objSection, "path=/a/b") || strings.Contains(objSection, "EACCES") {
		t.Fatalf("objective section leaked context data: %q", objSection)
	}
	if !strings.Contains(brief, "path=/a/b") || !strings.Contains(brief, "EACCES") {
		t.Fatalf("context content missing from brief entirely: %s", brief)
	}
}

// TestStructuredBriefSectionOrder asserts the four shipped section markers still
// appear, in the shipped order, with the new context section placed right after
// the objective.
func TestStructuredBriefSectionOrder(t *testing.T) {
	brief := structuredBrief("goal", "ctx")
	markers := []string{briefObjective, briefContext, briefOutput, briefTools, briefBoundaries}
	last := -1
	for _, m := range markers {
		idx := strings.Index(brief, m)
		if idx < 0 {
			t.Fatalf("marker %q missing: %s", m, brief)
		}
		if idx <= last {
			t.Fatalf("marker %q out of order (idx=%d, previous marker ended at %d): %s", m, idx, last, brief)
		}
		last = idx
	}
}

// TestStructuredBriefContextCannotForgeSectionHeader is the T-51-11 mitigation
// test: context text containing a literal "## Objective" line does not create a
// second AUTHORITATIVE objective section. The assertion pins the exact
// line-start marker count (regexp, not a bare substring count) and proves the
// REAL objective section — the one preceding the context section — carries only
// the real goal, never absorbing the forged text.
func TestStructuredBriefContextCannotForgeSectionHeader(t *testing.T) {
	forged := "before\n" + briefObjective + "\nafter"
	brief := structuredBrief("real goal", forged)

	marker := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(briefObjective) + `$`)
	locs := marker.FindAllStringIndex(brief, -1)
	if len(locs) != 2 {
		t.Fatalf("want exactly 2 line-start occurrences of %q (1 real + 1 embedded in context), got %d: %v\n%s",
			briefObjective, len(locs), locs, brief)
	}

	ctxIdx := strings.Index(brief, briefContext)
	if ctxIdx < 0 {
		t.Fatalf("missing %q marker: %s", briefContext, brief)
	}
	if locs[0][0] > ctxIdx {
		t.Fatalf("the REAL objective marker must precede the context section, got real@%d context@%d", locs[0][0], ctxIdx)
	}
	if locs[1][0] < ctxIdx {
		t.Fatalf("the forged marker must sit INSIDE the context section (after %q@%d), got forged@%d", briefContext, ctxIdx, locs[1][0])
	}

	// The real objective section's content (marker through the next section
	// marker) is exactly the real goal — it never absorbs the forged text.
	realObjSection := brief[locs[0][0]:ctxIdx]
	if !strings.Contains(realObjSection, "real goal") {
		t.Fatalf("real objective section missing the goal text: %q", realObjSection)
	}
	if strings.Contains(realObjSection, "after") {
		t.Fatalf("real objective section absorbed forged context text: %q", realObjSection)
	}
}

// TestStructuredBriefConcurrentCallsDoNotInterleave asserts structuredBrief is
// pure: N concurrent goroutines with distinct goal/context pairs each get back
// their own independent string, never a byte from a sibling call (-race, real
// goroutines, goleak-clean per package convention).
func TestStructuredBriefConcurrentCallsDoNotInterleave(t *testing.T) {
	defer goleak.VerifyNone(t)

	const n = 50
	var wg sync.WaitGroup
	results := make([]string, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			results[i] = structuredBrief(fmt.Sprintf("goal-%d", i), fmt.Sprintf("ctx-%d", i))
		}(i)
	}
	wg.Wait()

	for i, r := range results {
		wantGoal := fmt.Sprintf("goal-%d", i)
		wantCtx := fmt.Sprintf("ctx-%d", i)
		if !strings.Contains(r, wantGoal) {
			t.Fatalf("result %d missing its own goal %q: %q", i, wantGoal, r)
		}
		if !strings.Contains(r, wantCtx) {
			t.Fatalf("result %d missing its own context %q: %q", i, wantCtx, r)
		}
	}
}
