package conversations

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
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
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return Conversation{}, fmt.Errorf("get conversation: %w", err)
	}
	owner, err := db.ParseUUID("identity_id", identityID)
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
		last, lErr := q.GetConversationLastInputTokens(ctx, id)
		if lErr != nil {
			return fmt.Errorf("get conversation %s last input tokens: %w", conversationID, lErr)
		}
		conv.LastInputTokens = int64(last)
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
	owner, err := db.ParseUUID("identity_id", identityID)
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
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("delete conversation: %w", err)
	}
	owner, err := db.ParseUUID("identity_id", identityID)
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
	return affected, err
}

// VersionForIdentity returns the monotonic version advanced by every exported
// conversation, turn, or thread-asset mutation.
func (s *Store) VersionForIdentity(ctx context.Context, conversationID, identityID string) (int64, error) {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return 0, err
	}
	owner, err := db.ParseUUID("identity_id", identityID)
	if err != nil {
		return 0, err
	}
	var version int64
	err = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		value, queryErr := q.GetConversationVersionForIdentity(ctx, sqlc.GetConversationVersionForIdentityParams{ID: id, IdentityID: owner})
		if queryErr != nil {
			return queryErr
		}
		version = value
		return nil
	})
	return version, err
}

// ReserveDeleteForIdentityIfVersion commits the export-delete ownership fence before
// any in-memory or external teardown. Every snapshot writer is DB-guarded against a
// reserved conversation, including writers in another server process.
func (s *Store) ReserveDeleteForIdentityIfVersion(ctx context.Context, conversationID, identityID string, expected int64, reservation string) (int64, error) {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return 0, err
	}
	owner, err := db.ParseUUID("identity_id", identityID)
	if err != nil {
		return 0, err
	}
	var affected int64
	err = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		value, reserveErr := q.ReserveConversationDeleteForIdentityIfVersion(ctx, sqlc.ReserveConversationDeleteForIdentityIfVersionParams{
			ID: id, IdentityID: owner, ExpectedVersion: expected,
			Reservation: pgtype.Text{String: reservation, Valid: true},
		})
		affected = value
		return reserveErr
	})
	return affected, err
}

// ReservedDelete is durable recovery work for one export-delete operation.
type ReservedDelete struct {
	ConversationID  string
	IdentityID      string
	ExpectedVersion int64
	Reservation     string
	Phase           string
}

// ExportDeleteRecoveryGrace keeps the reconciler away from a freshly reserved
// foreground export-delete while its detached finalizer is still allowed to run.
// It must remain greater than runner's foreground finalization timeout.
const ExportDeleteRecoveryGrace = 3 * time.Minute

// ClaimDeleteTeardown crosses the irreversible boundary and leases execution
// to one worker while the operation reservation remains the durable authority.
func (s *Store) ClaimDeleteTeardown(ctx context.Context, conversationID, identityID, reservation, worker string, leaseExpiresAt time.Time) (int64, error) {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return 0, err
	}
	owner, err := db.ParseUUID("identity_id", identityID)
	if err != nil {
		return 0, err
	}
	var affected int64
	err = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		value, markErr := q.ClaimConversationDeleteTeardown(ctx, sqlc.ClaimConversationDeleteTeardownParams{
			ID: id, IdentityID: owner, Reservation: pgtype.Text{String: reservation, Valid: true},
			Worker: pgtype.Text{String: worker, Valid: true}, LeaseExpiresAt: pgtype.Timestamptz{Time: leaseExpiresAt.UTC(), Valid: true},
			Now: pgtype.Timestamptz{Time: time.Now().UTC(), Valid: true},
		})
		affected = value
		return markErr
	})
	return affected, err
}

// ReleaseDeleteLease makes a failed attempt immediately retryable without ever
// releasing the operation reservation after teardown has started.
func (s *Store) ReleaseDeleteLease(ctx context.Context, conversationID, identityID, reservation, worker string) (int64, error) {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return 0, err
	}
	owner, err := db.ParseUUID("identity_id", identityID)
	if err != nil {
		return 0, err
	}
	var affected int64
	err = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		value, releaseErr := q.ReleaseConversationDeleteLease(ctx, sqlc.ReleaseConversationDeleteLeaseParams{
			ID: id, IdentityID: owner, Reservation: pgtype.Text{String: reservation, Valid: true},
			Worker: pgtype.Text{String: worker, Valid: true},
		})
		affected = value
		return releaseErr
	})
	return affected, err
}

// ReleaseReservedDelete removes a fence only before teardown has started.
func (s *Store) ReleaseReservedDelete(ctx context.Context, conversationID, identityID, reservation string) (int64, error) {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return 0, err
	}
	owner, err := db.ParseUUID("identity_id", identityID)
	if err != nil {
		return 0, err
	}
	var affected int64
	err = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		value, releaseErr := q.ReleaseReservedConversationDelete(ctx, sqlc.ReleaseReservedConversationDeleteParams{
			ID: id, IdentityID: owner, Reservation: pgtype.Text{String: reservation, Valid: true},
		})
		affected = value
		return releaseErr
	})
	return affected, err
}

