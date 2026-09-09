// capability_denial_store.go persists one row per RequireCapability refusal (RBAC-09/
// RBAC-10, T-02-20): who was denied, which capability, at which route, when, and why.
// aura.capability_denials (migration 0123) is the table; PgCapabilityDenialStore is the
// only writer. The read side is Task 2's fifth UNION leg on audit_store.go's existing
// admin feed — this file has no read method and mounts no route.
package agui

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// The closed set of denial causes RequireCapability's three refusal branches map onto
// (internal/agui/auth.go). Free text here would be a place a message carrying a path or
// an id accumulates — the migration's CHECK constraint enforces the same vocabulary at
// the database layer, so a bug that produces a fourth value fails loudly instead of
// silently becoming a new row shape.
const (
	DenialCauseNoPrincipal = "no_principal"
	DenialCauseStoreError  = "store_error"
	DenialCauseNotHeld     = "not_held"
)

// NoPrincipalIdentityID is the documented sentinel RequireCapability records for its
// no-principal refusal branch: there is no identity uuid to record there, and dropping
// the row would lose exactly the denial RBAC-09 most wants kept. It is deliberately NOT
// a valid UUID, so it can never collide with a real identity id.
const NoPrincipalIdentityID = "(no-principal)"

// PgCapabilityDenialStore appends one row per capability refusal to aura.capability_denials.
type PgCapabilityDenialStore struct {
	pool *pgxpool.Pool
}

// NewPgCapabilityDenialStore builds the recorder over the shared pool. Wired at the
// composition root (cmd/aura) into AuthDeps.DenialRecorder.
func NewPgCapabilityDenialStore(pool *pgxpool.Pool) *PgCapabilityDenialStore {
	return &PgCapabilityDenialStore{pool: pool}
}

// RecordDenial writes one denial row. It is RLS-scoped via db.WithIdentityTx to
// identityID itself — the row being written — the same shape every identity-keyed write
// in this schema uses, so the permissive owner-isolation policy admits the insert for
// both a real identity uuid and the no-principal sentinel.
func (s *PgCapabilityDenialStore) RecordDenial(ctx context.Context, identityID, capability, route, cause string) error {
	if err := db.WithIdentityTx(ctx, s.pool, identityID, func(q *sqlc.Queries) error {
		return q.InsertCapabilityDenial(ctx, sqlc.InsertCapabilityDenialParams{
			IdentityID: identityID,
			Capability: capability,
			Route:      route,
			Cause:      cause,
		})
	}); err != nil {
		return fmt.Errorf("record capability denial for %s/%s: %w", identityID, capability, err)
	}
	return nil
}
