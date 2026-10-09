// Package approvalpolicies is the narrowing counterpart of internal/approvalgrants (prd.md
// §5, "A tool policy per identity", 2026-10-09): the per-identity rows that say a tool, or
// one verb of an action-multiplexed tool, must be asked for whatever its tier (`ask`) or
// must never run (`deny`). A grant widens, a policy narrows; the gateway reads both and
// gives the policy precedence.
//
// It is a Store in the canonical shape internal/identity established (Store{pool,q}, no
// interface declared here — the gateway and the cockpit each declare the narrow one they
// need). Every method binds app.current_identity before touching the table, because
// aura.gateway_tool_policies carries the fail-closed RLS pair from migration 0087: a
// connection that has not said whose policies it means must see none.
package approvalpolicies

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/chetto1983/aura/internal/approvalgrants"
	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
)

// Policy is the two-word vocabulary the table's CHECK constraint enforces.
type Policy string

const (
	// PolicyAsk routes the subject to approval whatever its tier, through the reservation
	// funnel, and its prompt offers no "always".
	PolicyAsk Policy = "ask"
	// PolicyDeny refuses the call at the gateway with a reason the model reads.
	PolicyDeny Policy = "deny"
)

// ErrInvalidPolicy reports a word outside ask|deny before it reaches the CHECK constraint,
// so the caller gets a message naming the vocabulary rather than a constraint violation.
var ErrInvalidPolicy = errors.New("policy must be ask or deny")

// ParsePolicy validates an operator-typed word.
func ParsePolicy(s string) (Policy, error) {
	switch Policy(s) {
	case PolicyAsk, PolicyDeny:
		return Policy(s), nil
	}
	return "", fmt.Errorf("%w: got %q", ErrInvalidPolicy, s)
}

// Store wraps a pgx pool and the generated Queries.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// New builds a Store over an open pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlc.New(pool)}
}

// Row is one durable policy: the identity narrows tool+action to Policy.
type Row struct {
	Tool   string
	Action string
	Policy Policy
	SetAt  time.Time
	SetBy  string
}

// Subject renders the row the way the grants and the approval prompt name it, through the
// one renderer, so a policy reads exactly like the grant it outranks.
func (r Row) Subject() string { return approvalgrants.Subject(r.Tool, r.Action) }

func (s *Store) withIdentity(ctx context.Context, identityID string, fn func(*sqlc.Queries) error) error {
	if s.pool == nil {
		return fn(s.q)
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, fn)
}

// Set records or replaces the policy for tool+action. An empty tool is refused: a policy
// with no subject would match nothing and reads, in a listing, like it matches everything.
func (s *Store) Set(ctx context.Context, identityID, tool, action string, policy Policy, setBy string) error {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return fmt.Errorf("set tool policy: %w", err)
	}
	if tool == "" {
		return fmt.Errorf("set tool policy: tool is required")
	}
	if _, err := ParsePolicy(string(policy)); err != nil {
		return fmt.Errorf("set tool policy: %w", err)
	}
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		return q.SetGatewayToolPolicy(ctx, sqlc.SetGatewayToolPolicyParams{
			IdentityID: id,
			Tool:       tool,
			Action:     action,
			Policy:     string(policy),
			SetBy:      optionalText(setBy),
		})
	})
	if err != nil {
		return fmt.Errorf("set tool policy %q/%q for %s: %w", tool, action, identityID, err)
	}
	return nil
}

// Get returns the policy for exactly this tool+action and whether one exists. It is an
// exact match on both coordinates by design: there is no wildcard row and no prefix rule,
// so a policy can never turn out to be wider than the subject that created it.
func (s *Store) Get(ctx context.Context, identityID, tool, action string) (Policy, bool, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return "", false, fmt.Errorf("get tool policy: %w", err)
	}
	var policy string
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		policy, e = q.GetGatewayToolPolicy(ctx, sqlc.GetGatewayToolPolicyParams{
			IdentityID: id,
			Tool:       tool,
			Action:     action,
		})
		return e
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get tool policy %q/%q for %s: %w", tool, action, identityID, err)
	}
	return Policy(policy), true, nil
}

// List returns the identity's policies, ordered by tool then action.
func (s *Store) List(ctx context.Context, identityID string) ([]Row, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return nil, fmt.Errorf("list tool policies: %w", err)
	}
	var rows []sqlc.AuraGatewayToolPolicies
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		rows, e = q.ListGatewayToolPolicies(ctx, id)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("list tool policies for %s: %w", identityID, err)
	}
	out := make([]Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, Row{
			Tool:   r.Tool,
			Action: r.Action,
			Policy: Policy(r.Policy),
			SetAt:  r.SetAt.Time,
			SetBy:  r.SetBy.String,
		})
	}
	return out, nil
}

// Clear drops a policy and reports whether a row was actually removed, so a caller can tell
// "cleared" from "there was nothing there" instead of printing success over a typo.
func (s *Store) Clear(ctx context.Context, identityID, tool, action string) (bool, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return false, fmt.Errorf("clear tool policy: %w", err)
	}
	var affected int64
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		affected, e = q.ClearGatewayToolPolicy(ctx, sqlc.ClearGatewayToolPolicyParams{
			IdentityID: id,
			Tool:       tool,
			Action:     action,
		})
		return e
	})
	if err != nil {
		return false, fmt.Errorf("clear tool policy %q/%q for %s: %w", tool, action, identityID, err)
	}
	return affected > 0, nil
}

// optionalText maps an empty attribution to SQL NULL, matching the column's documented
// "NULL for a row seeded outside the cockpit and the CLI".
func optionalText(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}
