package breakglass

import (
	"context"
	"errors"
	"fmt"

	authulaservices "github.com/Authula/authula/services"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/agui"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/identity"
)

// auditEventOperatorRecovered is the neutral, secret-free audit event appended on a
// successful break-glass recovery. It mirrors recovery.go's break_glass_token_minted:
// only IdentityID + Event are set, so Metadata defaults to '{}' — no password/answer
// is ever recorded (D-06).
const auditEventOperatorRecovered = "operator_password_recovered"

// Deps are the live backends RecoverOperator writes through: the aura pgx pool
// (identity resolution + auth-link lookup + recovery re-seed + audit) and Authula's
// CoreServices (offline argon2 reset). NoRecovery skips only the identity_recovery
// re-seed leg (--no-recovery), leaving the password reset + session kill + audit intact.
type Deps struct {
	Pool       *pgxpool.Pool
	Core       *authulaservices.CoreServices
	NoRecovery bool
}

// RecoverOperator composes the full offline break-glass flow into one call:
//
//  1. resolve the single operator via ListIdentities → selectSoleOperator (a guard
//     refusal returns BEFORE any write, so 0/ambiguous operators change nothing);
//  2. reset the password + kill sessions through Authula (setOperatorPassword);
//  3. unless NoRecovery, re-seed aura.identity_recovery with an argon2 answer hash
//     (idempotent UpsertIdentityRecovery — never a duplicate row);
//  4. append the neutral operator_password_recovered audit row.
//
// The re-seed is ordered BEFORE the audit so a failed re-seed never leaves a
// "recovered" audit record. It returns the operator identity id on success. No
// password/answer is ever placed in an error or the audit row.
func RecoverOperator(ctx context.Context, deps Deps, secrets Secrets) (string, error) {
	if deps.Pool == nil {
		return "", errors.New("break-glass requires a database pool")
	}

	ids, err := identity.New(deps.Pool).ListIdentities(ctx)
	if err != nil {
		return "", fmt.Errorf("list identities: %w", err)
	}
	op, err := selectSoleOperator(ids)
	if err != nil {
		return "", err
	}

	if err := setOperatorPassword(ctx, deps.Pool, deps.Core, op.ID, secrets.Password); err != nil {
		return "", err
	}

	opUUID, err := uuid.Parse(op.ID)
	if err != nil {
		return "", fmt.Errorf("parse operator identity id: %w", err)
	}
	opID := pgtype.UUID{Bytes: opUUID, Valid: true}
	q := sqlc.New(deps.Pool)

	if !deps.NoRecovery {
		hash, version, err := agui.RecoveryHasher{}.HashAnswer(secrets.Answer)
		if err != nil {
			return "", fmt.Errorf("hash recovery answer: %w", err)
		}
		if err := q.UpsertIdentityRecovery(ctx, sqlc.UpsertIdentityRecoveryParams{
			IdentityID:        opID,
			Question:          secrets.Question,
			AnswerHash:        hash,
			AnswerHashVersion: version,
		}); err != nil {
			return "", fmt.Errorf("re-seed identity recovery: %w", err)
		}
	}

	if _, err := q.InsertIdentityRecoveryAudit(ctx, sqlc.InsertIdentityRecoveryAuditParams{
		IdentityID: opID,
		Event:      auditEventOperatorRecovered,
	}); err != nil {
		return "", fmt.Errorf("append recovery audit: %w", err)
	}

	return op.ID, nil
}
