// Package identity is the canonical per-domain Store over the generated sqlc
// surface (D-A2-01). It is built FIRST in Phase 4 to prove the Store pattern
// (Store{pool,q} + SQLSTATE error classification + db_integration discipline)
// that conversations/askuser copy verbatim. Scope is the single-user `local`
// identity scaffolding (Slice 1.7): identity CRUD plus capability grant/revoke
// with wildcard-or-exact HasCapability semantics.
//
// No interface is declared here — the consumer (the Runner) declares the narrow
// interface it needs (D-A2-02, "accept interfaces, return structs").
package identity

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Wildcard is the system-managed match-all capability. It is seeded by migration
// 0004 on the `local` identity and is NEVER granted or revoked through the Store
// or CLI — HasCapability treats it as "has every capability".
const Wildcard = "*"

// capNameRe is the capability-name grammar (SPEC Req#6): a lowercase letter
// followed by up to 63 of [a-z0-9._-]. Compiled once at package init.
var capNameRe = regexp.MustCompile(`^[a-z][a-z0-9._-]{0,63}$`)

// Sentinel errors so callers (CLI, tests) classify failures without string
// matching. ErrWildcardManaged is the "wildcard is system-managed" rejection;
// ErrInvalidCapability is the name-grammar failure; ErrIdentityNotFound is a
// missing identity lookup.
var (
	ErrWildcardManaged   = errors.New("wildcard '*' is system-managed and cannot be granted or revoked")
	ErrInvalidCapability = errors.New("invalid capability name")
	ErrIdentityNotFound  = errors.New("identity not found")
)

// Store wraps a pgx pool and the generated Queries. The canonical shape every
// future DB slice copies (D-A2-01): non-tx reads use s.q; atomic multi-statement
// writes wrap db.WithTx (none needed in identity — it is single-statement only).
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// New builds a Store over an open pool. sqlc.New(pool) binds the Queries to the
// pool's DBTX; mirrors the construction in internal/db/tx.go.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlc.New(pool)}
}

// Identity is the domain projection of aura.identities — plain Go types at the
// package boundary instead of the sqlc pgtype wrappers.
type Identity struct {
	ID   string
	Name string
	Kind string
	// Deactivated mirrors the 0029 deactivated_at column (non-NULL ⇒ true). It is surfaced
	// for the HI-02 auth-boundary deny; the GetIdentityBy* queries keep NO deactivated_at
	// filter (the deprovision saga + admin roster still see deactivated rows).
	Deactivated bool
}

// fromRow converts a generated row to the domain projection. DeactivatedAt is a
// pgtype.Timestamptz (0029): Valid == true means deactivated_at IS NOT NULL, i.e. the
// identity is soft-deleted (the HI-02 auth gate denies it).
func fromRow(r sqlc.AuraIdentities) Identity {
	return Identity{
		ID:          uuid.UUID(r.ID.Bytes).String(),
		Name:        r.Name,
		Kind:        r.Kind,
		Deactivated: r.DeactivatedAt.Valid,
	}
}

// ListIdentities returns every identity ordered by creation then name.
func (s *Store) ListIdentities(ctx context.Context) ([]Identity, error) {
	rows, err := s.q.ListIdentities(ctx)
	if err != nil {
		return nil, fmt.Errorf("list identities: %w", err)
	}
	out := make([]Identity, 0, len(rows))
	for _, r := range rows {
		out = append(out, fromRow(r))
	}
	return out, nil
}

// GetIdentityByName fetches one identity by its unique name. A missing row is
// reported as ErrIdentityNotFound (wrapped) rather than the raw pgx.ErrNoRows.
func (s *Store) GetIdentityByName(ctx context.Context, name string) (Identity, error) {
	r, err := s.q.GetIdentityByName(ctx, name)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Identity{}, fmt.Errorf("get identity %q: %w", name, ErrIdentityNotFound)
		}
		return Identity{}, fmt.Errorf("get identity %q: %w", name, err)
	}
	return fromRow(r), nil
}

// GetIdentityByID fetches one identity by UUID. A missing row is reported as
// ErrIdentityNotFound (wrapped) rather than the raw pgx.ErrNoRows.
func (s *Store) GetIdentityByID(ctx context.Context, identityID string) (Identity, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return Identity{}, fmt.Errorf("get identity by id: %w", err)
	}
	r, err := s.q.GetIdentityByID(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Identity{}, fmt.Errorf("get identity id %q: %w", identityID, ErrIdentityNotFound)
		}
		return Identity{}, fmt.Errorf("get identity id %q: %w", identityID, err)
	}
	return fromRow(r), nil
}

// DeleteIdentity removes an identity by name. Its capability_grants cascade away
// via the FK ON DELETE CASCADE (verified by the integration test). Deleting an
// absent identity is a no-op (DELETE affects zero rows, no error).
func (s *Store) DeleteIdentity(ctx context.Context, name string) error {
	if err := s.q.DeleteIdentity(ctx, name); err != nil {
		return fmt.Errorf("delete identity %q: %w", name, err)
	}
	return nil
}

