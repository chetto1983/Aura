package messagedrafts

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestMergeApprovedArgsEmail(t *testing.T) {
	original := json.RawMessage(`{"accountId":"account-1","to":["first@example.test"],"cc":["copy@example.test"],"subject":"Original","body":"Original body","bodyFormat":"text","attachments":[{"attachmentId":"asset-1"}]}`)
	overrides := json.RawMessage(`{"to":["second@example.test"],"cc":[],"subject":"Revised","body":"Ciao 👋"}`)
	effective, err := MergeApprovedArgs(emailSend, original, overrides)
	if err != nil {
		t.Fatal("valid email edits rejected")
	}
	var got map[string]json.RawMessage
	if json.Unmarshal(effective, &got) != nil {
		t.Fatal("effective arguments invalid")
	}
	for field, want := range map[string]string{
		"accountId": "\"account-1\"", "to": `["second@example.test"]`, "cc": `[]`,
		"subject": "\"Revised\"", "body": "\"Ciao 👋\"",
		"bodyFormat": "\"text\"", "attachments": `[{"attachmentId":"asset-1"}]`,
	} {
		if string(got[field]) != want {
			t.Fatalf("effective %s differs from approved or fixed argument", field)
		}
	}
}

func TestMergeApprovedArgsMultipartAndWhatsApp(t *testing.T) {
	cases := []struct {
		name      string
		target    Target
		original  json.RawMessage
		overrides json.RawMessage
		field     string
		want      string
	}{
		{
			name: "multipart text", target: emailSend,
			original:  json.RawMessage(`{"to":["first@example.test"],"subject":"Hello","bodyFormat":"multipart","textBody":"old","htmlBody":"<p>old</p>"}`),
			overrides: json.RawMessage(`{"textBody":"new","htmlBody":"<p>new</p>"}`), field: "textBody", want: "new",
		},
		{
			name: "whatsapp quoted reply", target: Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
			original:  json.RawMessage(`{"recipient":"12345@s.whatsapp.net","message":"old","quoted_message_id":"msg-1","quoted_sender_jid":"23456@s.whatsapp.net","quoted_content":"prior"}`),
			overrides: json.RawMessage(`{"recipient":"98765@g.us","message":"Ciao 👋"}`), field: "message", want: "Ciao 👋",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			effective, err := MergeApprovedArgs(tc.target, tc.original, tc.overrides)
			if err != nil {
				t.Fatal("valid edited draft rejected")
			}
			var got map[string]any
			if json.Unmarshal(effective, &got) != nil || got[tc.field] != tc.want {
				t.Fatal("edited value is not the effective argument")
			}
		})
	}
}

// The refusal is what a model reads before it retries, so it names the fields it must change and
// never echoes a value. Measured 2026-11-23 on the lab VM: a model that sent unknown keys and no
// subject heard only "message review required" and retried the same call five times
// (docs/superpowers/verification/2026-11-23-mailcraft-gate1-pim-as-is.md).
func TestMergeApprovedArgsNamesTheFieldsToChange(t *testing.T) {
	cases := []struct {
		name     string
		original string
		want     []string
	}{
		{"unknown keys, all of them",
			`{"to":["first@example.test"],"subject":"Hello","body":"Hi","topics":"secret-topic","toDate":null}`,
			[]string{`"toDate"`, `"topics"`, "subject"}},
		{"missing subject", `{"to":["first@example.test"],"body":"Hi"}`, []string{`"subject"`}},
		{"bad recipient", `{"to":["secret-not-an-address"],"subject":"Hello"}`, []string{`"to"`}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := MergeApprovedArgs(emailSend, json.RawMessage(tc.original), nil)
			if !errors.Is(err, ErrInvalidArgs) {
				t.Fatalf("err = %v, want ErrInvalidArgs", err)
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("err = %q, want it to name %s", err, want)
				}
			}
			if strings.Contains(err.Error(), "secret") {
				t.Errorf("err = %q echoes an argument value", err)
			}
		})
	}
}

