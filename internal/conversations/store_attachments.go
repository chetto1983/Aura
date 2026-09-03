package conversations

// What a turn was sent with, read back for display.
//
// Deliberately a sibling of TurnReasoning rather than a field on Turn/llm.Message, for the
// same reason: the history rebuild must stay structurally incapable of carrying it into the
// model context. An attachment reaches the model as projected CONTENT (an image part, a
// document in scope), never as a list of ids in the prompt.
//
// It exists because nothing recorded the link. The asset carried a thread_id and the turn
// carried nothing, so the web client could only zip assets onto user turns by POSITION --
// the Nth asset onto the Nth user turn -- which put an image sent with the third message
// against the first (measured 2026-09-03, migration 0116).

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
)

// TurnAttachments is one turn's attachment id list. Only turns that carry attachments are
// returned at all, so an empty IDs slice never occurs here — the absence of a row is how
// "sent with nothing" is expressed.
//
// UserOrdinal is the turn's index among USER turns, which is what a snapshot consumer can
// match on: the messages it merges into carry no seq, while user turns rebuild
// one-for-one and in order. Seq is kept for logs and tests, where the real turn number is
// the useful identifier.
type TurnAttachments struct {
	Seq         int
	UserOrdinal int
	IDs         []string
}

// ListTurnAttachments returns the attachment ids of every turn that has any, in seq order,
// over the same conversation-wide scope LoadHistory reads.
//
// The caller pairs by UserOrdinal, never by the row's position in this slice: rows are
// sparse (only turns that carry attachments), so matching the k-th row to the k-th user
// message would reproduce the very bug the column was added to end.
func (s *Store) ListTurnAttachments(ctx context.Context, conversationID string) ([]TurnAttachments, error) {
	id, err := db.ParseUUID("conversation_id", conversationID)
	if err != nil {
		return nil, fmt.Errorf("list turn attachments: %w", err)
	}
	var rows []sqlc.ListUserTurnAttachmentsRow
	if err := s.scoped(ctx, func(q *sqlc.Queries) error {
		listed, lErr := q.ListUserTurnAttachments(ctx, id)
		if lErr != nil {
			return fmt.Errorf("list turn attachments %s: %w", conversationID, lErr)
		}
		rows = listed
		return nil
	}); err != nil {
		return nil, err
	}
	out := make([]TurnAttachments, 0, len(rows))
	for _, r := range rows {
		ids := make([]string, 0, len(r.AttachmentIds))
		for _, raw := range r.AttachmentIds {
			// A row that failed to scan as a UUID is dropped rather than emitted as the
			// zero uuid, which would send the reader looking for an asset that cannot exist.
			if !raw.Valid {
				continue
			}
			ids = append(ids, uuid.UUID(raw.Bytes).String())
		}
		if len(ids) == 0 {
			continue
		}
		out = append(out, TurnAttachments{
			Seq: int(r.Seq), UserOrdinal: int(r.Ordinal), IDs: ids,
		})
	}
	return out, nil
}
