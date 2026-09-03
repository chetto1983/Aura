package agui

import (
	"testing"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

func userSnapshot(texts ...string) displaySnapshotEvent {
	var snap displaySnapshotEvent
	for i, text := range texts {
		role := llm.RoleUser
		if i%2 == 1 {
			role = llm.RoleAssistant
		}
		snap.Messages = append(snap.Messages, displaySnapshotMessage{
			ID: text, Role: types.Role(role), Content: text,
		})
	}
	return snap
}

// The live defect this merge exists to end (2026-09-03): with no per-turn record, the
// cockpit could only zip a thread's assets onto user turns by position, so an image sent
// with the third message rendered against the first.
func TestAttachTurnAttachmentsAddressesTheTurnThatSentThem(t *testing.T) {
	snap := userSnapshot("ciao", "hello", "chi sei?", "aura", "guarda")

	attachTurnAttachments(&snap, []conversations.TurnAttachments{
		{Seq: 9, UserOrdinal: 2, IDs: []string{"asset-a", "asset-b"}},
	})

	if got := snap.Messages[0].AttachmentIDs; len(got) != 0 {
		t.Errorf("first user turn got %v, want none", got)
	}
	if got := snap.Messages[2].AttachmentIDs; len(got) != 0 {
		t.Errorf("second user turn got %v, want none", got)
	}
	got := snap.Messages[4].AttachmentIDs
	if len(got) != 2 || got[0] != "asset-a" || got[1] != "asset-b" {
		t.Errorf("third user turn = %v, want both ids in order", got)
	}
}

// Rows are sparse -- only turns that carry attachments -- so the merge must address them
// by ordinal and never by their index in the slice.
func TestAttachTurnAttachmentsIsAddressedByOrdinalNotRowOrder(t *testing.T) {
	snap := userSnapshot("one", "a", "two", "b", "three")

	attachTurnAttachments(&snap, []conversations.TurnAttachments{
		{Seq: 3, UserOrdinal: 1, IDs: []string{"second"}},
		{Seq: 7, UserOrdinal: 2, IDs: []string{"third"}},
	})

	if got := snap.Messages[0].AttachmentIDs; len(got) != 0 {
		t.Errorf("untouched turn = %v, want none", got)
	}
	if got := snap.Messages[2].AttachmentIDs; len(got) != 1 || got[0] != "second" {
		t.Errorf("second user turn = %v", got)
	}
	if got := snap.Messages[4].AttachmentIDs; len(got) != 1 || got[0] != "third" {
		t.Errorf("third user turn = %v", got)
	}
}

// Additive display data degrades, never panics: an ordinal past the end of a snapshot
// (a branch read, a truncated history) attaches nothing.
func TestAttachTurnAttachmentsIgnoresAnOrdinalItCannotPlace(t *testing.T) {
	snap := userSnapshot("only")

	attachTurnAttachments(&snap, []conversations.TurnAttachments{
		{Seq: 40, UserOrdinal: 12, IDs: []string{"stray"}},
		{Seq: 41, UserOrdinal: -1, IDs: []string{"impossible"}},
	})

	if got := snap.Messages[0].AttachmentIDs; len(got) != 0 {
		t.Errorf("message = %v, want an unplaceable row to attach nothing", got)
	}
}
