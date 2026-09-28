package messagedrafts

import (
	"encoding/json"
	"testing"

	"github.com/chetto1983/aura/internal/agent/tools"
)

func TestMessageDraftPauseContextRequiresTypeAndID(t *testing.T) {
	if id, ok := PauseDraftID(json.RawMessage(`{"type":"message_draft","draft_id":"draft-1"}`)); !ok || id != "draft-1" {
		t.Fatal("valid review pause lost its draft ID")
	}
	for _, raw := range []json.RawMessage{
		nil, json.RawMessage(`not-json`), json.RawMessage(`{"type":"other","draft_id":"draft-1"}`),
		json.RawMessage(`{"type":"message_draft"}`),
	} {
		if _, ok := PauseDraftID(raw); ok {
			t.Fatal("malformed or foreign pause was treated as a draft")
		}
	}
	if !IsReviewPause(json.RawMessage(`{"type":"message_draft"}`)) ||
		IsReviewPause(json.RawMessage(`{"type":"other"}`)) || IsReviewPause(json.RawMessage(`not-json`)) {
		t.Fatal("generic approval queue did not recognize review pause type")
	}
}

func TestMessageDraftTrustedOutboundTargetUsesRawBridgeIdentity(t *testing.T) {
	longName := "namespace_that_is_very_long_and_needs_truncate__c_0123456789ab"
	cases := []struct {
		name string
		spec tools.Spec
		args json.RawMessage
		want Target
		ok   bool
	}{
		{"hashed calendar", tools.Spec{Name: longName, TrustedRecipeSource: "recipe:calendar", TrustedRecipeTool: "calendar"}, json.RawMessage(`{"action":"send_email"}`), Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}, true},
		{"whatsapp", tools.Spec{Name: longName, TrustedRecipeSource: "recipe:whatsapp", TrustedRecipeTool: "send_message"}, json.RawMessage(`{"recipient":"123","message":"hello"}`), Target{Recipe: "recipe:whatsapp", Tool: "send_message"}, true},
		{"calendar read", tools.Spec{Name: longName, TrustedRecipeSource: "recipe:calendar", TrustedRecipeTool: "calendar"}, json.RawMessage(`{"action":"get_emails"}`), Target{}, false},
		{"invalid calendar JSON", tools.Spec{Name: longName, TrustedRecipeSource: "recipe:calendar", TrustedRecipeTool: "calendar"}, json.RawMessage(`{`), Target{}, false},
		{"spoofed source", tools.Spec{Name: longName, TrustedRecipeSource: "custom", TrustedRecipeTool: "calendar"}, json.RawMessage(`{"action":"send_email"}`), Target{}, false},
		{"wrong raw tool", tools.Spec{Name: longName, TrustedRecipeSource: "recipe:whatsapp", TrustedRecipeTool: "send_file"}, json.RawMessage(`{}`), Target{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := TrustedOutboundTarget(tc.spec, tc.args)
			if got != tc.want || ok != tc.ok {
				t.Fatalf("target = (%+v, %v), want (%+v, %v)", got, ok, tc.want, tc.ok)
			}
		})
	}
}
