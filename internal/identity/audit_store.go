package identity

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// ErrAuditImmutable is the sentinel for an attempted UPDATE/DELETE/TRUNCATE against
// the append-only identity audit ledger.
var ErrAuditImmutable = errors.New("identity_audit is append-only")

// IdentityAuditInsert carries the plain fields for one new audit row.
type IdentityAuditInsert struct {
	ActorIdentityID     string
	NewIdentityID       string
	NewIdentityName     string
	GrantedCapabilities []string
	AuthulaUserID       string
}

// toParams converts an IdentityAuditInsert to the generated sqlc insert params.
func (in IdentityAuditInsert) toParams() (sqlc.InsertIdentityAuditParams, error) {
	newID, err := uuid.Parse(in.NewIdentityID)
	if err != nil {
		return sqlc.InsertIdentityAuditParams{}, fmt.Errorf("invalid new_identity_id %q: %w", in.NewIdentityID, err)
	}
	caps := in.GrantedCapabilities
	if caps == nil {
		caps = []string{}
	}
	return sqlc.InsertIdentityAuditParams{
		ActorIdentityID:     in.ActorIdentityID,
		NewIdentityID:       pgtype.UUID{Bytes: newID, Valid: true},
		NewIdentityName:     in.NewIdentityName,
		GrantedCapabilities: caps,
		AuthulaUserID:       in.AuthulaUserID,
	}, nil
}

// InsertIdentityAuditTx appends one audit row using a tx-bound Queries.
func InsertIdentityAuditTx(ctx context.Context, q *sqlc.Queries, in IdentityAuditInsert) error {
	params, err := in.toParams()
	if err != nil {
		return fmt.Errorf("insert identity audit (tx): %w", err)
	}
	if _, err := q.InsertIdentityAudit(ctx, params); err != nil {
		return fmt.Errorf("insert identity audit %q (tx): %w", in.NewIdentityName, classifyAuditErr(err))
	}
	return nil
}

func classifyAuditErr(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	if pgErr.Code == "42501" {
		return fmt.Errorf("%w: %v", ErrAuditImmutable, err)
	}
	return err
}
