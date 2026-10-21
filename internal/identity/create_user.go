package identity

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

// CreateUserTx inserts a `user` identity named name inside tx and grants it capabilities,
// returning its id. aura.capability_grants is fail-closed as of migration 0087, so the grants
// need app.current_identity bound to the identity being granted; that identity only exists
// as of the INSERT, which is why the scoping happens inside tx rather than around it.
//
// Every capability is validated before anything is written (ErrWildcardManaged,
// ErrInvalidCapability). A duplicate name surfaces as SQLSTATE 23505 for the caller to
// classify.
func CreateUserTx(ctx context.Context, tx pgx.Tx, name string, capabilities []string) (uuid.UUID, error) {
	for _, capability := range capabilities {
		if err := ValidateCapabilityName(capability); err != nil {
			return uuid.Nil, err
		}
	}
	q := sqlc.New(tx)
	newID := uuid.New()
	pgID := pgtype.UUID{Bytes: newID, Valid: true}
	if _, err := q.CreateIdentity(ctx, sqlc.CreateIdentityParams{ID: pgID, Name: name, Kind: "user"}); err != nil {
		return uuid.Nil, fmt.Errorf("create identity: %w", err)
	}
	if err := db.SetTxIdentity(ctx, tx, newID.String()); err != nil {
		return uuid.Nil, fmt.Errorf("scope tx to new identity: %w", err)
	}
	for _, capability := range capabilities {
		if err := q.GrantCapability(ctx, sqlc.GrantCapabilityParams{IdentityID: pgID, Capability: capability}); err != nil {
			return uuid.Nil, fmt.Errorf("grant capability %q: %w", capability, err)
		}
	}
	return newID, nil
}