// ListReservedDeletes returns a bounded cross-owner recovery page. The stored
// reservation is the authority; recovery never mints or adopts another token.
//
// Cross-owner and fail-closed RLS are not contradictory here, but they do dictate the
// shape: aura.conversations has been invisible without an identity since migration 0089, so
// this sweep ENUMERATES the owners (aura.identities carries no RLS) and runs the same
// unscoped recovery query once per owner, each inside that owner's transaction. The
// alternative — an RLS carve-out letting any connection read reserved rows — would leak
// cross-tenant existence of a conversation mid-delete to buy nothing this loop does not
// already give. limit bounds the WHOLE page, not each owner's slice, so a single tenant with
// a long backlog cannot starve the others out of one tick.
func (s *Store) ListReservedDeletes(ctx context.Context, limit int32) ([]ReservedDelete, error) {
	if limit <= 0 {
		return []ReservedDelete{}, nil
	}
	owners, err := s.ownerIdentities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list reserved conversation deletes: %w", err)
	}
	now := time.Now().UTC()
	params := sqlc.ListReservedConversationDeletesParams{
		ReservedBefore: pgtype.Timestamptz{Time: now.Add(-ExportDeleteRecoveryGrace), Valid: true},
		Now:            pgtype.Timestamptz{Time: now, Valid: true},
		BatchSize:      limit,
	}
	deletes := make([]ReservedDelete, 0, limit)
	for _, owner := range owners {
		if int32(len(deletes)) >= limit {
			break
		}
		params.BatchSize = limit - int32(len(deletes))
		ownerDeletes, oErr := s.reservedDeletesForOwner(ctx, owner, params)
		if oErr != nil {
			return nil, oErr
		}
		deletes = append(deletes, ownerDeletes...)
	}
	return deletes, nil
}

// reservedDeletesForOwner is one owner's slice of the recovery page: the same unscoped
// query, filtered to that owner by RLS rather than by a predicate.
func (s *Store) reservedDeletesForOwner(ctx context.Context, owner string, params sqlc.ListReservedConversationDeletesParams) ([]ReservedDelete, error) {
	var deletes []ReservedDelete
	if err := db.WithIdentityTx(ctx, s.pool, owner, func(q *sqlc.Queries) error {
		rows, lErr := q.ListReservedConversationDeletes(ctx, params)
		if lErr != nil {
			return fmt.Errorf("list reserved conversation deletes: %w", lErr)
		}
		deletes = make([]ReservedDelete, 0, len(rows))
		for _, row := range rows {
			if !row.DeleteReservation.Valid || !row.DeletePhase.Valid {
				return errors.New("list reserved conversation deletes: invalid lifecycle row")
			}
			deletes = append(deletes, ReservedDelete{
				ConversationID: uuid.UUID(row.ID.Bytes).String(), IdentityID: uuid.UUID(row.IdentityID.Bytes).String(),
				ExpectedVersion: row.SnapshotVersion, Reservation: row.DeleteReservation.String, Phase: row.DeletePhase.String,
			})
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return deletes, nil
}

// ConversationDeleteCompleted observes the durable terminal state of one exact
// reservation. A matching row remains while another worker is active or retryable;
// absence means its reservation committed the delete, never a version conflict.
func (s *Store) ConversationDeleteCompleted(ctx context.Context, conversationID, identityID, reservation string) (bool, error) {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return false, err
	}
	owner, err := db.ParseUUID("identity_id", identityID)
	if err != nil {
		return false, err
	}
	var completed bool
	err = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		value, observeErr := q.ConversationDeleteCompleted(ctx, sqlc.ConversationDeleteCompletedParams{
			ID: id, IdentityID: owner, Reservation: pgtype.Text{String: reservation, Valid: true},
		})
		completed = value
		return observeErr
	})
	return completed, err
}

// DeleteForIdentityIfReservation is the only persistence delete that can remove a
// reserved conversation. The token proves this lifecycle owns the committed fence.
func (s *Store) DeleteForIdentityIfReservation(ctx context.Context, conversationID, identityID, reservation, worker string) (int64, error) {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return 0, err
	}
	owner, err := db.ParseUUID("identity_id", identityID)
	if err != nil {
		return 0, err
	}
	var affected int64
	err = db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		value, deleteErr := q.DeleteConversationForIdentityIfReservation(ctx, sqlc.DeleteConversationForIdentityIfReservationParams{
			ID: id, IdentityID: owner, Reservation: pgtype.Text{String: reservation, Valid: true},
			Worker: pgtype.Text{String: worker, Valid: true},
		})
		affected = value
		return deleteErr
	})
	if err != nil || affected == 0 {
		return affected, err
	}
	if purgeErr := s.purgeConversationArtifacts(conversationID); purgeErr != nil {
		return affected, purgeErr
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
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("update status: %w", err)
	}
	owner, err := db.ParseUUID("identity_id", identityID)
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
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("rename: %w", err)
	}
	owner, err := db.ParseUUID("identity_id", identityID)
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

// UpdateReasoningEffortForIdentity persists the per-conversation reasoning-effort symbol
// into the metadata jsonb only when identityID owns the conversation (Phase 37E WEBMODEL-01
// / D-06 — Claude parity: persisted + restored on reopen). Returns rows-affected (0 = not
// owned, mirroring the other *ForIdentity mutations). The effort is a symbol validated
// upstream (plan 06's two-stage governance); this method is the dumb owner-scoped writer,
// mirroring RenameForIdentity — no schema migration (the metadata column exists since 0005).
func (s *Store) UpdateReasoningEffortForIdentity(ctx context.Context, conversationID, identityID, effort string) (int64, error) {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return 0, fmt.Errorf("update reasoning effort: %w", err)
	}
	owner, err := db.ParseUUID("identity_id", identityID)
	if err != nil {
		return 0, fmt.Errorf("update reasoning effort: %w", err)
	}
	var affected int64
	txErr := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		n, uErr := q.UpdateConversationReasoningEffortForIdentity(ctx, sqlc.UpdateConversationReasoningEffortForIdentityParams{
			Effort:     effort,
			ID:         id,
			IdentityID: owner,
		})
		if uErr != nil {
			return fmt.Errorf("update reasoning effort %s: %w", conversationID, uErr)
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
