// Package agenteval gates Aura's BEHAVIOUR the way the rest of CI gates her code.
//
// The repository runs roughly twenty gates — build, vet, lint, deadcode,
// file-size, race, goleak, coverage, mutation, fuzz, contract tests and four live
// integration tiers — and not one of them can tell whether she answers the
// question. So behaviour is free to rot with every gate green, and the only
// detector left is an operator typing something and being disappointed. On
// 2026-08-03 that is exactly how all three of the day's defects surfaced: a wasted
// tool round trip, a memory search that returned the whole memory for an unrelated
// query, and a web search for a customer code that was sitting in the operator's
// own spreadsheet. Each was found by hand, one at a time. The eval oracle that used
// to watch this was removed in acd029d47 and nothing took its place.
//
// This is deliberately NOT that oracle. There is no LLM judge and no rubric: a case
// is a question whose answer is known, and every assertion is a machine check over
// the durable record of what she did. A gate that needs a model to decide whether
// it passed cannot be trusted to gate the model.
package agenteval

import (
	"fmt"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// Case is one question with a machine-checkable expectation. Every field is
// structural — exact tokens, tool names, counts — because natural-language
// matching on an answer is how a gate starts passing for the wrong reason.
type Case struct {
	// Name identifies the case in failures. Kebab-case, stable: it ends up in
	// commit messages when a regression is bisected.
	Name string

	// Question is sent verbatim, in the language the operator would use.
	Question string

	// AnswerContains are exact tokens the final answer must carry. Use values that
	// cannot be produced by paraphrase — a customer code, a row count — never a
	// phrase. "699" is ground truth; "molti clienti" is an opinion about prose.
	AnswerContains []string

	// AnswerMustNotContain are exact tokens the answer must NOT carry. It is the
	// deletion primitive: after an operator deletes a document, the only assertion
	// that means anything is that the number it held has stopped coming out. A
	// catalog row marked deleted proves the catalog, not the deletion — on
	// 2026-08-03 the catalog was correct and she still answered 699 from a copy left
	// in the sandbox.
	AnswerMustNotContain []string

	// RequiredTools passes when AT LEAST ONE was called. It states the capability
	// the question demands without pinning the route, which would make the gate
	// fail on an improvement.
	RequiredTools []string

	// ForbiddenTools must never be called. This is the sharp end: it is what
	// catches her reaching for the open internet to answer a question about the
	// operator's own files.
	ForbiddenTools []string

	// MaxLLMCalls caps the assistant turns. A turn that answers correctly in
	// fourteen calls is a regression against one that answers in six, and nothing
	// else in CI can see the difference.
	MaxLLMCalls int

	// MemoryMustBeEmpty demands that every memory search performed during the turn
	// came back with no facts. It exists because RRF has no notion of relevance:
	// vector.neighbors returns the k nearest however far away they are, and an RRF
	// score is 1/(k+rank) — a rank, not a similarity. With a small memory, "the top
	// five" IS the memory, so a query matching nothing still returns everything.
	// Recall@K, MRR and nDCG all score that behaviour perfectly, which is why a
	// precision case has to be asserted separately or it is never asserted at all.
	MemoryMustBeEmpty bool
}

// Evidence is what the live deployment actually did, read back from the durable
// record rather than from the model's own account of itself.
type Evidence struct {
	// Answer is the final assistant message.
	Answer string
	// LLMCalls is the number of assistant turns the conversation produced.
	LLMCalls int
	// Tools are the tool names in call order, repeats included — the repeats are
	// the signal when a turn loops.
	Tools []string
	// MemoryFactCounts holds one entry per memory-search result, in call order,
	// carrying how many facts it returned.
	MemoryFactCounts []int
}

// EvidenceFromMessages reads the durable conversation record. One persisted
// assistant message is one model call; streamed deltas and tool-result previews
// are deliberately not calls or answers.
func EvidenceFromMessages(messages []llm.Message) Evidence {
	var evidence Evidence
	for _, message := range messages {
		switch message.Role {
		case llm.RoleAssistant:
			evidence.LLMCalls++
			for _, call := range message.ToolCalls {
				evidence.Tools = append(evidence.Tools, call.Function.Name)
			}
			if answer := strings.TrimSpace(message.Content); answer != "" {
				evidence.Answer = answer
			}
		case llm.RoleTool:
			if count, ok := memoryFactCount(message.Content); ok {
				evidence.MemoryFactCounts = append(evidence.MemoryFactCounts, count)
			}
		}
	}
	return evidence
}

// Check returns one line per violation. An empty slice means the case passed; the
// caller reports every violation at once, because a turn that answers wrong AND
// burns twelve calls should say both rather than be fixed twice.
func (c Case) Check(e Evidence) []string {
	var failures []string
	for _, want := range c.AnswerContains {
		if !strings.Contains(e.Answer, want) {
			failures = append(failures, fmt.Sprintf("answer is missing %q; got: %s", want, truncate(e.Answer, 300)))
		}
	}
	for _, banned := range c.AnswerMustNotContain {
		if strings.Contains(e.Answer, banned) {
			failures = append(failures, fmt.Sprintf(
				"the answer still carries %q, which should no longer be reachable; tools: %v — answer: %s",
				banned, e.Tools, truncate(e.Answer, 300)))
		}
	}
	if len(c.RequiredTools) > 0 && !anyCalled(e.Tools, c.RequiredTools) {
		failures = append(failures, fmt.Sprintf("none of the required tools %v was called; called: %v", c.RequiredTools, e.Tools))
	}
	for _, banned := range c.ForbiddenTools {
		if countCalled(e.Tools, banned) > 0 {
			failures = append(failures, fmt.Sprintf("forbidden tool %q was called (%d times); called: %v", banned, countCalled(e.Tools, banned), e.Tools))
		}
	}
	if c.MaxLLMCalls > 0 && e.LLMCalls > c.MaxLLMCalls {
		failures = append(failures, fmt.Sprintf("%d LLM calls, budget is %d; tools: %v", e.LLMCalls, c.MaxLLMCalls, e.Tools))
	}
	if c.MemoryMustBeEmpty {
		for i, n := range e.MemoryFactCounts {
			if n > 0 {
				failures = append(failures, fmt.Sprintf("memory search #%d returned %d facts for a query nothing in memory answers", i+1, n))
			}
		}
	}
	return failures
}

// truncate keeps a failure message readable when the answer is a wall of text.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func anyCalled(called, wanted []string) bool {
	for _, w := range wanted {
		if countCalled(called, w) > 0 {
			return true
		}
	}
	return false
}

// countCalled matches a tool name exactly, so a namespaced MCP tool
// (memory__memory_search) is never mistaken for a built-in with a shared prefix.
func countCalled(called []string, name string) int {
	n := 0
	for _, c := range called {
		if c == name {
			n++
		}
	}
	return n
}
