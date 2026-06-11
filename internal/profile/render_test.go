package profile

import (
	"strings"
	"testing"
)

func TestAddFactIdempotentAndPreservesUnknownSections(t *testing.T) {
	t.Parallel()
	agentMD := "# Agent.md\n\n## Preferences\n- Prefer Italian responses.\n\n## Notes\nkeep this\n"
	got, added, err := AddFact(agentMD, "I prefer concise answers")
	if err != nil {
		t.Fatalf("AddFact: %v", err)
	}
	if !added {
		t.Fatal("AddFact first call added=false, want true")
	}
	if !strings.Contains(got, "## Facts\n- I prefer concise answers") {
		t.Fatalf("facts section missing inserted fact:\n%s", got)
	}
	if !strings.Contains(got, "## Notes\nkeep this") {
		t.Fatalf("unknown section was not preserved:\n%s", got)
	}

	again, added, err := AddFact(got, "I prefer concise answers")
	if err != nil {
		t.Fatalf("AddFact duplicate: %v", err)
	}
	if added {
		t.Fatal("AddFact duplicate added=true, want false")
	}
	if again != got {
		t.Fatal("duplicate AddFact changed Agent.md")
	}
}

func TestStoreAddFactWritesProfileAndChangelog(t *testing.T) {
	t.Parallel()
	s := NewStore(t.TempDir())
	s.now = fixedClock()

	added, err := s.AddFact("local", "I prefer Italian responses")
	if err != nil {
		t.Fatalf("AddFact: %v", err)
	}
	if !added {
		t.Fatal("AddFact added=false, want true")
	}
	added, err = s.AddFact("local", "I prefer Italian responses")
	if err != nil {
		t.Fatalf("AddFact duplicate: %v", err)
	}
	if added {
		t.Fatal("AddFact duplicate added=true, want false")
	}
	got, err := s.ReadProfile("local")
	if err != nil {
		t.Fatalf("ReadProfile: %v", err)
	}
	if strings.Count(got.AgentMD, "I prefer Italian responses") != 1 {
		t.Fatalf("fact should appear once:\n%s", got.AgentMD)
	}
	if strings.Count(got.Changelog, "added fact: I prefer Italian responses") != 1 {
		t.Fatalf("changelog should have one add entry:\n%s", got.Changelog)
	}
}

func TestParseSectionsAndFormatTree(t *testing.T) {
	t.Parallel()
	md := RenderAgentMD(AgentContent{
		Facts:              []string{"Name: Davide"},
		Preferences:        []string{"Prefer Italian responses."},
		CustomInstructions: []string{"Keep it direct."},
	})
	sections := ParseSections(md)
	if len(sections) != 4 {
		t.Fatalf("ParseSections count = %d, want 4: %#v", len(sections), sections)
	}
	if sections[1].Title != "Preferences" || sections[1].Items[0] != "Prefer Italian responses." {
		t.Fatalf("Preferences section not parsed: %#v", sections[1])
	}
	tree := FormatSectionTree(md)
	for _, want := range []string{"Agent.md", "- Preferences", "  - Prefer Italian responses.", "- Custom Instructions"} {
		if !strings.Contains(tree, want) {
			t.Fatalf("tree missing %q:\n%s", want, tree)
		}
	}
}

func TestRenderAgentMDStableAndBounded(t *testing.T) {
	t.Parallel()
	c := AgentContent{Facts: []string{"Name: Davide"}}
	a := RenderAgentMD(c)
	b := RenderAgentMD(c)
	if a != b {
		t.Fatal("RenderAgentMD is not byte-stable for identical input")
	}
	if err := checkAgentSize(strings.Repeat("x", MaxAgentMDBytes+1)); err == nil {
		t.Fatal("checkAgentSize should reject oversized Agent.md")
	}
}
