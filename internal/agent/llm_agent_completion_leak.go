package agent

import (
	"regexp"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
)

// Detection for the two user-facing failures 45-08 measured on the running stack
// (45-09): deliberation that leaked into the reply, and an identifier the reply
// presents as real that no tool ever returned.
//
// 45-05 closed D-21 with a system-prompt rule. That rule is present in the shipped
// binary -- verified by grep inside aura:local at build ed252f6b6 -- and the model
// violated it anyway on a long verbatim-report turn, in English, to an operator
// writing Italian, while inventing fact_key values it then admitted inventing in the
// same visible text. A rule the model is asked to follow is necessary and, measured,
// not sufficient; this is the part the harness can check itself.
//
// Both functions read ONLY operator-facing text. Deliberation in the reasoning
// channel is correct and expected -- on the measured turn that channel held 15108
// characters of it and none of the markers below -- so a detector that scanned both
// would fire on every healthy turn and be worse than nothing.

// deliberationMarkers are self-DIRECTED corrections: the model addressing its own
// drafting rather than the operator. Each was observed in operator-facing text on the
// measured turn, or is the same act in the operator's language.
//
// The list is deliberately about self-address, not about the first person. "Ho cercato
// in memoria" and "I searched memory and found nothing" are results and must pass
// clean; "let me be careful to copy them exactly" is the model talking to itself with
// the operator watching.
var deliberationMarkers = []string{
	"hmm, wait",
	"ok stop",
	"i'm making this up",
	"im making this up",
	"i shouldn't",
	"i shouldnt",
	"let me be careful",
	"let me just write",
	"wait, i need to",
	"actually, let me",
	"no. i'm",
	"aspetta, no",
	"fammi ricontrollare",
	"devo stare attento",
}

// factKeyShape matches the 64-lowercase-hex fact_key the memory tools return. The
// bounding is explicit rather than \b: a hex run longer than 64 is some other
// identifier and must not be mistaken for a fact_key.
var factKeyShape = regexp.MustCompile(`(^|[^0-9a-f])([0-9a-f]{64})([^0-9a-f]|$)`)

// leakedDeliberation reports the first self-directed deliberation marker found in an
// operator-facing reply.
//
// Fenced and blockquoted spans are stripped first: a transcript the operator pasted
// back, or tool output echoed as evidence, is the model SHOWING text rather than
// thinking out loud, and flagging it would make quoting unanswerable.
func leakedDeliberation(answer string) (marker string, leaked bool) {
	visible := strings.ToLower(stripQuotedSpans(answer))
	for _, m := range deliberationMarkers {
		if strings.Contains(visible, m) {
			return m, true
		}
	}
	return "", false
}

// unsourcedIdentifiers returns fact_key-shaped tokens that appear in the reply but in
// none of the turn's tool results -- the sharpest available signal for a fabricated
// identifier, because a real fact_key can only have come from a tool.
//
// Quoted spans are NOT stripped here, unlike leakedDeliberation: a key presented
// inside a code fence is exactly how the measured failure presented one, and a fence
// is not evidence of provenance.
func unsourcedIdentifiers(answer string, toolResults []string) []string {
	matches := factKeyShape.FindAllStringSubmatch(answer, -1)
	if len(matches) == 0 {
		return nil
	}
	sourced := strings.Join(toolResults, "\n")
	var unsourced []string
	seen := map[string]bool{}
	for _, m := range matches {
		key := m[2]
		if seen[key] || strings.Contains(sourced, key) {
			continue
		}
		seen[key] = true
		unsourced = append(unsourced, key)
	}
	return unsourced
}

// gateReplyHygiene vetoes a voluntary termination whose operator-facing text is unfit
// to send, independently of whether the work behind it was done.
//
// It reports through gateCompletion's existing veto, adding no parallel mechanism, and
// it never rewrites the reply: laundering the text would hide the failure from the
// operator instead of surfacing it, and an error that passes silently is the habit
// this whole phase exists to break. The model is told what tripped and asked to send
// the result again without it.
func (a *LlmAgent) gateReplyHygiene(answer string) (veto bool, feedback string) {
	if keys := unsourcedIdentifiers(answer, a.toolResultTexts()); len(keys) > 0 {
		return true, completionVetoPrefix + "the reply presents identifier " + keys[0] +
			" as real, but no tool result this turn returned it.\nDo NOT end the turn. " +
			"Remove any identifier you did not read from a tool result, or recall it and quote what came back."
	}
	if marker, leaked := leakedDeliberation(answer); leaked {
		return true, completionVetoPrefix + "the reply contains drafting notes (" + marker +
			") that are for you, not for the operator.\nDo NOT end the turn. Send the result again in the " +
			"operator's language, carrying the answer and its essential context but not the route you took to it."
	}
	return false, ""
}

// toolResultTexts collects this turn's tool-result contents, the only legitimate
// provenance for an identifier the reply presents as real.
func (a *LlmAgent) toolResultTexts() []string {
	out := make([]string, 0, len(a.history))
	for i := range a.history {
		if m := a.history[i]; m.Role == llm.RoleTool && m.Content != "" {
			out = append(out, m.Content)
		}
	}
	return out
}

// stripQuotedSpans removes fenced blocks and blockquote lines so only the model's own
// prose is inspected.
func stripQuotedSpans(s string) string {
	var b strings.Builder
	fenced := false
	for line := range strings.SplitSeq(s, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fenced = !fenced
			continue
		}
		if fenced || strings.HasPrefix(strings.TrimSpace(line), ">") {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}
