package agent

import "github.com/chetto1983/aura/internal/llm"

// HistoryForTest exposes the agent's in-memory message history to black-box
// tests (package agent_test) that must assert pause-seam side effects — no
// RoleTool appended on a pause, and the assistant message rewritten to
// ask_user-only tool_calls (D-A1-07). It returns a copy so a test cannot mutate
// the agent's history. Test-only surface (this file compiles only under `go test`).
func (a *LlmAgent) HistoryForTest() []llm.Message {
	return append([]llm.Message(nil), a.history...)
}
