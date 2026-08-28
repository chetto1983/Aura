package agenteval

import (
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
)

func TestEvidenceFromMessagesCountsDurableAssistantTurns(t *testing.T) {
	t.Parallel()
	search := llm.ToolCall{ID: "call-1", Type: "function"}
	search.Function.Name = "document_search"
	evidence := EvidenceFromMessages([]llm.Message{
		{Role: llm.RoleUser, Content: "domanda"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{search}},
		{Role: llm.RoleTool, Content: `{"documents":[]}`},
		{Role: llm.RoleAssistant, Content: "risposta esatta"},
	})
	if evidence.LLMCalls != 2 || strings.Join(evidence.Tools, ",") != "document_search" ||
		evidence.Answer != "risposta esatta" {
		t.Fatalf("durable evidence = %+v", evidence)
	}
}

// TestCheckReportsEveryViolationAtOnce: a turn that answers wrong AND burns the
// budget must say both. Reporting one at a time turns a single bad run into two
// debugging cycles, and the second one only starts after someone re-runs a live
// turn that costs real money.
func TestCheckReportsEveryViolationAtOnce(t *testing.T) {
	t.Parallel()
	c := Case{
		AnswerContains: []string{"632587"},
		RequiredTools:  []string{"document_search"},
		ForbiddenTools: []string{"web_search"},
		MaxLLMCalls:    5,
	}
	got := c.Check(Evidence{
		Answer:   "non l'ho trovato",
		LLMCalls: 12,
		Tools:    []string{"web_search", "web_search", "shell_exec"},
	})
	if len(got) != 4 {
		t.Fatalf("expected 4 violations (answer, required, forbidden, budget), got %d:\n%s",
			len(got), strings.Join(got, "\n"))
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"632587", "document_search", "web_search", "12 LLM calls"} {
		if !strings.Contains(joined, want) {
			t.Errorf("violations do not mention %q:\n%s", want, joined)
		}
	}
	// The forbidden-tool line carries the COUNT: twice is a loop, once is a slip,
	// and the difference decides whether it is a routing bug or a prompt bug.
	if !strings.Contains(joined, "(2 times)") {
		t.Errorf("forbidden-tool violation lost the call count:\n%s", joined)
	}
}

func TestCheckPassesACleanTurn(t *testing.T) {
	t.Parallel()
	c := Case{
		AnswerContains: []string{"632587"},
		RequiredTools:  []string{"document_search", "document_open"},
		ForbiddenTools: []string{"web_search"},
		MaxLLMCalls:    9,
	}
	// Only the SECOND required tool was called: RequiredTools is any-of on
	// purpose, so the gate states the capability without pinning the route and
	// failing on an improvement.
	if got := c.Check(Evidence{
		Answer:   "Il codice cliente di WPT SRL è 632587.",
		LLMCalls: 6,
		Tools:    []string{"tool_search", "document_open", "shell_exec"},
	}); len(got) != 0 {
		t.Fatalf("clean turn reported violations:\n%s", strings.Join(got, "\n"))
	}
}

func TestCheckRequiresDeclaredToolSequence(t *testing.T) {
	t.Parallel()
	c := Case{RequiredToolSequence: []string{"document_search", "document_open", "shell_exec"}}
	for name, evidence := range map[string]Evidence{
		"missing open":   {Tools: []string{"document_search", "shell_exec"}},
		"wrong order":    {Tools: []string{"document_open", "document_search", "shell_exec"}},
		"only search":    {Tools: []string{"document_search"}},
		"empty evidence": {},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			got := c.Check(evidence)
			if len(got) != 1 || !strings.Contains(got[0], "document_search document_open shell_exec") {
				t.Fatalf("incomplete sequence was not reported: %v", got)
			}
		})
	}
	clean := Evidence{Tools: []string{
		"tool_search", "document_search", "read_tool_output", "document_open", "shell_exec",
	}}
	if got := c.Check(clean); len(got) != 0 {
		t.Fatalf("ordered sequence with intervening tools reported violations: %v", got)
	}
}

