// Completion critic gate (amendment #54 / D-43): the spine that turns the
// prompt's "verify before reporting" from prose into an enforced contract.
// Before the loop accepts a VOLUNTARY termination (text_response or the
// content-stop fallback) on a turn that mutated host state, a cheap critic call
// judges the user's request against the OBSERVED tool results — not the agent's
// claims — and vetoes ONCE when the promised deliverable is not verifiably
// present. It reuses the maybeRecover counter discipline (D-08): a dedicated
// completionAttempts counter (max 1) keeps the veto bounded, the extra turn
// rides the normal budget gate, and a broken/empty/unparseable critic fails
// OPEN so a verifier outage can never wedge a turn. Concern-split out of
// llm_agent.go to keep that file under the no-god-class cap.
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/llm"
)

// completionCriticSystem instructs the critic to grade by tool RESULTS, never by
// the agent's prose. Load-bearing wording: "a script written but never executed
// ... is NOT done" is the exact Borsa failure mode this gate exists to catch.
const completionCriticSystem = "You are a strict completion auditor for an autonomous agent that works on a real machine. " +
	"You are given the user's request, a log of the tool calls the agent made this turn with their results, and the agent's proposed final answer. " +
	"Decide whether the concrete deliverable the user asked for actually EXISTS and is VERIFIED by the tool results — not merely promised, described, or left as a script for the user to run themselves. " +
	"Judge ONLY by the tool results, never by the agent's claims. A script that was written but never executed, a file the agent says it created but no tool result confirms exists, or an answer that tells the user to run something themselves, is NOT done. " +
	"If the user only asked a question and no artifact was requested, a well-supported answer IS done. " +
	"Reply with a single line: `DONE` if the deliverable is present and verified, or `NOT_DONE: <one short sentence naming what is missing and the next concrete action>`. Output nothing else."

// completionVetoPrefix leads the feedback fed back to the model on a veto. It is
// also matched by lastUserRequest so a prior veto nudge is never mistaken for the
// user's actual request when the gate runs a second time in the same run.
const completionVetoPrefix = "Completion check FAILED: "

// criticMaxTokens caps the verdict (one short line); criticArgsCap/ResultCap/
// DigestCap bound the side-effect digest so a runaway tool result cannot inflate
// the critic request.
const (
	criticMaxTokens = 256
	criticArgsCap   = 200
	criticResultCap = 400
	criticDigestCap = 4000
)

// gateCompletion decides whether to VETO a voluntary termination (amendment #54 /
// D-43). It returns veto=true (with feedback for the model) only when ALL hold:
// the gate is enabled (Load() default; off in hand-built test configs), the turn
// mutated host state, the per-run veto budget is unspent, and the critic returns
// NOT_DONE. Any other case — gate off, read-only turn, counter spent, critic
// DONE, or critic broken/empty/unparseable (fail-open) — returns veto=false so
// the termination proceeds unchanged. On a veto it spends the counter so the
// gate fires at most once per run.
func (a *LlmAgent) gateCompletion(ic InvocationContext, answer string) (veto bool, feedback string) {
	if !a.cfg.CompletionGate || !a.sideEffected || a.completionAttempts >= 1 {
		return false, ""
	}
	done, reason, ok := a.runCompletionCritic(ic, answer)
	if !ok || done {
		return false, "" // fail-open, or the deliverable is verified
	}
	a.completionAttempts++
	if strings.TrimSpace(reason) == "" {
		reason = "the requested deliverable is not present in the tool results"
	}
	return true, completionVetoPrefix + reason + "\nThe deliverable is not verified yet. Do NOT end the turn — " +
		"perform the missing action now (run the script, produce the file), then read it back to confirm, and only then call text_response."
}

// runCompletionCritic issues the tool-free critic call and parses the verdict. It
// never spends a budget step (like finalize/synthesize) and is bounded to one
// call per run by the completionAttempts gate in gateCompletion. ok=false on a
// transport error, empty stream, or an unparseable verdict → the caller fails
// open. The request is built directly (NOT through the prompt builder) so it
// carries only the compact critic context, not the full system prompt + tools.
func (a *LlmAgent) runCompletionCritic(ic InvocationContext, answer string) (done bool, reason string, ok bool) {
	req := llm.Request{
		Model: a.criticModel(),
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: completionCriticSystem},
			{Role: llm.RoleUser, Content: a.completionCriticUser(answer)},
		},
		Temperature: 0,
		MaxTokens:   criticMaxTokens,
		ToolChoice:  "none",
		SessionID:   a.sessionID,
	}

	callCtx, cancel := context.WithTimeout(ic.Ctx, time.Duration(a.cfg.TotalTimeoutSec)*time.Second)
	defer cancel()

	ch, err := a.client.Stream(callCtx, req)
	if err != nil {
		return false, "", false
	}
	var b strings.Builder
	for c := range ch {
		if c.Text != "" {
			b.WriteString(c.Text)
		}
	}
	return parseCriticVerdict(b.String())
}

