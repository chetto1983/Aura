package profile

import (
	"strings"
	"testing"
)

func TestAddFactIdempotentAndPreservesUnknownSections(t *testing.T) {
	t.Parallel()
	agentMD := "# Agent.md\n\n## Style\n- Prefer Italian responses.\n\n## Notes\nkeep this\n"
	got, added, err := AddFact(agentMD, "I prefer concise answers")
	if err != nil {
		t.Fatalf("AddFact: %v", err)
	}
	if !added {
		t.Fatal("AddFact first call added=false, want true")
	}
	if !strings.Contains(got, "## Identity\n- I prefer concise answers") {
		t.Fatalf("identity section missing inserted fact:\n%s", got)
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
		Identity:           []string{"Name: Davide"},
		Style:              []string{"Prefer Italian responses."},
		CustomInstructions: []string{"Keep it direct."},
	})
	sections := ParseSections(md)
	// RenderAgentMD emits all 8 sections (even empty ones get a heading).
	if len(sections) != 8 {
		t.Fatalf("ParseSections count = %d, want 8: %#v", len(sections), sections)
	}
	if sections[0].Title != "Identity" || sections[0].Items[0] != "Name: Davide" {
		t.Fatalf("Identity section not parsed: %#v", sections[0])
	}
	tree := FormatSectionTree(md)
	for _, want := range []string{"Agent.md", "- Style", "  - Prefer Italian responses.", "- Custom Instructions"} {
		if !strings.Contains(tree, want) {
			t.Fatalf("tree missing %q:\n%s", want, tree)
		}
	}
}

func TestRenderAgentMDStableAndBounded(t *testing.T) {
	t.Parallel()
	c := AgentContent{Identity: []string{"Name: Davide"}}
	a := RenderAgentMD(c)
	b := RenderAgentMD(c)
	if a != b {
		t.Fatal("RenderAgentMD is not byte-stable for identical input")
	}
	if err := checkAgentSize(strings.Repeat("x", MaxAgentMDBytes+1)); err == nil {
		t.Fatal("checkAgentSize should reject oversized Agent.md")
	}
}

func TestRenderAgentMD_EightSections(t *testing.T) {
	got := RenderAgentMD(AgentContent{
		Identity:           []string{"Preferred name: Davide", "Role: dev @ Aura"},
		ExpertiseTools:     []string{"Stack: Go, Neo4j"},
		ProjectsGoals:      []string{"Aura personal assistant"},
		Interests:          []string{"AI agents"},
		People:             []string{"Andrea — business partner"},
		Style:              []string{"Reply language: Italian"},
		Vetoes:             []string{"No em-dashes"},
		CustomInstructions: []string{"Be concise"},
	})
	for _, want := range []string{
		"## Identity\n- Preferred name: Davide",
		"## Expertise & Tools\n- Stack: Go, Neo4j",
		"## Projects & Goals\n- Aura personal assistant",
		"## Interests\n- AI agents",
		"## People\n- Andrea — business partner",
		"## Style\n- Reply language: Italian",
		"## Vetoes\n- No em-dashes",
		"## Custom Instructions\n- Be concise",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderAgentMD missing %q in:\n%s", want, got)
		}
	}
	if i, j := strings.Index(got, "## Identity"), strings.Index(got, "## Expertise & Tools"); i == -1 || j == -1 || i > j {
		t.Errorf("section order wrong: Identity at %d, Expertise at %d", i, j)
	}
}

func TestRenderContextBlockWrapsAndBoundsAgentMD(t *testing.T) {
	t.Parallel()
	block := RenderContextBlock("# Agent.md\n\n## Facts\n- Name: Davide\n")
	if !strings.HasPrefix(block, ProfileBlockStart+"\n# Agent.md") {
		t.Fatalf("block missing start marker:\n%s", block)
	}
	if !strings.HasSuffix(block, "\n"+ProfileBlockEnd) {
		t.Fatalf("block missing end marker:\n%s", block)
	}
	oversized := RenderContextBlock(strings.Repeat("x", MaxAgentMDBytes+100))
	if !strings.Contains(oversized, "profile truncated") {
		t.Fatalf("oversized profile should be truncated with warning:\n%s", oversized[len(oversized)-128:])
	}
	if len([]byte(oversized)) > MaxAgentMDBytes+len(ProfileBlockStart)+len(ProfileBlockEnd)+2 {
		t.Fatalf("bounded block too large: %d", len([]byte(oversized)))
	}
}
