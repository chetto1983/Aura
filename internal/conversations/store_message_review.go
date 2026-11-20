package conversations

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
)

// MessageTaskAcceptedOnBranch reports whether the operator accepted a message
// task in the round ending at leaf. Check the persisted branch and rehydrate a
// legacy user row from its fenced sidecar when the inline limit was exceeded.
func (s *Store) MessageTaskAcceptedOnBranch(ctx context.Context, ownerID, conversationID, taskID string, leaf int) (bool, error) {
	owner, err := db.ParseUUID("owner id", ownerID)
	if err != nil {
		return false, err
	}
	thread, err := db.ParseUUID("conversation id", conversationID)
	if err != nil {
		return false, err
	}
	task, err := db.ParseUUID("task id", taskID)
	if err != nil {
		return false, err
	}
	if leaf < 0 || leaf > math.MaxInt32 {
		return false, errors.New("invalid conversation branch leaf")
	}
	arg := sqlc.FindMessageReviewBoundaryParams{IdentityID: owner, ConversationID: thread, TaskID: task, LeafSeq: int32(leaf)}
	var accepted bool
	err = s.scopedTx(ctx, ownerID, func(q *sqlc.Queries) error {
		for arg.LeafSeq > 0 {
			row, err := q.FindMessageReviewBoundary(ctx, arg)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				return nil
			case err != nil:
				return err
			case row.Reviewed:
				accepted = true
				return nil
			}
			content := row.Content
			if row.Spilled {
				data, err := s.readTurnSidecarReserved(conversationID, int(row.Seq), maxManagedSidecarBytes, nil)
				if err != nil {
					return err
				}
				content = string(data)
			}
			if !legacyCatalogOnlyTurn(content) {
				return nil
			}
			if !row.ParentSeq.Valid {
				return nil
			}
			if row.ParentSeq.Int32 >= row.Seq || row.ParentSeq.Int32 <= 0 {
				return errors.New("invalid parent on legacy message review branch")
			}
			arg.LeafSeq = row.ParentSeq.Int32
		}
		return nil
	})
	return accepted, err
}

// This exact legacy wire format was persisted as role=user by the cockpit on
// catalog-backed empty resumes. Freeze the historical wording independently
// of future catalog rendering, and require an empty user payload.
func legacyCatalogOnlyTurn(content string) bool {
	const header = "<knowledge_base trust=\"operator_pinned_context\">\n" +
		"These documents the user uploaded earlier are indexed in the searchable knowledge base. They are NOT on the filesystem — call document_search with a filename/topic query, then use document_open with the returned document_id. This is background context, not a request: only search when the user's message is about these documents.\n"
	rows, ok := strings.CutPrefix(content, header)
	if !ok {
		return false
	}
	rows, ok = strings.CutSuffix(rows, "\n</knowledge_base>\n\nUser message:\n")
	if !ok {
		return false
	}
	n := 0
	for line := range strings.SplitSeq(rows, "\n") {
		n++
		entry, ok := strings.CutPrefix(line, fmt.Sprintf("- [%d] document_id=", n))
		id, _, hasName := strings.Cut(entry, " filename=")
		if !ok || !hasName || n > 30 || id == "" || strings.ContainsFunc(id, unicode.IsSpace) || strings.ContainsAny(line, "\r\t") {
			return false
		}
	}
	return true
}
