// identity_link.go owns the operator-user <-> Aura-identity binding (spec §5). Authula
// authenticates "who you are" (a users.id in the authula schema); Aura authorizes "what
// agent.run requires" via capability_grants keyed on aura.identities.id. The bridge is
// a single join row in aura.identity_auth_links(identity_id, authula_user_id UNIQUE),
// created by migration 0019. For the single-operator cockpit the Authula user-id is
// pinned to the seeded `local` identity UUID (00000000-…-0001).
//
// The link is queried over Aura's existing pgxpool (NOT bun, NOT the authula pool) —
// the table lives in aura.*, so it is owned by Aura's migration ledger. The UNIQUE is
// on authula_user_id (not identity_id) so the schema generalizes 1:N for the post-v1.0.0
// multi-user milestone (spec OQ-8).
package webauth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrLinkNotFound is returned when no identity is bound to the given Authula user-id.
var ErrLinkNotFound = errors.New("webauth: no aura identity linked to authula user")

// IdentityLinker resolves + upserts the operator-user <-> identity binding over the
// aura.identity_auth_links table. It satisfies the session_validate.go userResolver seam.
type IdentityLinker struct {
	pool *pgxpool.Pool
}

// NewIdentityLinker binds the linker to Aura's app pool (the same pool the identity
// Store uses). A nil pool yields ErrLinkNotFound on every resolve (fail closed).
func NewIdentityLinker(pool *pgxpool.Pool) *IdentityLinker {
	return &IdentityLinker{pool: pool}
}

// ResolveIdentityID returns the Aura identity UUID bound to the given Authula user-id,
// or ErrLinkNotFound when no link exists. It is the userResolver implementation the
// Validator calls on every authenticated request.
func (l *IdentityLinker) ResolveIdentityID(ctx context.Context, authulaUserID string) (string, error) {
	if l == nil || l.pool == nil {
		return "", ErrLinkNotFound
	}
	const q = `SELECT identity_id::text FROM aura.identity_auth_links WHERE authula_user_id = $1`
	var identityID string
	if err := l.pool.QueryRow(ctx, q, authulaUserID).Scan(&identityID); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrLinkNotFound
		}
		return "", fmt.Errorf("resolve identity link: %w", err)
	}
	return identityID, nil
}

// LinkOperator idempotently binds an Authula user-id to an Aura identity UUID. It is the
// enrollment-time call (spec M1) that pins the operator user to the `local` identity.
// Re-linking the same pair is a no-op; re-pointing an existing authula_user_id updates
// the bound identity (ON CONFLICT on the UNIQUE authula_user_id).
func (l *IdentityLinker) LinkOperator(ctx context.Context, identityID, authulaUserID string) error {
	if l == nil || l.pool == nil {
		return errors.New("webauth: identity linker has no pool")
	}
	const q = `
		INSERT INTO aura.identity_auth_links (identity_id, authula_user_id)
		VALUES ($1::uuid, $2)
		ON CONFLICT (authula_user_id) DO UPDATE SET identity_id = EXCLUDED.identity_id`
	if _, err := l.pool.Exec(ctx, q, identityID, authulaUserID); err != nil {
		return fmt.Errorf("link operator: %w", err)
	}
	return nil
}