// HasCapability reports whether the identity holds either the '*' wildcard or
// the exact capability. The generated query does the wildcard-or-exact match in
// SQL; only a real DB failure returns a non-nil error.
func (s *Store) HasCapability(ctx context.Context, identityID, capability string) (bool, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return false, fmt.Errorf("has capability: %w", err)
	}
	var ok bool
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		ok, e = q.HasCapability(ctx, sqlc.HasCapabilityParams{IdentityID: id, Capability: capability})
		return e
	})
	if err != nil {
		return false, fmt.Errorf("has capability %q for %s: %w", capability, identityID, err)
	}
	return ok, nil
}

// withIdentity runs fn with app.current_identity bound to identityID, so aura.capability_grants'
// fail-closed policies (migration 0087) admit exactly this identity's grants. Every capability
// method already receives the identity it is asking about, so this is the whole adaptation — the
// grants table is an authorization surface and must never be readable or writable by a
// connection that has not said whose grants it means.
// A pool-less Store is the fake-DBTX construction the package's unit tests use
// (newFakeStore builds &Store{q: sqlc.New(fake)}); it has no transaction to scope, so it
// runs fn on the injected Queries directly. Production always goes through New, which
// always carries the pool.
func (s *Store) withIdentity(ctx context.Context, identityID string, fn func(*sqlc.Queries) error) error {
	if s.pool == nil {
		return fn(s.q)
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, fn)
}

// ListCapabilities returns the identity's granted capability names, ordered by
// name. The '*' wildcard is returned verbatim when present — filtering it out (for
// the D-06 capability picker) is the handler's job, not the store's. An identity
// with no grants yields an empty slice; an invalid UUID is a wrapped error.
func (s *Store) ListCapabilities(ctx context.Context, identityID string) ([]string, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return nil, fmt.Errorf("list capabilities: %w", err)
	}
	var rows []sqlc.AuraCapabilityGrants
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		rows, e = q.ListCapabilities(ctx, id)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("list capabilities for %s: %w", identityID, err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Capability)
	}
	return out, nil
}

// GrantCapability grants an ordinary capability to an identity, idempotently:
// the underlying INSERT uses ON CONFLICT DO NOTHING, and a 23505 unique_violation
// (belt-and-suspenders) is swallowed so a repeat grant is a no-op, never an
// error. Granting '*' is rejected before any DB call (ErrWildcardManaged), and
// names failing the grammar are rejected (ErrInvalidCapability).
func (s *Store) GrantCapability(ctx context.Context, identityID, capability string) error {
	id, err := s.validateGrantInput(identityID, capability)
	if err != nil {
		return fmt.Errorf("grant capability: %w", err)
	}
	if err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		return q.GrantCapability(ctx, sqlc.GrantCapabilityParams{IdentityID: id, Capability: capability})
	}); err != nil {
		if isUniqueViolation(err) {
			return nil // already granted — idempotent no-op
		}
		return fmt.Errorf("grant capability %q to %s: %w", capability, identityID, err)
	}
	return nil
}

// RevokeCapability revokes a capability from an identity, idempotently: revoking
// an absent capability affects zero rows and returns no error. Revoking '*' is
// rejected before any DB call (ErrWildcardManaged); names failing the grammar
// are rejected (ErrInvalidCapability).
func (s *Store) RevokeCapability(ctx context.Context, identityID, capability string) error {
	id, err := s.validateGrantInput(identityID, capability)
	if err != nil {
		return fmt.Errorf("revoke capability: %w", err)
	}
	if err := s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		return q.RevokeCapability(ctx, sqlc.RevokeCapabilityParams{IdentityID: id, Capability: capability})
	}); err != nil {
		return fmt.Errorf("revoke capability %q from %s: %w", capability, identityID, err)
	}
	return nil
}

// ValidateCapabilityName applies the canonical capability grammar without touching the
// database. It rejects the system-managed '*' wildcard and any name outside the
// lowercase [a-z][a-z0-9._-]{0,63} grammar. Composition-root code that must bind raw
// sqlc queries inside an existing transaction uses this helper so the Store and tx paths
// share one validation rule.
func ValidateCapabilityName(capability string) error {
	if capability == Wildcard {
		return ErrWildcardManaged
	}
	if !capNameRe.MatchString(capability) {
		return fmt.Errorf("%w: %q must match %s", ErrInvalidCapability, capability, capNameRe.String())
	}
	return nil
}

// validateGrantInput rejects the system-managed wildcard and invalid names
// BEFORE any DB round-trip (the threat-model T-04-05/T-04-06 mitigation), then
// parses the identity UUID.
func (s *Store) validateGrantInput(identityID, capability string) (pgtype.UUID, error) {
	if err := ValidateCapabilityName(capability); err != nil {
		return pgtype.UUID{}, err
	}
	return db.ParseUUID("identity id", identityID)
}

// isUniqueViolation classifies a pgx error as SQLSTATE 23505 via errors.As +
// pgErr.Code — never string-matching the message (RESEARCH Pitfall 2).
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
