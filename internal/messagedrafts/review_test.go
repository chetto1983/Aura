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
		Target:       Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"},
		OriginalArgs: json.RawMessage(`{"action":"send_email","to":["a@example.com","b@example.com"],"subject":"Hi","body":"Text"}`),
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
		Target:        Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"},
		OriginalArgs:  json.RawMessage(`{"action":"send_email","accountId":"acc-1","to":["a@example.com"],"body":"Text"}`),
		EffectiveArgs: json.RawMessage(`{"action":"send_email","accountId":"acc-2","to":["a@example.com"],"body":"Text"}`),
	}
	if got := email.SendingAccount(); got != "acc-2" {
		t.Fatalf("SendingAccount = %q, want the edited acc-2", got)
	}
	whatsapp := Draft{Target: Target{Recipe: "recipe:whatsapp", Tool: "send_message"}, OriginalArgs: json.RawMessage(`{"recipient":"1","message":"m","accountId":"x"}`)}
	if got := whatsapp.SendingAccount(); got != "" {
		t.Fatalf("a WhatsApp draft names an account: %q", got)
	}
}
