package cron

import (
	"bytes"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// notifyMarkdown renders a job's output for the operator's mailbox. goldmark was already in the
// module graph (internal/multimodal/speech_text.go); without WithUnsafe it omits raw HTML, so a
// job's output cannot inject markup.
var notifyMarkdown = goldmark.New(goldmark.WithExtensions(extension.GFM))

// emailArgs sends the text with its rendering: measured 2026-11-12, a job's markdown summary
// reached the mailbox as raw markers. The PIM's multipart format takes both bodies. The PIM
// picks the sending account itself; the notifier does not guess one.
func emailArgs(recipient, text string) map[string]any {
	args := map[string]any{"action": "send_email", "to": []string{recipient}, "subject": "Aura scheduled task"}
	// The PIM refuses a multipart send whose textBody or htmlBody is empty.
	var html bytes.Buffer
	if strings.TrimSpace(text) == "" || notifyMarkdown.Convert([]byte(text), &html) != nil || html.Len() == 0 {
		args["body"], args["bodyFormat"] = text, "text"
		return args
	}
	args["bodyFormat"], args["textBody"], args["htmlBody"] = "multipart", text, html.String()
	return args
}
