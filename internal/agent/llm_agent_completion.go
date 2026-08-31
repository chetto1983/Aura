// Completion critic gate (amendment #54 / D-43, widened by D-20a/D-20b): the
// spine that turns the prompt's "verify before reporting" from prose into an
// enforced contract. Before the loop accepts ANY VOLUNTARY termination
// (text_response or the content-stop fallback), a cheap critic call judges the
// user's request against the OBSERVED tool results — not the agent's claims —
// and vetoes when the promised deliverable is not verifiably present. This
// includes a turn that dispatched nothing mutating at all: HARN-06's failure
// mode is exactly a turn that states an intention and dispatches nothing, which
// a prior side-effect-only trigger never reached. It reuses the maybeRecover
// counter discipline (D-08): a dedicated completionAttempts counter (max
// completionMaxAttempts, 2) keeps the veto bounded — a second veto names what
// did not run, a third attempt is accepted regardless of the critic's verdict —
// the extra turns ride the normal budget gate, and a broken/empty/unparseable
// critic fails OPEN so a verifier outage can never wedge a turn. Concern-split
// out of llm_agent.go to keep that file under the no-god-class cap.
package agent

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

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

// completionVetoPrefix leads the feedback fed back to the model on the FIRST
// veto. It is also matched by lastUserRequest so a prior veto nudge is never
// mistaken for the user's actual request when the gate runs again in the same
// run.
const completionVetoPrefix = "Completion check FAILED: "

// completionSecondVetoPrefix leads the feedback on the SECOND (final) veto
// (D-20b). Unlike the first nudge, it demands the turn state plainly which
// action did not run and why, rather than repeating the first nudge's generic
// instruction — and it must never suggest claiming completion, since a third
// attempt is accepted unconditionally regardless of what the model says here.
const completionSecondVetoPrefix = "Completion check FAILED again: "

// completionMaxAttempts bounds gateCompletion's veto budget: at most this many
// vetoes per run (D-20b). A third attempt is accepted regardless of the critic's
// verdict, so there is no path to an unbounded critic loop.
const completionMaxAttempts = 2

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
// D-43, widened by D-20a/D-20b). It returns veto=true (with feedback for the
// model) only when ALL hold: the gate is enabled (Load() default; off in
// hand-built test configs), the per-run veto budget is unspent, and the critic
// returns NOT_DONE. EVERY voluntary termination is judged now, including a turn
// that dispatched no mutating tool at all — HARN-06's failure mode is a turn that
// states an intention and dispatches nothing, and the critic's own prompt already
// treats a well-supported answer to a read-only question as done, so widening the
// trigger costs tokens, not false vetoes. Any other case — gate off, counter
// spent, critic DONE, or critic broken/empty/unparseable (fail-open) — returns
// veto=false so the termination proceeds unchanged. On a veto it spends the
// counter; the gate fires at most completionMaxAttempts (2) times per run, and a
// third attempt is accepted regardless of the critic's verdict.
func (a *LlmAgent) gateCompletion(ic InvocationContext, answer string) (veto bool, feedback string) {
	if !a.cfg.CompletionGate || a.completionAttempts >= completionMaxAttempts {
		return false, ""
	}
	// Local, deterministic checks first (45-09). They cost no tokens and they catch
	// what the LLM critic structurally cannot: the critic judges whether the WORK is
	// done, not whether the prose about it is fit to send. 45-08 measured a turn that
	// was substantively complete AND leaked drafting notes carrying invented
	// identifiers -- a correct DONE verdict on an unsendable reply.
	if veto, feedback := a.gateReplyHygiene(answer); veto {
		a.completionAttempts++
		slog.Info("agent completion gate: answer vetoed",
			"source", "reply_hygiene", "attempt", a.completionAttempts, "answer_runes", utf8.RuneCountInString(answer))
		return true, feedback
	}
	done, reason, ok := a.runCompletionCritic(ic, answer)
	if !ok {
		// Fail-open. Invisible until now, which matters: a verifier that is quietly
		// broken looks exactly like a verifier that keeps approving.
		slog.Warn("agent completion gate: critic unavailable, accepting the answer unchecked")
		return false, ""
	}
	if done {
		return false, "" // the deliverable is verified
	}
	a.completionAttempts++
	if strings.TrimSpace(reason) == "" {
		reason = "the requested deliverable is not present in the tool results"
	}
	// The gate used to log NOTHING — not a veto, not a reason, not the fail-open. An
	// operator watching drafts appear and vanish could only say "it seems to always
	// discard"; there was no number to check that against and no verdict to read.
	slog.Info("agent completion gate: answer vetoed",
		"source", "critic", "attempt", a.completionAttempts, "verdict", reason,
		"answer_runes", utf8.RuneCountInString(answer))
	if a.completionAttempts >= completionMaxAttempts {
		return true, completionSecondVetoPrefix + reason + "\nState in one sentence which action did not run and why — " +
			"do not claim completion. Then perform that action now, verify it, and only then call text_response."
	}
	return true, completionVetoPrefix + reason + "\nThe deliverable is not verified yet. Do NOT end the turn — " +
		"perform the missing action now (run the script, produce the file), then read it back to confirm, and only then call text_response."
}

