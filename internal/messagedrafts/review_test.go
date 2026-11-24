package messagedrafts

import (
	"encoding/json"
	"testing"
)

// The review shows what will be sent: the operator's edit when there is one.
func TestReviewReadsTheEditedArgumentsFirst(t *testing.T) {
	t.Parallel()
	whatsapp := Draft{
		Target:        Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		OriginalArgs:  json.RawMessage(`{"recipient":"10000000000","message":"Original"}`),
		EffectiveArgs: json.RawMessage(`{"recipient":"10000000000","message":"Edited"}`),
	}
	if r, s, x := whatsapp.Review(); r != "10000000000" || s != "" || x != "Edited" {
		t.Fatalf("whatsapp review = %q %q %q", r, s, x)
	}
	whatsapp.EffectiveArgs = nil
	if _, _, x := whatsapp.Review(); x != "Original" {
		t.Fatalf("unedited review text = %q", x)
	}
	email := Draft{
		Target:       emailSend,
		OriginalArgs: json.RawMessage(`{"to":["a@example.com","b@example.com"],"subject":"Hi","body":"Text"}`),
	}
	if r, s, x := email.Review(); r != "a@example.com, b@example.com" || s != "Hi" || x != "Text" {
		t.Fatalf("email review = %q %q %q", r, s, x)
	}
}

// Measured 2026-11-12: the PIM picks the sender itself when no accountId is given, so the review
// names the account whenever the arguments carry one.
func TestReviewNamesTheSendingAccount(t *testing.T) {
	t.Parallel()
	email := Draft{
		Target:        emailSend,
		OriginalArgs:  json.RawMessage(`{"accountId":"acc-1","to":["a@example.com"],"body":"Text"}`),
		EffectiveArgs: json.RawMessage(`{"accountId":"acc-2","to":["a@example.com"],"body":"Text"}`),
	}
	if got := email.SendingAccount(); got != "acc-2" {
		t.Fatalf("SendingAccount = %q, want the edited acc-2", got)
	}
	whatsapp := Draft{Target: Target{Recipe: "recipe:whatsapp", Tool: "send_message"}, OriginalArgs: json.RawMessage(`{"recipient":"1","message":"m","accountId":"x"}`)}
	if got := whatsapp.SendingAccount(); got != "" {
		t.Fatalf("a WhatsApp draft names an account: %q", got)
	}
}

// Completeness is keyed on the send_email tool: a resolved draft kept in the multiplexed shape
// from before the split is history and is never offered for an inline send.
func TestInlineReviewIsCompleteOnlyForTheSendEmailTool(t *testing.T) {
	t.Parallel()
	email := Draft{Target: emailSend, OriginalArgs: json.RawMessage(`{"to":["a@example.com"],"subject":"Hi","body":"Text","bodyFormat":"text"}`)}
	if !email.InlineReview().Complete {
		t.Fatal("a plain send_email draft is not complete")
	}
	retired := Draft{
		Target:       Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"},
		OriginalArgs: json.RawMessage(`{"to":["a@example.com"],"subject":"Hi","body":"Text","bodyFormat":"text"}`),
	}
	if retired.InlineReview().Complete {
		t.Fatal("a draft in the retired multiplexed shape is complete")
	}
}

// A draft resolved before the PIM split keeps its "action" key, and jsonb orders keys shortest first,
// so a strict decode stopped after to and body: its history lost the subject and the sender.
func TestLegacyDraftHistoryShowsSubjectAndSender(t *testing.T) {
	t.Parallel()
	legacy := Draft{
		Target:        LegacyEmailTarget(),
		Status:        StatusSent,
		SenderAddress: "me@example.com",
		OriginalArgs: json.RawMessage(`{"cc": ["c@example.com"], "to": ["a@example.com"], "body": "Text", ` +
			`"action": "send_email", "subject": "Hi", "accountId": "acc-1", "bodyFormat": "text"}`),
	}
	if _, err := legacy.reviewed(); err != nil {
		t.Fatalf("a legacy draft's arguments do not decode: %v", err)
	}
	content := legacy.InlineReview()
	if content.Recipient != "a@example.com" || content.CC != "c@example.com" || content.Subject != "Hi" ||
		content.Text != "Text" || content.BodyFormat != "text" {
		t.Fatalf("legacy review = %+v", content)
	}
	if got := legacy.SendingAccount(); got != "acc-1" {
		t.Fatalf("SendingAccount = %q, want acc-1", got)
	}
	if name, address := legacy.Sender(); name == "" || address != "me@example.com" {
		t.Fatalf("Sender = %q %q", name, address)
	}
}

// Only "action" is forgiven, and only on the legacy target: any other unknown key still fails.
func TestLegacyDraftStillRefusesAnUnknownKey(t *testing.T) {
	t.Parallel()
	legacy := Draft{
		Target:       LegacyEmailTarget(),
		OriginalArgs: json.RawMessage(`{"to": ["a@example.com"], "action": "send_email", "subject": "Hi", "replyTo": "x"}`),
	}
	if _, err := legacy.reviewed(); err == nil {
		t.Fatal("a legacy draft with an unknown key decoded")
	}
	current := Draft{Target: emailSend, OriginalArgs: json.RawMessage(`{"to": ["a@example.com"], "action": "send_email"}`)}
	if _, err := current.reviewed(); err == nil {
		t.Fatal("a send_email draft carrying action decoded")
	}
	broken := Draft{Target: LegacyEmailTarget(), OriginalArgs: json.RawMessage(`[1]`)}
	if _, err := broken.reviewed(); err == nil {
		t.Fatal("a legacy draft whose arguments are not an object decoded")
	}
}
