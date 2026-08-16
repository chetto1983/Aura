package conversations

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

// Tool output is 61% of a real conversation's bytes and the least summarizable part of it,
// so it is cut an order of magnitude harder than prose. If this ever equalises again, the
// summarizer goes back to reading shell transcripts instead of decisions.
func TestRenderForSummaryPrunesToolOutputFarHarderThanProse(t *testing.T) {
	long := strings.Repeat("x", 20000)
	rendered := renderRoundsForSummary([]llm.Message{
		{Role: llm.RoleAssistant, Content: long},
		{Role: llm.RoleTool, Content: long},
	})

	lines := strings.Split(strings.TrimSpace(rendered), "\n")
	if len(lines) != 2 {
		t.Fatalf("rendered %d lines, want one per turn:\n%s", len(lines), rendered)
	}
	assistant, tool := len(lines[0]), len(lines[1])
	if assistant <= summaryToolOutputCap*2 {
		t.Fatalf("assistant line = %d bytes, want the prose cap to be far larger", assistant)
	}
	if tool > summaryToolOutputCap+64 {
		t.Fatalf("tool line = %d bytes, want it pruned to about %d", tool, summaryToolOutputCap)
	}
}

// The head, not the tail: a tool result announces its outcome first and pads afterwards.
func TestRenderForSummaryKeepsTheHeadOfAToolResult(t *testing.T) {
	rendered := renderRoundsForSummary([]llm.Message{
		{Role: llm.RoleTool, Content: "exit code 1: permission denied" + strings.Repeat(" padding", 500)},
	})
	if !strings.Contains(rendered, "permission denied") {
		t.Fatalf("the outcome was pruned away:\n%s", rendered)
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