func TestMergeApprovedArgsRejectsUnapprovedOrInvalidFields(t *testing.T) {
	whatsapp := Target{Recipe: "recipe:whatsapp", Tool: "send_message"}
	baseEmail := json.RawMessage(`{"to":["first@example.test"],"subject":"Hello","body":"Hi"}`)
	baseWhatsApp := json.RawMessage(`{"recipient":"12345","message":"Hi"}`)
	cases := []struct {
		name      string
		target    Target
		original  json.RawMessage
		overrides json.RawMessage
	}{
		{"bcc unsupported by fork", emailSend, baseEmail, json.RawMessage(`{"bcc":["hidden@example.test"]}`)},
		{"sender cannot change", emailSend, baseEmail, json.RawMessage(`{"accountId":"other"}`)},
		{"an override cannot add action", emailSend, baseEmail, json.RawMessage(`{"action":"delete_email"}`)},
		{"empty to", emailSend, baseEmail, json.RawMessage(`{"to":[]}`)},
		{"empty subject", emailSend, baseEmail, json.RawMessage(`{"subject":""}`)},
		{"multipart body alias", emailSend, json.RawMessage(`{"to":["first@example.test"],"subject":"Hello","bodyFormat":"multipart","textBody":"a","htmlBody":"b"}`), json.RawMessage(`{"body":"ignored"}`)},
		{"unexpected original field", emailSend, json.RawMessage(`{"to":["first@example.test"],"subject":"Hello","body":"Hi","bcc":["hidden@example.test"]}`), nil},
		{"untrusted recipe", Target{Recipe: "custom", Tool: "send_email"}, baseEmail, nil},
		{"the retired multiplexed target", Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}, json.RawMessage(`{"action":"send_email","to":["first@example.test"],"subject":"Hello","body":"Hi"}`), nil},
		{"quote context cannot change", whatsapp, baseWhatsApp, json.RawMessage(`{"quoted_message_id":"other"}`)},
		{"empty recipient", whatsapp, baseWhatsApp, json.RawMessage(`{"recipient":""}`)},
		{"empty message", whatsapp, baseWhatsApp, json.RawMessage(`{"message":""}`)},
		{"duplicate override key", whatsapp, baseWhatsApp, json.RawMessage(`{"message":"first","message":"second"}`)},
		{"duplicate nested key", emailSend, json.RawMessage(`{"to":["first@example.test"],"subject":"Hello","attachments":[{"attachmentId":"first","attachmentId":"second"}]}`), nil},
		{"malformed original", whatsapp, json.RawMessage(`{"recipient":`), nil},
		{"excessive JSON depth", whatsapp, json.RawMessage(`{"recipient":"12345","message":"Hi","quoted_content":` + strings.Repeat("[", 40) + `"x"` + strings.Repeat("]", 40) + `}`), nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := MergeApprovedArgs(tc.target, tc.original, tc.overrides); err == nil {
				t.Fatal("unsafe draft arguments accepted")
			}
		})
	}
}

// The PIM's send_email tool takes no action discriminator: one sent along is a field the send
// does not accept, and the refusal names it so the model drops it.
func TestEmailArgsWithActionAreRefused(t *testing.T) {
	_, err := MergeApprovedArgs(emailSend, json.RawMessage(`{"action":"send_email","to":["first@example.test"],"subject":"Hello","body":"Hi"}`), nil)
	if !errors.Is(err, ErrInvalidArgs) || !strings.Contains(err.Error(), `"action"`) {
		t.Fatalf("err = %v, want ErrInvalidArgs naming \"action\"", err)
	}
}

// Operator decision 2026-11-23 (spec §5.3): Aura does not require the sending account before
// review; send_email's own schema asks the model for it.
func TestAccountIdStaysOptionalBeforeReview(t *testing.T) {
	for name, original := range map[string]string{
		"no account":    `{"to":["first@example.test"],"subject":"Hello","body":"Hi"}`,
		"named account": `{"accountId":"t__personale","to":["first@example.test"],"subject":"Hello","body":"Hi"}`,
	} {
		if _, err := MergeApprovedArgs(emailSend, json.RawMessage(original), nil); err != nil {
			t.Errorf("%s: err = %v, want the draft accepted", name, err)
		}
	}
}
