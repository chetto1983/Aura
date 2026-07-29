package agent

import "github.com/chetto1983/aura/internal/llm"

// turnUsage accumulates the usage of EVERY LLM call a turn makes.
//
// It replaces a loop-local variable that was re-created on each iteration, so a turn
// with N tool round-trips reported the tokens and cost of its LAST call and dropped the
// other N. Measured on the appliance: 24 assistant turns, 5 with any usage recorded —
// and the twelve blanks were exactly the steps that had emitted tool calls. Single-call
// turns matched the provider to the cent, which is why the gap stayed invisible.
//
// Summing prompt tokens across calls is deliberate. They are what the provider BILLED:
// every call in the loop re-sends the whole prefix and is charged for it. The figure
// answers "what did this turn cost", not "how large was the final prompt" — and cost is
// the question every consumer of it asks.
type turnUsage struct {
	prompt     int
	completion int
	cached     int
	cost       float64
	hasCost    bool
}

// add folds one LLM call's usage into the turn total.
func (t *turnUsage) add(u llm.Usage) {
	t.prompt += u.PromptTokens
	t.completion += u.CompletionTokens
	t.cached += u.CachedTokens
	if u.Cost != nil {
		t.cost += *u.Cost
		t.hasCost = true
	}
}

// total projects the accumulated calls onto the llm.Usage the terminal Event carries.
//
// Cost stays nil unless at least one call reported one, preserving the D-18/D-23 honest
// absence: a turn whose calls all omitted cost must fall back to the rate table rather
// than publish an authoritative-looking 0. A partial sum (some calls priced, some not)
// still returns non-nil — it is the provider's own figure for the calls that carried it,
// and understating a known cost is better than discarding it.
func (t *turnUsage) total() llm.Usage {
	out := llm.Usage{
		PromptTokens:     t.prompt,
		CompletionTokens: t.completion,
		CachedTokens:     t.cached,
	}
	if t.hasCost {
		c := t.cost
		out.Cost = &c
	}
	return out
}
