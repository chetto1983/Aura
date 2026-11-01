package agui

import (
	"strings"

	"github.com/chetto1983/aura/internal/agent"
)

// server_display_watch.go shows a mail watch's conversation record to a person. The record reaches
// the model framed as untrusted data (spec 2026-10-31-mail-calendar-watch-design.md §5.7); the
// cockpit shows the same lines without the frame or its escaping. They are attacker text, and the
// cockpit renders an assistant turn as Markdown, images and links included, so they go in a code
// fence: plain text, as §5.6 requires.

func displayWatchRecord(content string) string {
	return agent.ShowWatchRecord(content, plainTextBlock)
}

// plainTextBlock fences text with a fence longer than any it holds, so no line can close it.
func plainTextBlock(text string) string {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fence + "text\n" + text + "\n" + fence
}
