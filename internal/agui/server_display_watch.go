package agui

import (
	"strings"

	"github.com/chetto1983/aura/internal/agent"
)

// server_display_watch.go shows a person the text an assistant turn records from someone else. It
// reaches the model framed as untrusted data; the cockpit shows it without the frame or its
// escaping. A mail watch's lines are attacker text (spec 2026-10-31-mail-calendar-watch-design.md
// §5.7), and the cockpit renders an assistant turn as Markdown, images and links included, so
// they go in a code fence: plain text, as §5.6 requires. A delegated worker's or a scheduled run's
// report renders as it did before its record was framed (2026-11-28).

func displayRecord(content string) string {
	return agent.ShowRecord(content, plainTextBlock)
}

// plainTextBlock fences text with a fence longer than any it holds, so no line can close it.
func plainTextBlock(text string) string {
	fence := "```"
	for strings.Contains(text, fence) {
		fence += "`"
	}
	return fence + "text\n" + text + "\n" + fence
}
