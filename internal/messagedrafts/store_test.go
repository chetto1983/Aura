package messagedrafts

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestArgsFingerprintBindsTargetAndEdits(t *testing.T) {
	email := Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}
	first := argsFingerprint(email, json.RawMessage(`{"to":["first@example.test"],"subject":"Hello"}`))
	second := argsFingerprint(email, json.RawMessage(`{"to":["second@example.test"],"subject":"Hello"}`))
	otherTool := argsFingerprint(Target{Recipe: "recipe:whatsapp", Tool: "send_message"}, json.RawMessage(`{"to":["first@example.test"],"subject":"Hello"}`))
	if len(first) != 64 || first == second || first == otherTool {
		t.Fatal("draft fingerprint is not bound to tool and effective arguments")
	}
}

func TestStoreRejectsInvalidDraftBeforeDatabase(t *testing.T) {
	store := NewStore(nil)
	valid := DraftInput{
		IdentityID: "11111111-1111-4111-8111-111111111111", ConversationID: "thread-1", ToolCallID: "call-1",
		Target:             Target{Recipe: "recipe:whatsapp", Tool: "send_message"},
		RegisteredToolName: "whatsapp__send_message",
		OriginalArgs:       json.RawMessage(`{"recipient":"12345","message":"hello"}`), ExpiresAt: time.Now().Add(time.Hour),
	}
	cases := []DraftInput{
		{IdentityID: valid.IdentityID, Target: valid.Target, OriginalArgs: valid.OriginalArgs, ExpiresAt: valid.ExpiresAt},
		{IdentityID: "invalid", ConversationID: valid.ConversationID, ToolCallID: valid.ToolCallID, Target: valid.Target, OriginalArgs: valid.OriginalArgs, ExpiresAt: valid.ExpiresAt},
		{IdentityID: valid.IdentityID, ConversationID: valid.ConversationID, ToolCallID: valid.ToolCallID, Target: valid.Target, OriginalArgs: valid.OriginalArgs, ExpiresAt: time.Now().Add(-time.Minute)},
		{IdentityID: valid.IdentityID, ConversationID: valid.ConversationID, ToolCallID: valid.ToolCallID, Target: valid.Target, RegisteredToolName: "malformed:name", OriginalArgs: valid.OriginalArgs, ExpiresAt: valid.ExpiresAt},
		{IdentityID: valid.IdentityID, ConversationID: valid.ConversationID, ToolCallID: valid.ToolCallID, Target: Target{Recipe: "custom", Tool: "send_message"}, OriginalArgs: valid.OriginalArgs, ExpiresAt: valid.ExpiresAt},
	}
	for _, input := range cases {
		if _, err := store.Create(context.Background(), input); err == nil {
			t.Fatal("invalid draft reached the database")
		}
	}
	if _, err := store.MarkOutcome(context.Background(), valid.IdentityID, valid.ToolCallID, StatusPending, ""); err == nil {
		t.Fatal("pending is not a transport outcome")
	}
	if _, err := store.MarkOutcome(context.Background(), valid.IdentityID, valid.ToolCallID, StatusSent, "private content"); err == nil {
		t.Fatal("unsafe outcome code accepted")
	}
	if _, err := store.Get(context.Background(), valid.IdentityID, "not-a-uuid"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("malformed draft ID was not treated as unavailable")
	}
	if _, err := store.ListPending(context.Background(), "invalid-owner", valid.ConversationID); !errors.Is(err, ErrUnavailable) {
		t.Fatal("invalid owner listed drafts")
	}
	if _, err := store.ClaimSend(context.Background(), valid.IdentityID, "not-a-uuid", nil); !errors.Is(err, ErrUnavailable) {
		t.Fatal("invalid draft ID reached the send claim")
	}
	if _, err := store.Decline(context.Background(), valid.IdentityID, "not-a-uuid"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("invalid draft ID reached decline")
	}
	if _, err := store.Reconcile(context.Background(), valid.IdentityID, "not-a-uuid"); !errors.Is(err, ErrUnavailable) {
		t.Fatal("invalid draft ID reached recovery")
	}
}

func TestRegisteredToolNameAcceptsHashedBridgeName(t *testing.T) {
	if !validRegisteredToolName("namespace_that_is_very_long_and_needs_truncate__c_0123456789ab") {
		t.Fatal("a hash-suffixed bridge name was rejected")
	}
	for _, name := range []string{"short", "server__tool!", strings.Repeat("a", 55) + "__tool_name"} {
		if validRegisteredToolName(name) {
			t.Fatalf("invalid mounted tool name accepted: %q", name)
		}
	}
}

func TestClaimMatchesOnlyExactOwnerMountedToolAndArguments(t *testing.T) {
	target := Target{Recipe: "recipe:calendar", Tool: "calendar", Action: "send_email"}
	args := json.RawMessage(`{"action":"send_email","to":["first@example.test"],"subject":"Hello","body":"Reviewed"}`)
	canonical, err := MergeApprovedArgs(target, args, nil)
	if err != nil {
		t.Fatal(err)
	}
	claim := Draft{
		IdentityID: "owner", RegisteredToolName: "pim__calendar", Target: target,
		Status: StatusDispatching, EffectiveArgs: json.RawMessage(`{"body":"Reviewed","subject":"Hello","to":["first@example.test"],"action":"send_email"}`),
		EffectiveFingerprint: argsFingerprint(target, canonical),
	}
	if !claim.MatchesDispatch("owner", "pim__calendar", target, args) {
		t.Fatal("claimed draft rejected identical effective arguments after JSONB reordering")
	}
	if claim.MatchesDispatch("other", "pim__calendar", target, args) ||
		claim.MatchesDispatch("owner", "other__calendar", target, args) ||
		claim.MatchesDispatch("owner", "pim__calendar", target, json.RawMessage(`{"action":"send_email","to":["second@example.test"],"subject":"Hello","body":"Reviewed"}`)) {
		t.Fatal("claim authorized changed owner, tool, or destination")
	}
}
