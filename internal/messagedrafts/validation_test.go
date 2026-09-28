package messagedrafts

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestMergeApprovedArgsEmail(t *testing.T) {
	original := json.RawMessage(`{"action":"send_email","accountId":"account-1","to":["first@example.test"],"cc":["copy@example.test"],"subject":"Original","body":"Original body","bodyFormat":"text","attachments":[{"attachmentId":"asset-1"}]}`)
	overrides := json.RawMessage(`{"to":["second@example.test"],"cc":[],"subject":"Revised","body":"Ciao 👋"}`)
	effective, err := MergeApprovedArgs(Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}, original, overrides)
	if err != nil {
		t.Fatal("valid email edits rejected")
	}
	var got map[string]json.RawMessage
	if json.Unmarshal(effective, &got) != nil {
		t.Fatal("effective arguments invalid")
	}
	for field, want := range map[string]string{
		"action": "\"send_email\"", "accountId": "\"account-1\"",
		"to": `["second@example.test"]`, "cc": `[]`,
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
			name: "multipart text", target: Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"},
			original:  json.RawMessage(`{"action":"send_email","to":["first@example.test"],"subject":"Hello","bodyFormat":"multipart","textBody":"old","htmlBody":"<p>old</p>"}`),
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

func TestMergeApprovedArgsRejectsUnapprovedOrInvalidFields(t *testing.T) {
	email := Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}
	whatsapp := Target{Recipe: "recipe:whatsapp", Tool: "send_message"}
	baseEmail := json.RawMessage(`{"action":"send_email","to":["first@example.test"],"subject":"Hello","body":"Hi"}`)
	baseWhatsApp := json.RawMessage(`{"recipient":"12345","message":"Hi"}`)
	cases := []struct {
		name      string
		target    Target
		original  json.RawMessage
		overrides json.RawMessage
	}{
		{"bcc unsupported by fork", email, baseEmail, json.RawMessage(`{"bcc":["hidden@example.test"]}`)},
		{"sender cannot change", email, baseEmail, json.RawMessage(`{"accountId":"other"}`)},
		{"action cannot change", email, baseEmail, json.RawMessage(`{"action":"delete_email"}`)},
		{"empty to", email, baseEmail, json.RawMessage(`{"to":[]}`)},
		{"empty subject", email, baseEmail, json.RawMessage(`{"subject":""}`)},
		{"multipart body alias", email, json.RawMessage(`{"action":"send_email","to":["first@example.test"],"subject":"Hello","bodyFormat":"multipart","textBody":"a","htmlBody":"b"}`), json.RawMessage(`{"body":"ignored"}`)},
		{"unexpected original field", email, json.RawMessage(`{"action":"send_email","to":["first@example.test"],"subject":"Hello","body":"Hi","bcc":["hidden@example.test"]}`), nil},
		{"untrusted recipe", Target{Recipe: "custom", Tool: "calendar", Action: "send_email"}, baseEmail, nil},
		{"wrong original action", email, json.RawMessage(`{"action":"delete_email","to":["first@example.test"],"subject":"Hello","body":"Hi"}`), nil},
		{"quote context cannot change", whatsapp, baseWhatsApp, json.RawMessage(`{"quoted_message_id":"other"}`)},
		{"empty recipient", whatsapp, baseWhatsApp, json.RawMessage(`{"recipient":""}`)},
		{"empty message", whatsapp, baseWhatsApp, json.RawMessage(`{"message":""}`)},
		{"duplicate override key", whatsapp, baseWhatsApp, json.RawMessage(`{"message":"first","message":"second"}`)},
		{"duplicate nested key", email, json.RawMessage(`{"action":"send_email","to":["first@example.test"],"subject":"Hello","attachments":[{"attachmentId":"first","attachmentId":"second"}]}`), nil},
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
