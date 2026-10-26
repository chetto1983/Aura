package manager

import (
	"context"
	"errors"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
)

// The append-only MCP-audit store copies the canonical Store pattern proved in
// internal/skills/audit_store.go and internal/identity/audit_store.go: Store{pool,q}
// over the generated sqlc surface, SQLSTATE-based error classification via errors.As
// + pgErr.Code (never message matching), a sentinel error, pgtype conversion at the
// boundary. aura.mcp_audit is APPEND-ONLY at the database (migration 0022,
// T-29-01-01): the role grant withholds UPDATE/DELETE/TRUNCATE and two triggers
// raise. Production writes append through InsertMCPAuditTx inside the mutation's
// db.WithTx so the audit row commits atomically with the config change (D-04). The
// cockpit reads the ledger through the admin activity feed (internal/agui/audit_store.go),
// so the read half here exists only for db_integration tests (audit_store_integration.go).
// The schema is flat like 0021 — NO D-29 coherence
// machinery (no ApprovalSource/token NULL-mapping), so the store omits it too.

// ErrMCPAuditImmutable is the sentinel for an attempted UPDATE/DELETE/TRUNCATE
// against the append-only ledger. Both the role-denied path and the trigger raise
// SQLSTATE 42501 (insufficient_privilege); classifyMCPAuditErr maps that here.
var ErrMCPAuditImmutable = errors.New("mcp_audit is append-only")

// MCPAuditInsert carries the plain fields for one new audit row: the capability-layer
// principal (ActorIdentityID, D-03), the mutation (Action — one of install/edit/
// enable/disable/remove/trust), the affected ServerName, and an optional operator
// Reason ("" maps to a NULL column — present only on `trust`).
type MCPAuditInsert struct {
	ActorIdentityID string
	Action          string
	ServerName      string
	Reason          string // "" -> NULL
}

// toParams converts an MCPAuditInsert to the generated InsertMcpAuditParams, applying
// the NULL boundary mapping for the optional reason column.
func (in MCPAuditInsert) toParams() sqlc.InsertMcpAuditParams {
	var reason pgtype.Text
	if in.Reason != "" {
		reason = pgtype.Text{String: in.Reason, Valid: true}
	}
	return sqlc.InsertMcpAuditParams{
		ActorIdentityID: in.ActorIdentityID,
		Action:          in.Action,
		ServerName:      in.ServerName,
		Reason:          reason,
	}
}

// InsertMCPAuditTx appends one audit row using a tx-bound Queries (the mutation's
// db.WithTx closure passes it), so the INSERT participates in the surrounding atomic
// write — a config mutation cannot commit without its audit row.
func InsertMCPAuditTx(ctx context.Context, q *sqlc.Queries, in MCPAuditInsert) error {
	if _, err := q.InsertMcpAudit(ctx, in.toParams()); err != nil {
		return fmt.Errorf("insert mcp audit %q (tx): %w", in.ServerName, classifyMCPAuditErr(err))
	}
	return nil
}

// classifyMCPAuditErr maps a pgx error to a sentinel via SQLSTATE (errors.As +
// pgErr.Code, never message-matching, mirrors identity.classifyAuditErr): 42501
// insufficient_privilege (role-denied OR the append-only trigger) → ErrMCPAuditImmutable.
func classifyMCPAuditErr(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	if pgErr.Code == "42501" {
		return fmt.Errorf("%w: %v", ErrMCPAuditImmutable, err)
	}
	return err
}
