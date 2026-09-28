package messagedrafts

import (
	"context"
	"encoding/json"
	"errors"
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
}

func TestRegisteredToolNameAcceptsHashedBridgeName(t *testing.T) {
	if !validRegisteredToolName("namespace_that_is_very_long_and_needs_truncate__c_0123456789ab") {
		t.Fatal("a hash-suffixed bridge name was rejected")
	}
}
