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
