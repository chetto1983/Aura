// Output-budget truncation handling, split out of llm_agent.go to keep that file
// under the no-god-class cap. When a tool-call turn finishes with finish_reason=
// "length" its arguments were cut mid-JSON by the output budget, so dispatching it
// yields "unexpected end of JSON input" and the model retries the same oversized call
// — the 203-turn truncation thrash observed live 2026-06-14. Tool turns get the full
// output budget (reasoning_policy floor) so this fires only on a genuinely oversized
// call; when it does, nudge once and finalize on a repeat so the loop can never thrash.
package agent

import "github.com/chetto1983/aura/internal/llm"

// maxTruncatedToolTurns bounds consecutive finish_reason="length" tool-call turns
// before the loop force-finalizes: the first truncation nudges the model to shrink the
// call, a second routes to finalize.
const maxTruncatedToolTurns = 2

// truncatedToolNudge steers the model off the doomed retry path when its tool-call
// arguments were cut mid-JSON by the output budget.
const truncatedToolNudge = "Your previous tool call was cut off mid-argument because it exceeded the output budget. Do NOT retry the same oversized call. Either write large content to a file in small successive pieces (short fs_write/fs_edit chunks, appending), or, if you already have enough to answer, give the user your final answer now."

// truncationAction is the loop directive after classifying a turn's finish_reason for
// output-budget truncation.
type truncationAction int

const (
	truncationProceed  truncationAction = iota // not truncated → dispatch normally
	truncationContinue                         // first truncation → nudged, run another turn
	truncationFinalize                         // repeated truncation → force-finalize
)

// classifyToolTruncation records a tool-call turn's truncation state and returns the
// loop directive. A clean (non-"length") turn resets the per-run counter and proceeds;
// the first truncation appends a single nudge and asks the loop to continue; a second
// consecutive truncation asks it to finalize.
func (a *LlmAgent) classifyToolTruncation(finish string) truncationAction {
	if finish != "length" {
		a.truncatedToolTurns = 0
		return truncationProceed
	}
	a.truncatedToolTurns++
	if a.truncatedToolTurns >= maxTruncatedToolTurns {
		return truncationFinalize
	}
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: truncatedToolNudge})
	return truncationContinue
}
