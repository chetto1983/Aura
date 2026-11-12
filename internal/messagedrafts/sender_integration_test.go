//go:build db_integration

package messagedrafts

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// Migration 0156: the address the PIM reported when the draft was made is the one every later read
// shows, and an account the PIM does not list makes no draft.
func TestCreateRecordsTheSenderAddress(t *testing.T) {
	pool := draftTestPool(t)
	owner := draftTestIdentity(t, pool)
	senders := &fakeSenders{addresses: map[string]string{"t__personale": "someone@gmail.com"}}
	store := NewStore(pool).WithSenders(senders)
	ctx := context.Background()
	input := DraftInput{
		IdentityID: owner, ConversationID: "sender-conversation", ToolCallID: "call-sender",
		Target:             calendarSend,
		RegisteredToolName: "calendar__calendar",
		OriginalArgs:       json.RawMessage(`{"action":"send_email","accountId":"t__personale","to":["first@example.test"],"subject":"Hello","body":"Text"}`),
		ExpiresAt:          time.Now().Add(time.Hour),
	}
	draft, err := store.Create(ctx, input)
	if err != nil || draft.SenderAddress != "someone@gmail.com" {
		t.Fatalf("create = %+v, %v", draft, err)
	}
	read, err := store.Get(ctx, owner, draft.ID)
	if err != nil || read.SenderAddress != "someone@gmail.com" {
		t.Fatalf("read back = %q, %v", read.SenderAddress, err)
	}
	unknown := input
	unknown.ToolCallID = "call-unknown"
	unknown.OriginalArgs = json.RawMessage(`{"action":"send_email","accountId":"t__invented","to":["first@example.test"],"subject":"Hello","body":"Text"}`)
	if _, err := store.Create(ctx, unknown); !errors.Is(err, ErrUnknownAccount) {
		t.Fatalf("an unknown account made a draft: %v", err)
	}
}
