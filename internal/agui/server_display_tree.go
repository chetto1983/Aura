package agui

// The history snapshot is the conversation tree (spec 2026-10-27, D5 and D6): every turn of
// every branch, with id msg-<seq> and parentId msg-<parent_seq>, null at a root. The cockpit
// imports it into its runtime and moves between branches without asking the server again.

import (
	"fmt"

	"github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/types"
	"github.com/chetto1983/aura/internal/conversations"
	"github.com/chetto1983/aura/internal/llm"
)

func seqID(seq int) string { return fmt.Sprintf("msg-%d", seq) }

// snapshotID names a message after its turn. A result the repair synthesized has no turn, so
// it is named after the assistant turn it hangs from and the call it answers.
func snapshotID(t conversations.DisplayTurn) string {
	if t.Seq > 0 {
		return seqID(t.Seq)
	}
	return fmt.Sprintf("msg-%d-%s", t.ParentSeq, t.Message.ToolCallID)
}

func snapshotParentID(t conversations.DisplayTurn) *string {
	if t.ParentSeq <= 0 {
		return nil
	}
	id := seqID(t.ParentSeq)
	return &id
}

// regeneratePlaceholder is the turn a regenerate forks before the agent answers (D6): an
// assistant turn with no content, no tool calls and no reasoning. It has nothing to draw, and
// the answer written after it is what the regenerate produced.
func regeneratePlaceholder(m *displaySnapshotMessage) bool {
	return m.Role == types.Role(llm.RoleAssistant) && m.Content == "" && len(m.ToolCalls) == 0 && m.Reasoning == ""
}

// hideMessages takes the messages hide picks out of the snapshot and hangs their children
// from the nearest message still drawn. A parent not drawn before its child, which only a
// damaged pointer produces, becomes the message drawn just before it: the order turns had
// before branches, and never a parent the cockpit cannot find.
func hideMessages(snap *displaySnapshotEvent, hide func(*displaySnapshotMessage) bool) {
	ancestor := make(map[string]*string)
	drawn := make(map[string]bool, len(snap.Messages))
	var previous *string
	kept := snap.Messages[:0]
	for _, m := range snap.Messages {
		if m.ParentID != nil {
			if a, hidden := ancestor[*m.ParentID]; hidden {
				m.ParentID = a
			}
		}
		if hide(&m) {
			ancestor[m.ID] = m.ParentID
			continue
		}
		if m.ParentID != nil && !drawn[*m.ParentID] {
			m.ParentID = previous
		}
		drawn[m.ID] = true
		id := m.ID
		previous = &id
		kept = append(kept, m)
	}
	snap.Messages = kept
}
