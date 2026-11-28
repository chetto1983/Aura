package conversations

import (
	"html"
	"regexp"

	"github.com/chetto1983/aura/internal/llm"
)

// steerLookalikeRe matches the FULL shape of the operator's steer marker (agent.wrapUserSteer):
// an opening tag with any or no nonce, the marked content, and the closing tag. A bare mention of
// the tag name, or a tag with no paired close, does not match.
var steerLookalikeRe = regexp.MustCompile(`<user_steer[^>]*>[\s\S]*?</user_steer>`)

// ScrubSteerLookalikes HTML-escapes every complete steer marker in content, which leaves it
// visible but inert. An escaped one is left alone, so the scrub is idempotent. The agent runs
// it over every tool result before the model reads it, and the managed history runs it again
// over every stored turn the operator did not write: a genuine marker is never stored (the
// drain adds it to the in-memory history only), so any marker a reload finds was forged, in a
// row written before the stored turn kept the model's rendering (2026-11-28).
func ScrubSteerLookalikes(content string) string {
	return steerLookalikeRe.ReplaceAllStringFunc(content, html.EscapeString)
}

// modelContent is a stored turn's content as the managed history hands it to the model. A tool
// result, an assistant turn (a recorded report among them) and a compaction summary (the
// summarizer's words, cached as a user-role turn) are scrubbed; the operator's words are not.
// Only toMessages calls it: the display and the export read the turn as it was stored.
func modelContent(t Turn) string {
	if t.Role == llm.RoleTool || t.Role == llm.RoleAssistant || isCompaction(t) {
		return ScrubSteerLookalikes(t.Content)
	}
	return t.Content
}
