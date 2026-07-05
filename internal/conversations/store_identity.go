package conversations

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// store_identity.go holds the Phase-36 owner-scoped conversation surface (MUSR-01 / D-06).
// Each method routes through db.WithIdentityTx(identityID) so the migration-0032 RLS owner
// policy is active for the statement — the *ForIdentity WHERE clause is the primary
// correctness path (the observable 404/403) and RLS is the kernel backstop that returns 0
// foreign rows even if that clause were ever dropped. The base (unscoped) methods in
// store.go stay for the CLI/no-principal path; the AG-UI handlers call these.
//
// The four mutating variants return rows-affected: 0 means the caller does NOT own the id
// (foreign OR absent), which the handler resolves to 403 (D-06 mutate) vs 404 via a pool
// existence probe. A non-nil error is a real failure, never the not-owned signal.

// GetForIdentity fetches one conversation scoped to identityID, mapping a miss (unknown id
// OR an id owned by another identity) to ErrConversationNotFound — the handler's 404 (a
// read hides foreign existence, D-06).
func (s *Store) GetForIdentity(ctx context.Context, conversationID, identityID string) (Conversation, error) {
	id, err := parseUUID("id", conversationID)
	if err != nil {
		return Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	var conv Conversation
	txErr := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		row, gErr := q.GetConversationForIdentity(ctx, sqlc.GetConversationForIdentityParams{ID: id, IdentityID: owner})
		if gErr != nil {
			if errors.Is(gErr, pgx.ErrNoRows) {
				return fmt.Errorf("get conversation %s: %w", conversationID, ErrConversationNotFound)
			}
			return fmt.Errorf("get conversation %s: %w", conversationID, gErr)
		}
		conv = conversationFromRow(row)
		return nil
	})
	if txErr != nil {
		return Conversation{}, txErr
	}
	return conv, nil
}

// ListForIdentity returns the identity's conversations ordered by last_active_at DESC
// (deleted rows always excluded; includeArchived adds archived rows).
func (s *Store) ListForIdentity(ctx context.Context, identityID string, includeArchived bool) ([]Conversation, error) {
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return nil, fmt.Errorf("list conversations: %w", err)
	}
	var out []Conversation
	txErr := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		rows, lErr := q.ListConversationsForIdentity(ctx, sqlc.ListConversationsForIdentityParams{
			IdentityID:      owner,
			IncludeArchived: includeArchived,
		})
		if lErr != nil {
			return fmt.Errorf("list conversations for %s: %w", identityID, lErr)
		}
		out = make([]Conversation, 0, len(rows))
		for _, r := range rows {
			out = append(out, conversationFromRow(r))
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return out, nil
}

// DeleteForIdentity hard-deletes a conversation only when identityID owns it, then tears
// down its sidecar tree. It returns rows-affected: 0 = not owned (the handler splits
// 403/404 via a pool existence probe), 1 = deleted.
func (s *Store) DeleteForIdentity(ctx context.Context, conversationID, identityID string) (int64, error) {
	id, err := parseUUID("id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("delete conversation: %w", err)
	}
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return 0, fmt.Errorf("delete conversation: %w", err)
	}
	var affected int64
	txErr := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		n, dErr := q.DeleteConversationForIdentity(ctx, sqlc.DeleteConversationForIdentityParams{ID: id, IdentityID: owner})
		if dErr != nil {
			return fmt.Errorf("delete conversation %s: %w", conversationID, dErr)
		}
		affected = n
		return nil
	})
	if txErr != nil {
		return 0, txErr
	}
	if affected == 0 {
		return 0, nil
	}
	if pErr := s.purgeConversationArtifacts(conversationID); pErr != nil {
		return affected, pErr
	}
	return affected, nil
}

// UpdateStatusForIdentity moves a conversation between active/archived/deleted only when
// identityID owns it (the /archive + /unarchive routes). Returns rows-affected (0 = not owned).
func (s *Store) UpdateStatusForIdentity(ctx context.Context, conversationID, identityID, status string) (int64, error) {
	switch status {
	case StatusActive, StatusArchived, StatusDeleted:
	default:
		return 0, fmt.Errorf("update status: invalid status %q", status)
	}
	id, err := parseUUID("id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("update status: %w", err)
	}
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return 0, fmt.Errorf("update status: %w", err)
	}
	var affected int64
	txErr := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		n, uErr := q.UpdateConversationStatusForIdentity(ctx, sqlc.UpdateConversationStatusForIdentityParams{
			ID:         id,
			Status:     status,
			IdentityID: owner,
		})
		if uErr != nil {
			return fmt.Errorf("update status %s -> %s: %w", conversationID, status, uErr)
		}
		affected = n
		return nil
	})
	if txErr != nil {
		return 0, txErr
	}
	return affected, nil
}

// RenameForIdentity sets the conversation title only when identityID owns it. Returns
// rows-affected (0 = not owned).
func (s *Store) RenameForIdentity(ctx context.Context, conversationID, identityID, title string) (int64, error) {
	id, err := parseUUID("id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("rename: %w", err)
	}
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return 0, fmt.Errorf("rename: %w", err)
	}
	var affected int64
	txErr := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		n, rErr := q.RenameConversationForIdentity(ctx, sqlc.RenameConversationForIdentityParams{
			ID:         id,
			Title:      pgtype.Text{String: title, Valid: true},
			IdentityID: owner,
		})
		if rErr != nil {
			return fmt.Errorf("rename %s: %w", conversationID, rErr)
		}
		affected = n
		return nil
	})
	if txErr != nil {
		return 0, txErr
	}
	return affected, nil
}

// SearchConversationTurnsForIdentity is the owner-scoped FTS: SearchConversationTurns with
// every hit filtered to identityID's conversations (MUSR-01). The LOCKED sqlc query is
// unchanged; the owner predicate is the Go-side filter in searchTurns.
func (s *Store) SearchConversationTurnsForIdentity(ctx context.Context, query, identityID string, limit int) ([]SearchResult, error) {
	return s.searchTurns(ctx, query, limit, identityID)
}
