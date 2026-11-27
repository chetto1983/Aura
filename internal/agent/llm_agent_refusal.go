package agent

import "github.com/chetto1983/aura/internal/llm"

// refusalNotice is the answer of a turn the model refused (finish content_filter).
const refusalNotice = "[risposta rifiutata dal modello: content_filter]"

// endOnRefusal ends the turn on a refused round and returns its outcome. A refusal
// is the model's answer, not a provider hiccup: the empty-answer recovery (nudge,
// retry, finalize synthesis) would only ask again, and a tool call of a refused
// response is not dispatched. Whatever the round streamed is repudiated, so the
// user sees the notice, never a refused partial.
func (a *LlmAgent) endOnRefusal(ic InvocationContext, spanID [8]byte, requestID, streamed string,
	acc *turnUsage, yield func(*Event, error) bool,
) string {
	if streamed != "" && !a.repudiateStreamed(ic, spanID, yield) {
		return "consumer_stopped"
	}
	a.history = append(a.history, llm.Message{Role: llm.RoleAssistant, Content: refusalNotice})
	yield(a.finalEvent(ic, spanID, requestID, refusalNotice, llm.FinishContentFilter, acc.total()), nil)
	return "refused"
}
