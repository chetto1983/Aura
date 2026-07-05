package askuser

import (
	"context"
	"errors"
	"fmt"
	"math"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
)

// store_identity.go holds the Phase-36 owner-scoped approval surface (MUSR-01 / D-06).
// Each method routes through db.WithIdentityTx(identityID) so the migration-0032 RLS owner
// policy on aura.paused_states is active for the statement; the identity_id predicate is
// the primary correctness path and RLS is the backstop. The pause's identity_id is
// populated from its parent conversation by the 0032 trigger (the runner write-path is
// plan 05), so these owner filters are functional now. The base methods in store.go stay
// for the CLI/no-principal path; the AG-UI approval center calls these.

// ListPendingAllForIdentity is the owner-scoped ListPendingAll (APRV-01): every still-pending
// pause across ALL of identityID's conversations, in the same total FIFO order. limit<=0
// falls back to 100 (mirrors ListPendingAll).
func (s *Store) ListPendingAllForIdentity(ctx context.Context, identityID string, limit int) ([]Pending, error) {
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return nil, fmt.Errorf("list pending (all, identity): %w", err)
	}
	var lim int32 = 100
	if limit > 0 && limit <= math.MaxInt32 {
		lim = int32(limit)
	}
	var out []Pending
	txErr := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		rows, lErr := q.ListAllPendingPausedStatesForIdentity(ctx, sqlc.ListAllPendingPausedStatesForIdentityParams{
			IdentityID: owner,
			Limit:      lim,
		})
		if lErr != nil {
			return fmt.Errorf("list pending (all conversations) for %s: %w", identityID, lErr)
		}
		out = make([]Pending, 0, len(rows))
		for _, r := range rows {
			out = append(out, fromRow(r))
		}
		return nil
	})
	if txErr != nil {
		return nil, txErr
	}
	return out, nil
}

// GetByTokenForIdentity fetches one pause scoped to identityID, mapping a miss (unknown
// token OR a token owned by another identity) to ErrPauseNotFound. It is the ownership
// gate the approval-resolve handler consults before delegating the atomic resume to the
// Runner: a hit ⇒ the caller owns the pause (proceed); ErrPauseNotFound ⇒ the handler
// probes unscoped existence to split 403 (a known-foreign token) from 404.
func (s *Store) GetByTokenForIdentity(ctx context.Context, token, identityID string) (Pending, error) {
	id, err := parseUUID("token", token)
	if err != nil {
		return Pending{}, fmt.Errorf("get paused state: %w", err)
	}
	owner, err := parseUUID("identity_id", identityID)
	if err != nil {
		return Pending{}, fmt.Errorf("get paused state: %w", err)
	}
	var pending Pending
	txErr := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		row, gErr := q.GetPausedStateByTokenForIdentity(ctx, sqlc.GetPausedStateByTokenForIdentityParams{
			Token:      id,
			IdentityID: owner,
		})
		if gErr != nil {
			if errors.Is(gErr, pgx.ErrNoRows) {
				return fmt.Errorf("get paused state %s: %w", token, ErrPauseNotFound)
			}
			return fmt.Errorf("get paused state %s: %w", token, gErr)
		}
		pending = fromRow(row)
		return nil
	})
	if txErr != nil {
		return Pending{}, txErr
	}
	return pending, nil
}
