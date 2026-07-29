package conversations

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
)

// PurgeConversations removes every durable conversation owned by identityID after
// reclaiming its sidecar trees. The filesystem-first order is retry-safe: a crash
// before the database transaction leaves rows that identify every remaining tree,
// while repeating a successful directory removal is harmless.
func (s *Store) PurgeConversations(ctx context.Context, identityID string) error {
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return fmt.Errorf("purge conversations: %w", err)
	}

	var ids []string
	if err := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		rows, listErr := q.ListConversationIDsForIdentityPurge(ctx, owner)
		if listErr != nil {
			return fmt.Errorf("list owned conversations: %w", listErr)
		}
		ids = make([]string, 0, len(rows))
		for _, row := range rows {
			if !row.Valid {
				return fmt.Errorf("list owned conversations: invalid conversation id")
			}
			ids = append(ids, uuid.UUID(row.Bytes).String())
		}
		return nil
	}); err != nil {
		return err
	}

	for _, id := range ids {
		if err := s.purgeConversationArtifacts(id); err != nil {
			return err
		}
	}

	return db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		if _, err := q.DeleteConversationsForIdentityPurge(ctx, owner); err != nil {
			return fmt.Errorf("delete owned conversations: %w", err)
		}
		remaining, err := q.CountConversationsForIdentityPurge(ctx, owner)
		if err != nil {
			return fmt.Errorf("verify owned conversation purge: %w", err)
		}
		if remaining != 0 {
			return fmt.Errorf("verify owned conversation purge: %d conversations remain", remaining)
		}
		return nil
	})
}