// runCompletionCritic issues the tool-free critic call and parses the verdict. It
// never spends a budget step (like finalize/synthesize) and is bounded to at most
// completionMaxAttempts (2) calls per run by the completionAttempts gate in
// gateCompletion. ok=false on a transport error, empty stream, or an unparseable
// verdict → the caller fails open. The request is built directly (NOT through the
// prompt builder) so it carries only the compact critic context, not the full
// system prompt + tools.
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

	// Fresh deadline (fix-plan 1.1): the critic gate runs on a voluntary termination
	// that can land right at the wallclock cutoff, where ic.Ctx (production drives it
	// from budget.WithDeadline, internal/runner/runner.go) may already be Done —
	// context.WithoutCancel severs that expired deadline (keeping request-scoped
	// values) so the critic call is never dead-on-arrival. See the longer rationale
	// on the mirrored synthesis call in llm_agent_finalize.go.
	callCtx, cancel := context.WithTimeout(context.WithoutCancel(ic.Ctx), time.Duration(a.cfg.TotalTimeoutSec)*time.Second)
	defer cancel()
	callCtx, llmEnd := llmCallBoundary.Start(callCtx)
	var boundaryErr error
	defer llmEnd.PanicSafe(&boundaryErr)

	ch, err := a.streamWithOpenRetry(callCtx, req, ic.RequestID.String()+":completion_critic")
	if err != nil {
		boundaryErr = err
		recordLLMError(llmErrorKind("completion_critic_open", err))
		return false, "", false
	}
	var b strings.Builder
	for c := range ch {
		if c.Err != nil {
			boundaryErr = c.Err
			recordLLMError(llmErrorKind("completion_critic_stream", c.Err))
			return false, "", false
		}
		if c.Usage != nil {
			recordUsage(*c.Usage)
		}
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
		line.WriteString(truncateBytesKeepingTail(results[id], criticResultCap))
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
	return truncateBytesKeepingTail(strings.Join(lines[start:], ""), criticDigestCap)
}

func truncateBytesKeepingTail(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	const marker = "\n...[truncated]...\n"
	if n <= len(marker) {
		return truncateTailBytes(s, n)
	}
	remaining := n - len(marker)
	headCap := remaining / 2
	tailCap := remaining - headCap
	return truncateBytes(s, headCap) + marker + truncateTailBytes(s, tailCap)
}

func truncateTailBytes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	start := len(s) - n
	for start < len(s) && !utf8.RuneStart(s[start]) {
		start++
	}
	return s[start:]
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
	for _, v := range slices.Backward(history) {
		m := v
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
		strings.HasPrefix(content, completionVetoPrefix) ||
		strings.HasPrefix(content, completionSecondVetoPrefix) ||
		strings.HasPrefix(content, verifyOnStopNudgePrefix) ||
		strings.HasPrefix(content, deliverOnStopNudgePrefix)
}
