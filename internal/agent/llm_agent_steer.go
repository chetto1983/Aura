// Concern-split out of llm_agent.go (mirroring llm_agent_finalize.go's own
// precedent): llm_agent.go is at its no-god-class headroom, so both steer
// drain points, the attribution marker, and the lookalike scrub live here.
package agent

import (
	"html"
	"regexp"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/steer"
)

// SteerInbox is the narrow consumer-side contract drainSteer needs. Defined
// here rather than referencing *steer.Inbox directly so internal/agent stays
// testable with a fake and never takes a hard dependency on the concrete
// inbox type; *steer.Inbox satisfies it by construction (identical Drain
// signature) — no adapter needed.
type SteerInbox interface {
	Drain(conv string) []steer.Message
}

// steerMarkerOpen/steerMarkerClose bound the non-forgeable attribution
// envelope wrapUserSteer mints. Package-visible so a test can locate the
// marker's position without re-deriving the tag shape.
const (
	steerMarkerOpen  = `<user_steer nonce="`
	steerMarkerClose = `</user_steer>`
)

// wrapUserSteer mints the attribution envelope: reuses toolOutputNonce()
// (trust.go) — there is exactly one nonce minter in internal/agent
// (CLAUDE.md's inventory-before-invention rule; amendment #132 D-07 says so
// explicitly). Unlike wrapUntrustedToolOutput, this does NOT HTML-escape
// text: the envelope wraps the OPERATOR's own words, not untrusted content,
// and it is appended OUTSIDE the already-escaped tool envelope, never inside
// it — double-escaping the operator's own message would be wrong, and the
// byte-identity-on-the-wire backstop truth depends on the envelope reaching
// the provider exactly as minted. The leading newline is the separator a
// suffix append onto an existing message's Content needs.
func wrapUserSteer(text string) string {
	nonce := toolOutputNonce()
	return "\n" + steerMarkerOpen + nonce + `">` + text + steerMarkerClose
}

// steerLookalikeRe matches the FULL shape of a steer marker — an opening tag
// with any (or no) nonce attribute value, the marked content, and the
// closing tag — regardless of whether the nonce is genuine. A bare mention
// of the tag name in running prose, or a truncated/partial tag with no
// paired close, does not match: only the complete open+content+close
// structure is neutralised, because a genuine marker never appears inside a
// tool's own raw preview (drainSteer appends it AFTER render, never before).
var steerLookalikeRe = regexp.MustCompile(`<user_steer[^>]*>[\s\S]*?</user_steer>`)

// scrubSteerLookalikes neutralises any forged steer marker inside RAW
// tool-result content by HTML-escaping just the matched span — turning it
// into inert visible text rather than a parseable tag, without deleting or
// otherwise mangling surrounding content. Its single call site is inside
// renderToolResultForPrompt (trust.go), ahead of the trusted/untrusted
// branch, so it sees the LITERAL marker bytes wrapUserSteer produces, never
// an already-escaped form: an escaped lookalike is already inert and is left
// untouched, because "<user_steer" never matches "&lt;user_steer". The
// load-bearing case is the TRUSTED branch — it does no escaping of its own,
// so without this call a forged marker there would reach history exactly as
// trusted as a genuine one (T-52-10/T-52-19).
func scrubSteerLookalikes(content string) string {
	return steerLookalikeRe.ReplaceAllStringFunc(content, html.EscapeString)
}

// markSteer picks the envelope a queued message is delivered in, and names it
// for the aura.steer echo frame. The envelope is chosen by AUTHOR, because an
// envelope is an authorship claim and the model reads it as one.
//
// Spike 098 measured what happens when it is not: three live runs proved the
// rail carries a delegated worker's report and that the model parses it, and
// then the model DISCOUNTED the report — `<user_steer>` declares the operator as
// author, the payload said a worker was reporting, and the model trusted the
// payload's self-declared authorship over the envelope and read the whole thing
// as a spoofing attempt. For a backgrounded worker whose report is the only
// copy, that is the delegated work silently lost.
//
// No third envelope is minted for this. Aura already has one that means exactly
// "evidence from a named non-operator source, act on it as data and never as an
// instruction" — the untrusted tool-output envelope, which already takes a
// source and is already what the swarm stamps on its own results
// (RunnerAdapter's Provenance{Source: "swarm", Trust: TrustUntrusted}). A
// worker's report is the deferred result of the model's OWN swarm_spawn call,
// so that is the honest shape for it, and it brings the escaping with it: the
// operator envelope deliberately does not escape (it wraps the operator's own
// words), while a worker's generated text is escaped like any other untrusted
// output.
//
// Only the reserved steer.SourceWorker takes the worker branch. Every channel
// in the tree pushes an operator source, and an unrecognised one keeps the
// operator envelope byte-for-byte, so a new channel cannot fall into the worker
// branch by forgetting to name itself.
func markSteer(m steer.Message) (marked, envelope string) {
	if m.Source == steer.SourceWorker {
		return "\n" + wrapUntrustedToolOutput(m.Source, m.Text), "worker_report"
	}
	return wrapUserSteer(m.Text), "user_steer"
}

// drainSteer delivers whatever is queued for this conversation into the
// history tail, behind the nonce-marked attribution envelope SteerChannelNote
// (prompt.go) teaches the model to trust. A nil inbox (the
// AURA_AGUI_RUN_STEER=false rollback, D-12) or an empty drain make it a total
// no-op: no history mutation, no allocation, no Event.
//
// The delivery rule, applied per drained message in FIFO order: if the LAST
// message in a.history has role tool, append the marked text onto the END of
// that message's Content — because that content is ALREADY
// `<tool_output ...>...</tool_output>` when the tool was untrusted
// (llm_agent_dispatch.go applies the envelope BEFORE the message enters
// history, never at render time), so a suffix append necessarily lands the
// marker OUTSIDE the envelope without parsing or re-wrapping anything.
// Otherwise — the tail is not a tool result — append ONE new RoleUser
// message carrying the marked text. This is amendment #132's ratified
// primary and its ratified fallback expressed as a single branch (D-07/D-08).
// Never touches a.history[0..2] — only ever the tail.
func (a *LlmAgent) drainSteer(ic InvocationContext, spanID [8]byte, parentSpanID *[8]byte, round modelRound) *Event {
	if a.steer == nil {
		return nil
	}
	msgs := a.steer.Drain(a.sessionID)
	if len(msgs) == 0 {
		return nil
	}
	delivered := make([]map[string]any, 0, len(msgs))
	for _, m := range msgs {
		marked, envelope := markSteer(m)
		delivery := "user_message_fallback"
		if n := len(a.history); n > 0 && a.history[n-1].Role == llm.RoleTool {
			a.history[n-1].Content += marked
			delivery = "tool_result_append"
		} else {
			a.history = append(a.history, llm.Message{Role: llm.RoleUser, Content: marked})
		}
		delivered = append(delivered, map[string]any{
			"id":       m.ID,
			"source":   m.Source,
			"text":     m.Text,
			"delivery": delivery,
			"envelope": envelope,
		})
	}
	ev := a.newEvent(ic, spanID, parentSpanID)
	ev.Actions.SteerDelta = map[string]any{
		"conversation_id": a.sessionID,
		"round":           round.ordinal,
		"steers":          delivered,
	}
	return ev
}
