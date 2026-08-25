// Concern-split out of llm_agent.go (mirroring llm_agent_finalize.go's own
// precedent): llm_agent.go is at its no-god-class headroom, so both steer
// drain points, the attribution marker, and the lookalike scrub live here.
package agent

import (
	"html"
	"regexp"

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
	// TODO(RED): stub — always a no-op. GREEN drains a.steer and mutates history.
	_ = ic
	_ = spanID
	_ = parentSpanID
	_ = round
	return nil
}
