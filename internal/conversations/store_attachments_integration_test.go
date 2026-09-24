//go:build db_integration

package conversations

import (
	"reflect"
	"testing"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// TestListTurnAttachmentsPairsByUserOrdinal pins why the column exists (migration 0116):
// rows are sparse and carry the index among USER turns, so an attachment sent with the
// third user message never lands on the first.
func TestListTurnAttachmentsPairsByUserOrdinal(t *testing.T) {
	pool := migratedPool(t)
	s := newStore(t, pool)
	convID := newConversation(t, s)
	ctx := ownerCtx()
	first, second, third := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for _, p := range []AppendTurnParams{
		{Seq: 1, Role: llm.RoleSystem, Content: "you are aura"},
		{Seq: 2, Role: llm.RoleUser, Content: "look at this", AttachmentIDs: []string{first}},
		{Seq: 3, Role: llm.RoleAssistant, Content: "seen"},
		{Seq: 4, Role: llm.RoleUser, Content: "no file this time"},
		{Seq: 5, Role: llm.RoleUser, Content: "two more", AttachmentIDs: []string{second, third}},
	} {
		p.ConversationID = convID
		if err := s.AppendTurn(ctx, p); err != nil {
			t.Fatalf("AppendTurn seq %d: %v", p.Seq, err)
		}
	}

	got, err := s.ListTurnAttachments(ctx, convID)
	if err != nil {
		t.Fatalf("ListTurnAttachments: %v", err)
	}
	want := []TurnAttachments{
		{Seq: 2, UserOrdinal: 0, IDs: []string{first}},
		{Seq: 5, UserOrdinal: 2, IDs: []string{second, third}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListTurnAttachments = %+v, want %+v", got, want)
	}
	if _, err := s.ListTurnAttachments(ctx, "not-a-uuid"); err == nil {
		t.Fatal("ListTurnAttachments accepted a malformed conversation id")
	}
}
