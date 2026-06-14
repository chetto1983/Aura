package profile

import (
	"strings"
	"testing"
)

// RenderContextBlock collapses whitespace-only input to an empty block so the
// protected messages[1] slot is skipped entirely when there is no profile.
func TestRenderContextBlockEmptyInputReturnsEmpty(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "   ", "\n\t\n", "\r\n  \r\n"} {
		if got := RenderContextBlock(in); got != "" {
			t.Fatalf("RenderContextBlock(%q) = %q, want empty", in, got)
		}
	}
}

// A non-empty profile under the cap is wrapped verbatim (no truncation marker).
func TestRenderContextBlockUnderCapNotTruncated(t *testing.T) {
	t.Parallel()
	block := RenderContextBlock("# Agent.md\n\n## Identity\n- Name: Davide\n")
	if strings.Contains(block, "profile truncated") {
		t.Fatalf("under-cap profile must not be truncated:\n%s", block)
	}
	if !strings.HasPrefix(block, ProfileBlockStart) || !strings.HasSuffix(block, ProfileBlockEnd) {
		t.Fatalf("wrapper markers missing:\n%s", block)
	}
}

// AddFact rejects facts that are empty after trimming the leading dash/space.
func TestAddFactRejectsEmptyFact(t *testing.T) {
	t.Parallel()
	for _, in := range []string{"", "   ", "-", "  -  ", "\t- \t"} {
		out, added, err := AddFact("# Agent.md\n", in)
		if err == nil {
			t.Fatalf("AddFact(%q) err = nil, want non-nil", in)
		}
		if added {
			t.Fatalf("AddFact(%q) added = true, want false", in)
		}
		if out != "" {
			t.Fatalf("AddFact(%q) out = %q, want empty", in, out)
		}
	}
}

// AddFact on completely empty Agent.md renders a fresh document around the fact.
func TestAddFactOnEmptyAgentMDRendersFresh(t *testing.T) {
	t.Parallel()
	out, added, err := AddFact("", "Name: Davide")
	if err != nil {
		t.Fatalf("AddFact: %v", err)
	}
	if !added {
		t.Fatal("AddFact added = false, want true")
	}
	if !strings.HasPrefix(out, "# Agent.md\n") {
		t.Fatalf("fresh render missing title header:\n%s", out)
	}
	if !strings.Contains(out, "## Identity\n- Name: Davide\n") {
		t.Fatalf("fresh render missing Identity bullet:\n%s", out)
	}
}

// When Agent.md has no Identity section and no "# Agent.md" header line, the new
// Identity section is inserted at the very top (insertAt = 0 branch).
func TestAddFactNoHeaderNoFactsInsertsAtTop(t *testing.T) {
	t.Parallel()
	md := "## Notes\nkeep this\n"
	out, added, err := AddFact(md, "Name: Davide")
	if err != nil {
		t.Fatalf("AddFact: %v", err)
	}
	if !added {
		t.Fatal("AddFact added = false, want true")
	}
	// insertAt == 0 prepends a blank separator line before the new section.
	if !strings.HasPrefix(out, "\n## Identity\n- Name: Davide") {
		t.Fatalf("Identity section should be inserted at top:\n%q", out)
	}
	if !strings.Contains(out, "## Notes\nkeep this") {
		t.Fatalf("existing Notes section must be preserved:\n%s", out)
	}
}

// With a "# Agent.md" header present but no Identity section, the new section lands
// right after the header (insertAt = 1 branch) with a blank separator line.
func TestAddFactHeaderNoFactsInsertsAfterHeader(t *testing.T) {
	t.Parallel()
	md := "# Agent.md\n\n## Notes\nkeep this\n"
	out, added, err := AddFact(md, "Name: Davide")
	if err != nil {
		t.Fatalf("AddFact: %v", err)
	}
	if !added {
		t.Fatal("AddFact added = false, want true")
	}
	if !strings.HasPrefix(out, "# Agent.md\n\n## Identity\n- Name: Davide") {
		t.Fatalf("Identity must follow the header line:\n%s", out)
	}
	if !strings.Contains(out, "## Notes\nkeep this") {
		t.Fatalf("Notes section must survive:\n%s", out)
	}
}