// criticModel resolves the critic model: the explicit override, else the loop
// model (the gate must never run without a model).
func (a *LlmAgent) criticModel() string {
	if m := strings.TrimSpace(a.cfg.CompletionCriticModel); m != "" {
		return m
	}
	return a.cfg.Model
}

// completionCriticUser renders the compact critic context: the user's active
// request, the side-effect digest, and the proposed final answer.
func (a *LlmAgent) completionCriticUser(answer string) string {
	return fmt.Sprintf("<user_request>\n%s\n</user_request>\n\n<tool_activity>%s\n</tool_activity>\n\n<proposed_final_answer>\n%s\n</proposed_final_answer>",
		lastUserRequest(a.history), a.sideEffectDigest(), answer)
}

// sideEffectDigest builds a `- name(args) → result` line per tool call in this
// run (the terminal tool excluded), preserving call order for the selected
// suffix and bounding each field + the whole digest. Reads are kept too: a
// read-back after a write is the verification evidence the critic needs. When a
// long setup phase would overflow the digest, the latest tool results win because
// they carry the final creation/verification evidence.
func (a *LlmAgent) sideEffectDigest() string {
	type toolCall struct{ name, args string }
	calls := map[string]toolCall{}
	order := make([]string, 0, len(a.history))
	results := map[string]string{}
	for i := range a.history {
		for _, tc := range a.history[i].ToolCalls {
			if _, seen := calls[tc.ID]; !seen {
				order = append(order, tc.ID)
			}
			calls[tc.ID] = toolCall{tc.Function.Name, tc.Function.Arguments}
		}
		if m := a.history[i]; m.Role == llm.RoleTool && m.ToolCallID != "" {
			results[m.ToolCallID] = m.Content
		}
	}
	lines := make([]string, 0, len(order))
	for _, id := range order {
		c := calls[id]
		if c.name == terminalTool {
			continue
		}
		var line strings.Builder
		line.WriteString("\n- ")
		line.WriteString(c.name)
		line.WriteString("(")
		line.WriteString(truncateBytes(c.args, criticArgsCap))
		line.WriteString(") → ")
		line.WriteString(truncateBytes(results[id], criticResultCap))
		lines = append(lines, line.String())
	}
	if len(lines) == 0 {
		return "\n(no tool calls)"
	}
	start := len(lines)
	total := 0
	for start > 0 {
		next := len(lines[start-1])
		if total > 0 && total+next > criticDigestCap {
			break
		}
		start--
		total += next
		if total >= criticDigestCap {
			break
		}
	}
	return truncateBytes(strings.Join(lines[start:], ""), criticDigestCap)
}

// parseCriticVerdict reads the DONE / NOT_DONE verdict. NOT_DONE is checked first
// because it contains the substring "DONE". An answer with neither token is
// unparseable → ok=false (the caller fails open).
func parseCriticVerdict(text string) (done bool, reason string, ok bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false, "", false
	}
	upper := strings.ToUpper(trimmed)
	if i := strings.Index(upper, "NOT_DONE"); i >= 0 {
		return false, extractReason(trimmed[i+len("NOT_DONE"):]), true
	}
	if i := strings.Index(upper, "NOT DONE"); i >= 0 {
		return false, extractReason(trimmed[i+len("NOT DONE"):]), true
	}
	if strings.Contains(upper, "DONE") {
		return true, "", true
	}
	return false, "", false
}

// extractReason strips the leading separator (": " / " - ") after the NOT_DONE
// token and returns the first line of the reason.
func extractReason(tail string) string {
	tail = strings.TrimLeft(tail, ": -\t")
	if nl := strings.IndexByte(tail, '\n'); nl >= 0 {
		tail = tail[:nl]
	}
	return strings.TrimSpace(tail)
}

// lastUserRequest returns the most recent genuine user turn, skipping the agent's
// own injected user-role nudges (recovery + completion-veto) so the critic grades
// against the real request even after a prior nudge.
func lastUserRequest(history []llm.Message) string {
	for i := len(history) - 1; i >= 0; i-- {
		m := history[i]
		if m.Role != llm.RoleUser || strings.TrimSpace(m.Content) == "" {
			continue
		}
		if isAgentNudge(m.Content) {
			continue
		}
		return m.Content
	}
	return ""
}

// isAgentNudge reports whether content is one of the agent's own injected
// user-role messages (so lastUserRequest never mistakes a nudge for the request).
func isAgentNudge(content string) bool {
	return content == recoveryNudgeGeneric ||
		content == recoveryNudgeEmpty ||
		strings.HasPrefix(content, recoveryNudgeToolPrefix) ||
		strings.HasPrefix(content, completionVetoPrefix)
}
