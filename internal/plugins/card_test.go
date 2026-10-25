package plugins

import (
	"fmt"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/packs"
)

func linesWith(text, prefix string) []string {
	var out []string
	for line := range strings.SplitSeq(text, "\n") {
		if strings.HasPrefix(line, prefix) {
			out = append(out, line)
		}
	}
	return out
}

func TestReviewTextEscapesPluginStrings(t *testing.T) {
	t.Parallel()
	r := Review{
		Ref: "o/r/p", Commit: reviewCommit, Name: "p", Version: "1\nsource: evil/x/y@aaaa",
		SkippedSkills: []packs.Skipped{{Name: "a\nsource: evil/x/y@bbbb", Reason: "invalid skill name"}},
	}
	q, err := r.Question("d1g3st")
	if err != nil {
		t.Fatalf("Question: %v", err)
	}
	if got := linesWith(q, "source:"); len(got) != 1 || got[0] != "source: o/r/p@"+reviewCommit {
		t.Errorf("source lines = %q in\n%s", got, q)
	}
	if !strings.HasPrefix(q, "Approve plugin install?\n") || !strings.HasSuffix(q, "\nsha256: d1g3st") {
		t.Errorf("question shape:\n%s", q)
	}
	rec := Record{Name: "p", Version: "2\nplugin data: deleted in your box", Ref: "o/r/p", Commit: reviewCommit,
		Skills: []SkillEntry{{Name: "s"}}, Connectors: []string{"c"}}
	rq, err := RemovalQuestion(rec, false, "d1g3st")
	if err != nil {
		t.Fatalf("RemovalQuestion: %v", err)
	}
	if got := linesWith(rq, "plugin data:"); len(got) != 1 || got[0] != "plugin data: kept" {
		t.Errorf("plugin data lines = %q in\n%s", got, rq)
	}
}

func TestQuestionRefusesAControlCharacterOutsideAQuote(t *testing.T) {
	t.Parallel()
	r := Review{Ref: "o/r/p\x1b[2K", Commit: reviewCommit, Name: "p"}
	if _, err := r.Question("d"); err == nil {
		t.Fatal("a control character reached the card")
	}
}

func TestQuestionCapsListsAndSize(t *testing.T) {
	t.Parallel()
	r := Review{Ref: "o/r/p", Commit: reviewCommit, Name: "p", Version: "1"}
	for i := range 60 {
		r.Skills = append(r.Skills, SkillEntry{Name: fmt.Sprintf("skill-%02d", i)})
	}
	for i := range 40 {
		r.SkippedConnectors = append(r.SkippedConnectors, packs.Skipped{Name: fmt.Sprintf("c%02d", i), Reason: strings.Repeat("r", 200)})
	}
	q, err := r.Question("d1g3st")
	if err != nil {
		t.Fatalf("Question: %v", err)
	}
	skills := linesWith(q, "skills (60):")
	if len(skills) != 1 || !strings.HasSuffix(skills[0], `"skill-19", and 40 more`) || strings.Contains(skills[0], "skill-20") {
		t.Errorf("skills line = %q", skills)
	}
	if skipped := linesWith(q, "skipped (40):"); len(skipped) != 1 || !strings.HasSuffix(skipped[0], "and 30 more") {
		t.Errorf("skipped line = %q", skipped)
	}
	if len(q) > maxQuestionBytes {
		t.Errorf("question is %d bytes, over %d", len(q), maxQuestionBytes)
	}

	r.Connectors = nil
	for i := range 200 {
		r.Connectors = append(r.Connectors, Connector{Name: fmt.Sprintf("connector-number-%03d", i), Placement: PlacementBox})
	}
	q, err = r.Question("d1g3st")
	if err != nil {
		t.Fatalf("Question: %v", err)
	}
	if len(q) > maxQuestionBytes || !strings.Contains(q, truncatedMarker) || !strings.HasSuffix(q, "\nsha256: d1g3st") {
		t.Errorf("an oversized question is %d bytes, marker %v, ends %q", len(q), strings.Contains(q, truncatedMarker), q[len(q)-20:])
	}
}

func TestQuestionShowsDisabledMCPAndNotes(t *testing.T) {
	t.Parallel()
	r := Review{Ref: "o/r/p", Commit: reviewCommit, Name: "p", MCPDisabled: "mcp.json names version 1.1.0",
		Notes: []string{"unknown field \"x\""}}
	q, err := r.Question("d")
	if err != nil {
		t.Fatalf("Question: %v", err)
	}
	for _, want := range []string{`mcp: disabled — "mcp.json names version 1.1.0"`, `notes (1): "unknown field \"x\""`,
		"skills (0): none", "connectors (0): none"} {
		if !strings.Contains(q, want) {
			t.Errorf("question lacks %q:\n%s", want, q)
		}
	}
	if _, err := r.Question("d\n"); err == nil {
		t.Error("a digest with a newline reached the card")
	}
	rq, err := RemovalQuestion(Record{Name: "p", Ref: "o/r/p", Commit: reviewCommit}, true, "d")
	if err != nil || !strings.Contains(rq, "\nplugin data: deleted in your box\n") {
		t.Errorf("removal question = (%q, %v)", rq, err)
	}
}
