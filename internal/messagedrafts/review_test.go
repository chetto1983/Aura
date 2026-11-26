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

// A draft resolved before the PIM split keeps its "action" key, which the strict decode records as
// unknown and decodes past: its history still shows every field, the subject and the sender too.
func TestLegacyDraftHistoryShowsSubjectAndSender(t *testing.T) {
	t.Parallel()
	legacy := Draft{
		Target:        LegacyEmailTarget(),
		Status:        StatusSent,
		SenderAddress: "me@example.com",
		OriginalArgs: json.RawMessage(`{"cc": ["c@example.com"], "to": ["a@example.com"], "body": "Text", ` +
			`"action": "send_email", "subject": "Hi", "accountId": "acc-1", "bodyFormat": "text"}`),
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

// A send_email draft carrying a key the review cannot show, "action" included, is never offered
// for an inline send: the operator would approve fields they were not shown.
func TestSendEmailDraftWithAnUnknownKeyIsNotComplete(t *testing.T) {
	t.Parallel()
	for _, args := range []string{
		`{"to": ["a@example.com"], "subject": "Hi", "body": "Text", "bodyFormat": "text", "action": "send_email"}`,
		`{"to": ["a@example.com"], "subject": "Hi", "body": "Text", "bodyFormat": "text", "replyTo": "x"}`,
	} {
		if (Draft{Target: emailSend, OriginalArgs: json.RawMessage(args)}).InlineReview().Complete {
			t.Errorf("%s is complete", args)
		}
	}
}

// The history does not validate: a stored send the new send_email would refuse still shows its
// account and subject, because the strict decode records an unknown key and goes on decoding the
// rest of the object.
func TestLegacyHistoryShowsAccountAndSubjectPastAnUnknownKey(t *testing.T) {
	t.Parallel()
	legacy := Draft{
		Target: LegacyEmailTarget(),
		OriginalArgs: json.RawMessage(`{"to": ["a@example.com"], "action": "send_email", "replyTo": "x", ` +
			`"subject": "Hi", "accountId": "acc-1"}`),
	}
	if _, err := LegacyEmailArgs(legacy.OriginalArgs); err == nil {
		t.Fatal("the fixture is one the new send accepts")
	}
	if got := legacy.SendingAccount(); got != "acc-1" {
		t.Fatalf("SendingAccount = %q, want acc-1", got)
	}
	if got := legacy.InlineReview().Subject; got != "Hi" {
		t.Fatalf("Subject = %q, want Hi", got)
	}
}

// A pre-split send becomes the bytes Create would store for the same send_email call, whatever key
// order jsonb handed it back in, so the fingerprint the dispatch recomputes matches.
func TestLegacyEmailArgsAreCreatesCanonicalSendEmailArgs(t *testing.T) {
	t.Parallel()
	got, err := LegacyEmailArgs(json.RawMessage(`{"to": ["a@example.com"], "body": "Text", ` +
		`"action": "send_email", "subject": "Hi", "accountId": "acc-1", "bodyFormat": "text"}`))
	if err != nil {
		t.Fatal(err)
	}
	want, err := MergeApprovedArgs(emailTarget,
		json.RawMessage(`{"accountId":"acc-1","bodyFormat":"text","subject":"Hi","body":"Text","to":["a@example.com"]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("LegacyEmailArgs = %s, want %s", got, want)
	}
	for _, refused := range []string{
		`[1]`,
		`{"to": ["a@example.com"], "action": "send_email", "subject": "Hi", "accountId": "acc-1", "replyTo": "x"}`,
		`{"to": "a@example.com", "action": "send_email", "subject": "Hi", "accountId": "acc-1"}`,
	} {
		if args, err := LegacyEmailArgs(json.RawMessage(refused)); err == nil {
			t.Errorf("LegacyEmailArgs(%s) = %s, want an error", refused, args)
		}
	}
}
