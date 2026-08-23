// Package approvalgrants is the durable half of the gateway's approval scopes (PRD
// amendment #127): the per-identity "always approve this verb" rows an operator creates by
// picking the widest scope on an approval prompt, and revokes with `aura gateway grants`.
//
// It is a Store in the canonical shape internal/identity established (Store{pool,q}, no
// interface declared here — the gateway declares the narrow one it needs). Every method
// binds app.current_identity before touching the table, because aura.gateway_approval_grants
// carries the fail-closed RLS pair from migration 0087: a connection that has not said whose
// grants it means must see none.
package approvalgrants

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store wraps a pgx pool and the generated Queries.
type Store struct {
	pool *pgxpool.Pool
	q    *sqlc.Queries
}

// New builds a Store over an open pool.
func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool, q: sqlc.New(pool)}
}

// Grant is one durable row: the identity has standing approval for tool+action.
type Grant struct {
	Tool      string
	Action    string
	GrantedAt time.Time
	GrantedBy string
}

// Subject renders the grant the way the approval option named it. It is the SINGLE renderer
// — the gateway's own grant subject calls it — so the label an operator approved and the
// label they later revoke can never drift apart.
func (g Grant) Subject() string { return Subject(g.Tool, g.Action) }

// Subject renders a tool+action pair. An action-less tool is just its name; a multiplexed
// one is "tool verb", which is what the operator reads on the approval button.
func Subject(tool, action string) string {
	if action == "" {
		return tool
	}
	return tool + " " + action
}

// withIdentity runs fn with app.current_identity bound, satisfying the table's fail-closed
// policies. A pool-less Store is the fake-DBTX construction the package's unit tests use;
// it has no transaction to scope, so it runs fn on the injected Queries directly.
func (s *Store) withIdentity(ctx context.Context, identityID string, fn func(*sqlc.Queries) error) error {
	if s.pool == nil {
		return fn(s.q)
	}
	return db.WithIdentityTx(ctx, s.pool, identityID, fn)
}

// Grant records a standing approval. It is idempotent (ON CONFLICT DO NOTHING), so an
// operator who picks "always" twice does not see an error the second time — they see the
// state they asked for, which is what they meant. An empty tool is refused: a grant with no
// subject would match nothing and reads, in a listing, like it matches everything.
func (s *Store) Grant(ctx context.Context, identityID, tool, action, grantedBy string) error {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return fmt.Errorf("grant approval: %w", err)
	}
	if tool == "" {
		return fmt.Errorf("grant approval: tool is required")
	}
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		return q.GrantGatewayApproval(ctx, sqlc.GrantGatewayApprovalParams{
			IdentityID: id,
			Tool:       tool,
			Action:     action,
			GrantedBy:  optionalText(grantedBy),
		})
	})
	if err != nil {
		return fmt.Errorf("grant approval %q/%q for %s: %w", tool, action, identityID, err)
	}
	return nil
}

// Has reports whether the identity holds a standing approval for exactly this tool+action.
// It is an exact match on both coordinates by design: there is no wildcard row and no
// prefix rule, so a grant can never turn out to be wider than the label that created it.
func (s *Store) Has(ctx context.Context, identityID, tool, action string) (bool, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return false, fmt.Errorf("has approval grant: %w", err)
	}
	var ok bool
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		ok, e = q.HasGatewayApprovalGrant(ctx, sqlc.HasGatewayApprovalGrantParams{
			IdentityID: id,
			Tool:       tool,
			Action:     action,
		})
		return e
	})
	if err != nil {
		return false, fmt.Errorf("has approval grant %q/%q for %s: %w", tool, action, identityID, err)
	}
	return ok, nil
}

// List returns the identity's standing approvals, ordered by tool then action.
func (s *Store) List(ctx context.Context, identityID string) ([]Grant, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return nil, fmt.Errorf("list approval grants: %w", err)
	}
	var rows []sqlc.AuraGatewayApprovalGrants
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		rows, e = q.ListGatewayApprovalGrants(ctx, id)
		return e
	})
	if err != nil {
		return nil, fmt.Errorf("list approval grants for %s: %w", identityID, err)
	}
	out := make([]Grant, 0, len(rows))
	for _, r := range rows {
		out = append(out, Grant{
			Tool:      r.Tool,
			Action:    r.Action,
			GrantedAt: r.GrantedAt.Time,
			GrantedBy: r.GrantedBy.String,
		})
	}
	return out, nil
}

// Revoke drops a standing approval and reports whether a row was actually removed, so the
// CLI can tell the operator "revoked" from "there was nothing there" instead of printing
// success over a typo.
func (s *Store) Revoke(ctx context.Context, identityID, tool, action string) (bool, error) {
	id, err := db.ParseUUID("identity id", identityID)
	if err != nil {
		return false, fmt.Errorf("revoke approval grant: %w", err)
	}
	var affected int64
	err = s.withIdentity(ctx, identityID, func(q *sqlc.Queries) error {
		var e error
		affected, e = q.RevokeGatewayApprovalGrant(ctx, sqlc.RevokeGatewayApprovalGrantParams{
			IdentityID: id,
			Tool:       tool,
			Action:     action,
		})
		return e
	})
	if err != nil {
		return false, fmt.Errorf("revoke approval grant %q/%q for %s: %w", tool, action, identityID, err)
	}
	return affected > 0, nil
}

// optionalText maps an empty attribution to SQL NULL, matching the column's documented
// "NULL for a grant seeded outside the cockpit".
func optionalText(v string) pgtype.Text {
	if v == "" {
		return pgtype.Text{}
	}
	return pgtype.Text{String: v, Valid: true}
}
