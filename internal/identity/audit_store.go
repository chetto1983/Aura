package identity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The append-only identity-audit store copies the canonical Store pattern proved in
// internal/skills/audit_store.go: Store{pool,q} over the generated sqlc surface,
// SQLSTATE-based error classification via errors.As + pgErr.Code (never message
// matching), a sentinel error, pgtype conversion at the boundary. aura.identity_audit
// is APPEND-ONLY at the database (migration 0021, T-28-01-01): the role grant
// withholds UPDATE/DELETE/TRUNCATE and two triggers raise. Production writes append
// through InsertIdentityAuditTx inside the provisioning saga's db.WithTx so the audit
// row commits atomically with the identity + grants; the store also exposes a read
// path. The db_integration build adds a non-tx insert helper for round-trip tests.

// ErrAuditImmutable is the sentinel for an attempted UPDATE/DELETE/TRUNCATE against
// the append-only ledger. Both the role-denied path and the trigger raise SQLSTATE
// 42501 (insufficient_privilege); classifyAuditErr maps that to this sentinel.
var ErrAuditImmutable = errors.New("identity_audit is append-only")

// IdentityAuditStore wraps a pgx pool and the generated Queries (canonical shape).
// It is INSERT + SELECT only — the append-only contract is enforced at the DB, and
// the Store mirrors it by exposing no mutation method.
type IdentityAuditStore struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// NewIdentityAuditStore builds an IdentityAuditStore over an open pool.
func NewIdentityAuditStore(pool *pgxpool.Pool) *IdentityAuditStore {
	return &IdentityAuditStore{pool: pool, q: sqlc.New(pool)}
}

// IdentityAuditRow is the domain projection of one aura.identity_audit row — plain
// Go types at the package boundary instead of the sqlc pgtype wrappers.
type IdentityAuditRow struct {
	ID                  string
	CreatedAt           time.Time
	ActorIdentityID     string
	NewIdentityID       string
	NewIdentityName     string
	GrantedCapabilities []string
	AuthulaUserID       string
}

// IdentityAuditInsert carries the plain fields for one new audit row: the creating
// operator (ActorIdentityID), the provisioned identity (NewIdentityID/Name), the
// capability set granted at creation, and the bound Authula login.
type IdentityAuditInsert struct {
	ActorIdentityID     string
	NewIdentityID       string
	NewIdentityName     string
	GrantedCapabilities []string
	AuthulaUserID       string
}

// toParams converts an IdentityAuditInsert to the generated InsertIdentityAuditParams,
// parsing the new identity UUID at the boundary. GrantedCapabilities is normalized to
// a non-nil slice so the NOT NULL text[] column always receives an array.
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

// InsertIdentityAuditTx appends one audit row using a tx-bound Queries (the
// provisioning saga's db.WithTx closure passes it), so the INSERT participates in the
// surrounding atomic write — "exactly one immutable row per successful provision".
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

// IdentityAuditFilter narrows the audit list. NewIdentityID "" lists all rows;
// Limit <= 0 falls back to 100.
type IdentityAuditFilter struct {
	NewIdentityID string // "" = all identities
	Limit         int
}

// ListIdentityAudit returns audit rows newest-first, optionally filtered to one
// provisioned identity. It is the read half (aura_app holds SELECT).
func (s *IdentityAuditStore) ListIdentityAudit(ctx context.Context, f IdentityAuditFilter) ([]IdentityAuditRow, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	var newID pgtype.UUID
	if f.NewIdentityID != "" {
		u, err := uuid.Parse(f.NewIdentityID)
		if err != nil {
			return nil, fmt.Errorf("list identity audit: invalid new_identity_id %q: %w", f.NewIdentityID, err)
		}
		newID = pgtype.UUID{Bytes: u, Valid: true}
	}
	rows, err := s.q.ListIdentityAudit(ctx, sqlc.ListIdentityAuditParams{
		Limit:         int32(limit),
		NewIdentityID: newID,
	})
	if err != nil {
		return nil, fmt.Errorf("list identity audit: %w", err)
	}
	out := make([]IdentityAuditRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, identityAuditFromRow(r))
	}
	return out, nil
}

// identityAuditFromRow projects a generated row onto the domain IdentityAuditRow.
func identityAuditFromRow(r sqlc.AuraIdentityAudit) IdentityAuditRow {
	return IdentityAuditRow{
		ID:                  uuid.UUID(r.ID.Bytes).String(),
		CreatedAt:           r.CreatedAt.Time,
		ActorIdentityID:     r.ActorIdentityID,
		NewIdentityID:       uuid.UUID(r.NewIdentityID.Bytes).String(),
		NewIdentityName:     r.NewIdentityName,
		GrantedCapabilities: r.GrantedCapabilities,
		AuthulaUserID:       r.AuthulaUserID,
	}
}

// classifyAuditErr maps a pgx error to a sentinel via SQLSTATE (errors.As +
// pgErr.Code, never message-matching, mirrors skills.classifyAuditErr): 42501
// insufficient_privilege (role-denied OR the append-only trigger) → ErrAuditImmutable.
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