// writeSection skips bullet entries that are empty after dash/space trimming,
// so an Identity list with blank entries renders only the meaningful ones.
func TestRenderAgentMDSkipsEmptyItems(t *testing.T) {
	t.Parallel()
	md := RenderAgentMD(AgentContent{
		Identity: []string{"Name: Davide", "", "  ", "- ", "Role: builder"},
	})
	if strings.Count(md, "- ") != 2 {
		t.Fatalf("only two non-empty identity items should render:\n%s", md)
	}
	if !strings.Contains(md, "- Name: Davide\n") || !strings.Contains(md, "- Role: builder\n") {
		t.Fatalf("expected identity items missing:\n%s", md)
	}
}

// AddFact appends to an existing Identity section that is the final section in the
// document (nextSectionIndex returns len(lines)).
func TestAddFactAppendsToTrailingFactsSection(t *testing.T) {
	t.Parallel()
	md := "# Agent.md\n\n## Identity\n- Name: Davide\n"
	out, added, err := AddFact(md, "Role: builder")
	if err != nil {
		t.Fatalf("AddFact: %v", err)
	}
	if !added {
		t.Fatal("AddFact added = false, want true")
	}
	if !strings.Contains(out, "- Name: Davide\n- Role: builder") {
		t.Fatalf("new fact should append to the trailing Identity section:\n%s", out)
	}
}

// ParseSections collects non-bullet, non-heading lines into Section.Body, and
// FormatSectionTree renders those body lines as nested tree entries.
func TestParseSectionsCapturesBodyAndTreeRendersIt(t *testing.T) {
	t.Parallel()
	md := "# Agent.md\n\n## Context\nFreeform body line one.\n- a bullet item\nFreeform body line two.\n"
	sections := ParseSections(md)
	if len(sections) != 1 {
		t.Fatalf("ParseSections count = %d, want 1: %#v", len(sections), sections)
	}
	ctx := sections[0]
	if ctx.Title != "Context" {
		t.Fatalf("section title = %q, want Context", ctx.Title)
	}
	if len(ctx.Items) != 1 || ctx.Items[0] != "a bullet item" {
		t.Fatalf("bullet items not parsed: %#v", ctx.Items)
	}
	wantBody := []string{"Freeform body line one.", "Freeform body line two."}
	if len(ctx.Body) != len(wantBody) {
		t.Fatalf("body lines = %#v, want %#v", ctx.Body, wantBody)
	}
	for i, line := range wantBody {
		if ctx.Body[i] != line {
			t.Fatalf("body[%d] = %q, want %q", i, ctx.Body[i], line)
		}
	}
	tree := FormatSectionTree(md)
	for _, want := range []string{"- Context", "  - a bullet item", "  - Freeform body line one.", "  - Freeform body line two."} {
		if !strings.Contains(tree, want) {
			t.Fatalf("tree missing %q:\n%s", want, tree)
		}
	}
}

// ParseSections ignores leading content that appears before any H2 heading
// (current == -1 guard) and treats blank lines as separators.
func TestParseSectionsIgnoresPreambleAndBlanks(t *testing.T) {
	t.Parallel()
	md := "# Agent.md\nstray preamble before any section\n\n## Identity\n- Name: Davide\n"
	sections := ParseSections(md)
	if len(sections) != 1 {
		t.Fatalf("ParseSections count = %d, want 1: %#v", len(sections), sections)
	}
	if len(sections[0].Body) != 0 {
		t.Fatalf("preamble must not leak into a section body: %#v", sections[0].Body)
	}
	if len(sections[0].Items) != 1 || sections[0].Items[0] != "Name: Davide" {
		t.Fatalf("Identity not parsed: %#v", sections[0].Items)
	}
}

// boundedAgentMD truncates oversized profiles on a rune boundary and appends the
// truncation suffix, keeping the result within the byte cap.
func TestBoundedAgentMDTruncatesOnRuneBoundary(t *testing.T) {
	t.Parallel()
	// Multi-byte runes ensure the truncation loop respects rune boundaries.
	oversized := strings.Repeat("è", MaxAgentMDBytes)
	got := boundedAgentMD(oversized)
	if !strings.HasSuffix(got, "[profile truncated: Agent.md exceeded byte cap]") {
		t.Fatalf("truncated output missing suffix:\n%s", got[len(got)-80:])
	}
	if len([]byte(got)) > MaxAgentMDBytes {
		t.Fatalf("truncated output exceeds cap: %d > %d", len([]byte(got)), MaxAgentMDBytes)
	}
	// A profile exactly at the cap is returned unchanged.
	atCap := strings.Repeat("x", MaxAgentMDBytes)
	if boundedAgentMD(atCap) != atCap {
		t.Fatal("profile exactly at the cap must be returned verbatim")
	}
}
