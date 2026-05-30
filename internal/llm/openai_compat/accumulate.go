package openai_compat

import (
	"sort"

	"github.com/chetto1983/aura/internal/llm"
)

// toolCallDelta is the wire-delta shape of one streamed tool_calls element. It
// carries an Index (0,1,…) absent from the public llm.ToolCall (which is
// history-shaped, not wire-delta-shaped — RESEARCH Pattern 2 anti-pattern: do
// NOT add Index to llm.ToolCall). The first fragment for an index carries id +
// function.name; subsequent fragments carry function.arguments to concatenate.
type toolCallDelta struct {
	Index    int    `json:"index"`
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// toolCallAcc accumulates the fragments for one index into a single tool call.
// id/typ/name are set by the first fragment that carries them; args is appended
// across every fragment. firstSeen records arrival order as a stable tiebreaker.
type toolCallAcc struct {
	id        string
	typ       string
	name      string
	args      string
	firstSeen int
}

// accumulator merges tool_call deltas (which arrive split across SSE chunks) by
// index into one llm.ToolCall per index. It is single-goroutine — owned by the
// one stream goroutine — so it needs no locking.
type accumulator struct {
	byIndex map[int]*toolCallAcc
	order   int
}

func newAccumulator() *accumulator {
	return &accumulator{byIndex: make(map[int]*toolCallAcc)}
}

// add folds one wire delta into the per-index accumulator. id/type/name overwrite
// only when the fragment supplies them (the first fragment for an index); args
// always concatenate so a JSON value split across ≥3 chunks reassembles exactly.
func (a *accumulator) add(d toolCallDelta) {
	acc, ok := a.byIndex[d.Index]
	if !ok {
		acc = &toolCallAcc{firstSeen: a.order}
		a.order++
		a.byIndex[d.Index] = acc
	}
	if d.ID != "" {
		acc.id = d.ID
	}
	if d.Type != "" {
		acc.typ = d.Type
	}
	if d.Function.Name != "" {
		acc.name = d.Function.Name
	}
	acc.args += d.Function.Arguments
}

// empty reports whether any tool-call fragment has been seen.
func (a *accumulator) empty() bool { return len(a.byIndex) == 0 }

// finalize emits one llm.ToolCall per accumulated index, ordered by index
// (stable, deterministic) so the agent appends RoleTool results in a fixed
// order. Type defaults to "function" (OpenAI compat) when the wire omitted it.
func (a *accumulator) finalize() []llm.ToolCall {
	if a.empty() {
		return nil
	}
	indices := make([]int, 0, len(a.byIndex))
	for idx := range a.byIndex {
		indices = append(indices, idx)
	}
	sort.Ints(indices)

	out := make([]llm.ToolCall, 0, len(indices))
	for _, idx := range indices {
		acc := a.byIndex[idx]
		typ := acc.typ
		if typ == "" {
			typ = "function"
		}
		var tc llm.ToolCall
		tc.ID = acc.id
		tc.Type = typ
		tc.Function.Name = acc.name
		tc.Function.Arguments = acc.args
		out = append(out, tc)
	}
	return out
}
