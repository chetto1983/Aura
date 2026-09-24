// A tool call the provider failed to parse reaches the loop as plain content in the
// model's native markup — GLM's `<tool_call>name<arg_key>…</arg_key><arg_value>…` —
// instead of a structured tool_calls delta. Measured on a production conversation
// (2026-09-24, turn 44): a shell_exec call whose inlined base64 argument degenerated into
// repetition never closed, the provider returned it as text with no "length" finish, and
// the content-stop path saved the markup as the final answer. The call never ran and the
// user had to ask again. Like a truncated tool call it is never an answer and never
// dispatched: the streamed markup is repudiated, the model is nudged once, and a repeat
// finalizes.
package agent

import (
	"log/slog"
	"strings"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/redact"
)

const maxLeakedToolCallTurns = 2

const leakedToolCallNudge = "Your previous reply was a tool call written as plain text, so it never ran and the user only saw raw markup. Call the tool through the tool-calling interface. Keep its arguments small: never inline file contents or base64, write large data to a file in short pieces first. If you already have what you need, answer the user directly."

// leakedToolCallOpeners are the openers found at the start of stored answers; the
// Telegram renderer strips the same two from its live stream. Only an answer that OPENS
// with one counts, so prose explaining the markup stays an answer.
var leakedToolCallOpeners = []string{"<tool_call", "<tool_exec"}

func leakedToolCall(text string) bool {
	head := strings.ToLower(strings.TrimSpace(text))
	for _, opener := range leakedToolCallOpeners {
		if strings.HasPrefix(head, opener) {
			return true
		}
	}
	return false
}

// classifyLeakedToolCall counts leaks per run rather than consecutively: the leaked
// markup is never appended to history, so a leak, a recovery and a second leak must
// still finalize instead of nudging forever.
func (a *LlmAgent) classifyLeakedToolCall(requestID, text string) loopDirective {
	if !leakedToolCall(text) {
		return directiveProceed
	}
	a.leakedToolCallTurns++
	slog.Warn("agent tool call leaked as text: the provider returned call markup as content",
		"request_id", requestID, "thread_id", redact.Line(a.sessionID),
		"content_bytes", len(text), "occurrence", a.leakedToolCallTurns)
	if a.leakedToolCallTurns >= maxLeakedToolCallTurns {
		return directiveFinalize
	}
	a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: leakedToolCallNudge})
	return directiveRetry
}
