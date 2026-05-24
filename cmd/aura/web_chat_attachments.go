package main

import (
	"strings"

	"github.com/aura/aura/internal/chat"
)

func webUserInputText(msg chat.InboundMessage) string {
	text := strings.TrimSpace(msg.Text)
	if len(msg.Attachments) == 0 {
		return text
	}
	var b strings.Builder
	if text != "" {
		b.WriteString(text)
		b.WriteString("\n\n")
	}
	b.WriteString("Attached sources:")
	for _, attachment := range msg.Attachments {
		sourceID := strings.TrimSpace(attachment.SourceID)
		if sourceID == "" {
			continue
		}
		b.WriteString("\n- source_id: ")
		b.WriteString(sourceID)
		if filename := strings.TrimSpace(attachment.Filename); filename != "" {
			b.WriteString(" filename: ")
			b.WriteString(filename)
		}
		if mimeType := strings.TrimSpace(attachment.MimeType); mimeType != "" {
			b.WriteString(" mime_type: ")
			b.WriteString(mimeType)
		}
	}
	if b.Len() == 0 {
		return text
	}
	return b.String()
}
