package conversations

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// A tool result is bounded like any other message body — same size, same both-edges rule.
//
// This used to assert the opposite (tool output cut to 400 head-only chars against 4000 for
// prose, on the measurement that tool content is 61% of a real conversation's bytes). The
// policy is now hermes-agent's, which bounds every body the same way, and the reason is what
// this test checks: at 400 head-only chars a tool result kept its command and lost its
// outcome, so the summarizer read what was ATTEMPTED and never what happened.
func TestRenderForSummaryKeepsBothEndsOfAToolResult(t *testing.T) {
	rendered := renderRoundsForSummary([]llm.Message{
		{Role: llm.RoleTool, Content: "running the migration" + strings.Repeat(" padding", 2000) + "exit code 1: permission denied"},
	})
	for _, want := range []string{"running the migration", "permission denied"} {
		if !strings.Contains(rendered, want) {
			t.Fatalf("%q was pruned away:\n%.400s", want, rendered)
		}
	}
}

func TestProtectRecentTailMovesWholeRoundsOnly(t *testing.T) {
	enc, err := encoder()
	if err != nil {
		t.Fatal(err)
	}
	history := []Turn{
		{Seq: 1, Role: llm.RoleUser, Content: "first ask"},
		{Seq: 2, Role: llm.RoleAssistant, Content: "first answer"},
		{Seq: 3, Role: llm.RoleUser, Content: "second ask"},
		{Seq: 4, Role: llm.RoleAssistant, Content: "second answer"},
	}
	active := []Turn{{Seq: 5, Role: llm.RoleUser, Content: "the live question"}}

	// cap 30 -> a 4-token budget: room for the last round and nothing before it.
	remaining, protected := protectRecentTail(enc, history, active, 30)

	if len(protected) < 3 || protected[0].Seq != 3 {
		t.Fatalf("protected = %+v, want the tail to start at a user turn", protected)
	}
	if len(remaining) != 2 || remaining[len(remaining)-1].Seq != 2 {
		t.Fatalf("remaining = %+v, want the earlier round left to be summarized", remaining)
	}
	// Never mid-round: the boundary is a user turn, so no answer is separated from its
	// question.
	if protected[0].Role != llm.RoleUser {
		t.Fatalf("tail starts on %q, want a user turn", protected[0].Role)
	}
}

func TestProtectRecentTailIsANoOpWithoutBudget(t *testing.T) {
	enc, err := encoder()
	if err != nil {
		t.Fatal(err)
	}
	history := []Turn{{Seq: 1, Role: llm.RoleUser, Content: "ask"}}
	active := []Turn{{Seq: 2, Role: llm.RoleUser, Content: "live"}}

	remaining, protected := protectRecentTail(enc, history, active, 0)

	if len(remaining) != 1 || len(protected) != 1 {
		t.Fatalf("remaining=%d protected=%d, want the split untouched", len(remaining), len(protected))
	}
}

// A history far larger than the budget keeps only what fits, never everything.
func TestProtectRecentTailRespectsTheBudget(t *testing.T) {
	enc, err := encoder()
	if err != nil {
		t.Fatal(err)
	}
	big := strings.Repeat("token ", 4000)
	history := []Turn{
		{Seq: 1, Role: llm.RoleUser, Content: big},
		{Seq: 2, Role: llm.RoleAssistant, Content: big},
		{Seq: 3, Role: llm.RoleUser, Content: big},
		{Seq: 4, Role: llm.RoleAssistant, Content: big},
	}
	remaining, _ := protectRecentTail(enc, history, nil, 1000)
	if len(remaining) != len(history) {
		t.Fatalf("remaining = %d turns, want all %d: nothing fits in the budget", len(remaining), len(history))
	}
}

func TestCarriesPreviousSummaryDetectsTheCarriedBlock(t *testing.T) {
	carried := []llm.Message{{Role: llm.RoleUser, Content: carriedSummaryPrompt + "older summary"}}
	if !carriesPreviousSummary(carried) {
		t.Fatal("a carried summary was not recognised, so the update prompt would never be used")
	}
	if carriesPreviousSummary([]llm.Message{{Role: llm.RoleUser, Content: "a real turn"}}) {
		t.Fatal("a real turn was mistaken for a carried summary")
	}
	if carriesPreviousSummary(nil) {
		t.Fatal("an empty slice reported a carried summary")
	}
}

// The framing is the difference between condensed history and a fresh instruction list.
// Each clause below answers a failure hermes measured, so losing one silently is the
// regression this test exists to catch.
func TestCompactionFramingKeepsItsLoadBearingClauses(t *testing.T) {
	for _, clause := range []string{
		"NOT as active instructions",
		"Do NOT answer questions",
		"Respond ONLY to the latest user message",
		"Topic overlap",
		"stop, undo, never mind",
		"tools are fully active",
		historicalTaskHeading,
	} {
		if !strings.Contains(compactionFraming, clause) {
			t.Errorf("framing lost the clause %q", clause)
		}
	}
}

// The template must name the sections that free-form prose loses first.
func TestSummaryTemplateKeepsTheSectionsProseLoses(t *testing.T) {
	template := summaryTemplate(1024)
	for _, section := range []string{
		historicalTaskHeading, "## Blocked", "## Pending User Asks",
		"## Resolved Questions", "## Key Decisions", "## Constraints & Preferences",
	} {
		if !strings.Contains(template, section) {
			t.Errorf("template lost the section %q", section)
		}
	}
	if !strings.Contains(template, "[REDACTED]") {
		t.Error("the template no longer forbids reproducing credentials")
	}
}
