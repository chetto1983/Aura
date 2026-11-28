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
// it over every tool result before the model reads it, and a reload runs it again over every
// stored turn that is not the operator's own: a genuine marker is never stored (the drain adds
// it to the in-memory history only), so any marker a reload finds was forged, in a row written
// before the stored turn kept the model's rendering (2026-11-28).
func ScrubSteerLookalikes(content string) string {
	return steerLookalikeRe.ReplaceAllStringFunc(content, html.EscapeString)
}

// modelContent is a stored turn's content as a reload hands it on. A tool result or an
// assistant turn (a recorded report among them) is scrubbed; the operator's words are not.
func modelContent(t Turn) string {
	if t.Role == llm.RoleTool || t.Role == llm.RoleAssistant {
		return ScrubSteerLookalikes(t.Content)
	}
	return t.Content
}
