package cron

import (
	"strings"
	"testing"
)

// Measured 2026-11-12 on the lab VM: a job's summary reached the operator's mailbox as raw
// markdown, headings, bold and a table included. The email route sends the text with its GFM
// rendering, and the job's output cannot inject markup.
func TestTheEmailRouteSendsMarkdownAsHTML(t *testing.T) {
	t.Setenv("AURA_SCHEDULER_NOTIFY_RECIPIENT", "")
	fss := &fakeSelfSend{}
	text := "## Riepilogo\n\n**Totale: 5**\n\n| Task | Prossima |\n|---|---|\n| posta | 07:56 |\n\n<script>alert(1)</script>\n"
	if err := NewNotifier(fss, ownAddressBook()).Notify(ownerCtx(), RouteEmail, text); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	got := sentArgs(t, fss)
	if got["bodyFormat"] != "multipart" || got["textBody"] != text {
		t.Fatalf("args = %v", got)
	}
	if _, plain := got["body"]; plain {
		t.Fatal("a multipart send carries textBody and htmlBody, not body")
	}
	html, _ := got["htmlBody"].(string)
	for _, want := range []string{"<h2>Riepilogo</h2>", "<strong>Totale: 5</strong>", "<table>", "<td>posta</td>"} {
		if !strings.Contains(html, want) {
			t.Errorf("htmlBody lacks %q:\n%s", want, html)
		}
	}
	if strings.Contains(html, "<script>") {
		t.Fatalf("the job's raw HTML reached the mail:\n%s", html)
	}
}

// The PIM refuses a multipart send with an empty body, so text with nothing to render stays text.
func TestAnEmptyTextIsSentAsPlainText(t *testing.T) {
	args := emailArgs("owner@example.com", "  ")
	if args["bodyFormat"] != "text" || args["body"] != "  " {
		t.Fatalf("args = %v", args)
	}
}