// TestCheckToolNamesMatchExactly: a namespaced MCP tool must never satisfy — or
// trip — a rule written for a built-in whose name it merely contains.
func TestCheckToolNamesMatchExactly(t *testing.T) {
	t.Parallel()
	called := Evidence{Answer: "x", LLMCalls: 1, Tools: []string{"memory__memory_search"}}
	if got := (Case{ForbiddenTools: []string{"memory_search"}}).Check(called); len(got) != 0 {
		t.Errorf("substring match tripped a forbidden rule: %v", got)
	}
	if got := (Case{RequiredTools: []string{"memory_search"}}).Check(called); len(got) != 1 {
		t.Errorf("substring match satisfied a required rule: %v", got)
	}
}

// TestCheckMemoryPrecision is the assertion nothing else in the repository makes.
// Recall@K, MRR and nDCG all score a memory that returns everything at 100%,
// because they measure ordering and assume the answer is in the corpus. Only a
// query that NOTHING should answer can tell whether the retriever filters.
func TestCheckMemoryPrecision(t *testing.T) {
	t.Parallel()
	c := Case{MemoryMustBeEmpty: true}
	if got := c.Check(Evidence{Answer: "non lo so", LLMCalls: 2, MemoryFactCounts: []int{0, 0}}); len(got) != 0 {
		t.Errorf("two empty memory searches reported a violation: %v", got)
	}
	got := c.Check(Evidence{Answer: "non lo so", LLMCalls: 2, MemoryFactCounts: []int{0, 7}})
	if len(got) != 1 || !strings.Contains(got[0], "7 facts") {
		t.Fatalf("a search that returned 7 facts for an unanswerable query was not caught: %v", got)
	}
	// Without the flag the same evidence passes: precision is asserted only where
	// a case declares the question unanswerable.
	if got := (Case{}).Check(Evidence{Answer: "x", LLMCalls: 1, MemoryFactCounts: []int{7}}); len(got) != 0 {
		t.Errorf("memory facts flagged on a case that never asked: %v", got)
	}
}

// TestCheckAnswerMustNotContain is the deletion assertion. Nothing else in the
// gate can make it: every other check asks whether something is present, and a
// delete is proved by an absence.
func TestCheckAnswerMustNotContain(t *testing.T) {
	t.Parallel()
	c := Case{AnswerMustNotContain: []string{"699"}, MaxLLMCalls: 8}
	clean := Evidence{Answer: "Non ho documenti caricati, quindi non posso contarli.", LLMCalls: 2}
	if got := c.Check(clean); len(got) != 0 {
		t.Fatalf("an answer that dropped the deleted number reported violations: %v", got)
	}
	leaked := Evidence{Answer: "Ci sono 699 clienti a TORINO.", LLMCalls: 5, Tools: []string{"fs_glob", "shell_exec"}}
	got := c.Check(leaked)
	if len(got) != 1 || !strings.Contains(got[0], "699") {
		t.Fatalf("a deleted document was answered from and the gate did not catch it: %v", got)
	}
	// The tools are in the message: knowing she reached it via fs_glob rather than
	// document_search is the difference between a catalog bug and a residue on disk.
	if !strings.Contains(got[0], "fs_glob") {
		t.Errorf("the violation does not say HOW she reached it: %s", got[0])
	}
}

// TestCasesAreWellFormed keeps the corpus honest: a case with no assertion is a
// live turn that costs money and can never fail.
func TestCasesAreWellFormed(t *testing.T) {
	t.Parallel()
	seen := map[string]bool{}
	for _, c := range Cases() {
		if c.Name == "" || c.Question == "" {
			t.Errorf("case %+v has no name or no question", c)
		}
		if seen[c.Name] {
			t.Errorf("duplicate case name %q", c.Name)
		}
		seen[c.Name] = true
		if len(c.AnswerContains) == 0 && len(c.AnswerMustNotContain) == 0 &&
			!c.MemoryMustBeEmpty && len(c.ForbiddenTools) == 0 {
			t.Errorf("case %q asserts nothing that can fail", c.Name)
		}
		if c.MaxLLMCalls <= 0 {
			t.Errorf("case %q has no call budget, so a looping turn passes", c.Name)
		}
	}
	if len(seen) == 0 {
		t.Fatal("the gate has no cases")
	}
}
