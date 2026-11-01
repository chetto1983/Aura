package agui

import (
	"regexp"
	"strings"

	"github.com/chetto1983/aura/internal/agent"
)

// server_display_watch.go shows a mail watch's conversation record to a person. The record reaches
// the model framed as untrusted data (spec 2026-10-31-mail-calendar-watch-design.md §5.7); the
// cockpit shows the same lines without the frame or its escaping. They are attacker text, and the
// cockpit renders an assistant turn as Markdown, images and links included, so they go in a code
// fence: plain text, as §5.6 requires.

// watchRecordFrame matches the envelope agent.FrameUntrustedPromptContent writes. The content is
// escaped, so a forged closing tag cannot end the match early.
var watchRecordFrame = regexp.MustCompile(`(?s)<tool_output source="` + regexp.QuoteMeta(agent.WatchRecordSource) +
	`" trust="untrusted" nonce="[0-9a-f]+">\n(.*?)\n</tool_output>`)

func displayWatchRecord(content string) string {
	return watchRecordFrame.ReplaceAllStringFunc(content, func(frame string) string {
		quoted := watchRecordFrame.FindStringSubmatch(frame)[1]
		return plainTextBlock(agent.UnescapePromptText(quoted))
	})
}

// plainTextBlock fences text with a fence longer than any it holds, so no line can close it.
func plainTextBlock(text string) string {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fence + "text\n" + text + "\n" + fence
}
