package conversations

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/redact"
	"github.com/pkoukk/tiktoken-go"
)

// compaction_transcript.go turns the rounds being compacted into the summarizer's source
// material, and decides how long the summary it asks for should be.
//
// Every number here is hermes-agent's, read from `agent/context_compressor.py` rather than
// chosen: the file this package ports its prompt from also solved the sizing, and the
// version we had invented its own numbers and got them wrong in both directions.
//
// The load-bearing rule is hermes' explicit contract, at the summarize call site:
//
//	# NO max_tokens: the output cap must never truncate a summary.
//	# ``summary_budget`` is prompt-level guidance only ("Target ~N tokens")
//
// and at the input constant:
//
//	# This is a prompt-side bound only — NEVER add a max_tokens wire cap on the
//	# summary call (see the no-wire-cap contract test in
//	# test_compression_small_ctx_threshold_floor.py).
//
// hermes records WHY, and it is exactly what this deployment measured on 2026-08-17: a hard
// cap on the wire "cut summaries mid-section (thinking models burn the cap on reasoning
// first), producing truncated/thinking-only summaries and compaction loops". Our cap was
// 1024, the stream ended finish_reason="length", and Summarize failed — silently for the two
// automatic compactions that then hard-dropped history, and as a 500 for `/compact`.
//
// A bound on the INPUT is a different thing and hermes keeps it: the summarizer has its own
// window, and a session can serialize to hundreds of KB. What we had was the right idea at
// a tenth of the right size, and clipped only one edge.

const (
	// Per-message body bounds. Both edges are kept because they carry different things: the
	// start of a message says what it is, the end says how it came out. Clipping only the
	// head — which is what this did before, at 400 chars for a tool result — reliably keeps
	// a command and drops its exit code.
	summaryContentMax  = 6000
	summaryContentHead = 4000
	summaryContentTail = 1500

	// Aggregate bound over the whole serialized block, applied AFTER the per-message ones.
	// 160K chars is about 40K tokens: inside every model that can serve as a summarizer,
	// with room left for the template and the carried previous summary.
	summaryInputMaxChars  = 160_000
	summaryInputHeadShare = 0.45

	// The prompt-side length target. It scales with the amount being compressed
	// (summaryRatio) so a short history is not asked for a long summary, and its ceiling
	// scales with the window (summaryWindowShare) so a large-context model gets a richer
	// summary instead of a fixed one. summaryMinTokens is a floor, not a cap: below it a
	// summary cannot fill the template at all, which is the failure we started from.
	summaryRatio         = 0.20
	summaryMinTokens     = 2000
	summaryTokensCeiling = 10_000
	summaryWindowShare   = 0.05
)

const summaryTruncationMarker = "\n...[truncated]...\n"

// renderRoundsForSummary flattens the rounds into a labelled transcript, bounding each
// message and then the whole block.
func renderRoundsForSummary(rounds []llm.Message) string {
	rendered := make([]string, 0, len(rounds))
	for _, m := range rounds {
		if m.Role != llm.RoleUser && m.Role != llm.RoleAssistant && m.Role != llm.RoleTool {
			continue
		}
		content := strings.TrimSpace(m.Content)
		if content == "" {
			continue
		}
		content = keepEdges(content, summaryContentMax, summaryContentHead, summaryContentTail,
			summaryTruncationMarker)
		// Bound first, then redact: an over-bound secret in the omitted middle is already
		// gone and the patterns scan a bounded input (the ledger's ordering, same reason).
		//
		// The transcript carries verbatim user text and tool output, so it can carry a
		// credential the model put on a command line. It leaves the process for an external
		// summarizer, and the summary it produces is about to become DURABLE — which turns a
		// transient exposure into a stored one.
		rendered = append(rendered, m.Role+": "+redact.String(content))
	}
	if len(rendered) == 0 {
		return ""
	}
	return boundSummaryInput(strings.Join(rendered, "\n"))
}

// boundSummaryInput caps the serialized block, keeping both edges and naming what it left
// out. The marker is not politeness: a summarizer handed a silently-cut transcript writes a
// confident summary of a conversation that never happened.
func boundSummaryInput(content string) string {
	if len(content) <= summaryInputMaxChars {
		return content
	}
	marker := func(omitted int) string {
		return fmt.Sprintf(
			"\n\n...[summary input truncated: omitted %d chars from the middle to keep the compression prompt bounded]...\n\n",
			omitted)
	}
	// Two passes, as hermes does: the marker's own length depends on the number of omitted
	// chars, which depends on the split, which depends on the marker's length.
	split := func(markerLen int) (head, tail int) {
		remaining := max(summaryInputMaxChars-markerLen, 0)
		head = int(float64(remaining) * summaryInputHeadShare)
		return head, remaining - head
	}
	head, tail := split(len(marker(len(content))))
	head, tail = split(len(marker(max(len(content)-head-tail, 0))))
	return keepEdges(content, head+tail, head, tail, marker(max(len(content)-head-tail, 0)))
}

// keepEdges returns s unchanged when it is at most limit chars, and otherwise its first head
// and last tail chars joined by marker. Cuts land on UTF-8 rune boundaries: a naive byte
// slice can hand the summarizer half a rune.
func keepEdges(s string, limit, head, tail int, marker string) string {
	if len(s) <= limit {
		return s
	}
	cut := runeStart(s, head)
	// max, not a plain slice: a marker longer than the budget could otherwise put the tail's
	// start before the head's end and duplicate the overlap.
	resume := max(runeStart(s, len(s)-tail), cut)
	return strings.TrimRight(s[:cut], " \n") + marker + strings.TrimLeft(s[resume:], " \n")
}

// runeStart walks i back to the start of a rune (continuation bytes are 0b10xxxxxx).
func runeStart(s string, i int) int {
	if i <= 0 {
		return 0
	}
	if i >= len(s) {
		return len(s)
	}
	for i > 0 && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// summaryBudget is the "Target ~N tokens" the prompt asks for — guidance, never a cap. It is
// 20% of what is being compressed, floored so the template is always fillable and ceilinged
// at 5% of the window (never more than summaryTokensCeiling), because a summary large enough
// to be its own context pressure defeats the compaction that produced it.
func summaryBudget(enc *tiktoken.Tiktoken, transcript string, contextWindow int) int {
	ceiling := summaryTokensCeiling
	if contextWindow > 0 {
		ceiling = min(int(float64(contextWindow)*summaryWindowShare), summaryTokensCeiling)
	}
	return max(summaryMinTokens, min(int(float64(countTokens(enc, transcript))*summaryRatio), ceiling))
}
