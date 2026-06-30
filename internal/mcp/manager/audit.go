package manager

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
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
// db.WithTx so the audit row commits atomically with the config change (D-04); the
// store also exposes a read path. The schema is flat like 0021 — NO D-29 coherence
// machinery (no ApprovalSource/token NULL-mapping), so the store omits it too.

// ErrMCPAuditImmutable is the sentinel for an attempted UPDATE/DELETE/TRUNCATE
// against the append-only ledger. Both the role-denied path and the trigger raise
// SQLSTATE 42501 (insufficient_privilege); classifyMCPAuditErr maps that here.
var ErrMCPAuditImmutable = errors.New("mcp_audit is append-only")

// MCPAuditStore wraps a pgx pool and the generated Queries (canonical shape). It is
// INSERT + SELECT only — the append-only contract is enforced at the DB, and the
// Store mirrors it by exposing no mutation method.
type MCPAuditStore struct {
	q *sqlc.Queries
}

// MCPAuditRow is the domain projection of one aura.mcp_audit row — plain Go types at
// the package boundary instead of the sqlc pgtype wrappers. Reason is the empty
// string when the underlying column is NULL.
type MCPAuditRow struct {
	ID              string
	CreatedAt       time.Time
	ActorIdentityID string
	Action          string
	ServerName      string
	Reason          string
}

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

// MCPAuditFilter narrows the audit list. ServerName "" lists all servers; a zero
// Since imposes no lower bound; Limit <= 0 falls back to 100.
type MCPAuditFilter struct {
	ServerName string    // "" = all servers
	Since      time.Time // zero = no lower bound
	Limit      int
}

// List returns audit rows newest-first (created_at DESC, id DESC), filtered by server
// name and/or a since cutoff. It is the read half (aura_app holds SELECT).
func (s *MCPAuditStore) List(ctx context.Context, f MCPAuditFilter) ([]MCPAuditRow, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > math.MaxInt32 {
		limit = math.MaxInt32
	}
	var name pgtype.Text
	if f.ServerName != "" {
		name = pgtype.Text{String: f.ServerName, Valid: true}
	}
	var since pgtype.Timestamptz
	if !f.Since.IsZero() {
		since = pgtype.Timestamptz{Time: f.Since, Valid: true}
	}
	rows, err := s.q.ListMcpAudit(ctx, sqlc.ListMcpAuditParams{
		Limit:      int32(limit),
		ServerName: name,
		Since:      since,
	})
	if err != nil {
		return nil, fmt.Errorf("list mcp audit: %w", err)
	}
	out := make([]MCPAuditRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, mcpAuditFromRow(r))
	}
	return out, nil
}

// mcpAuditFromRow projects a generated row onto the domain MCPAuditRow.
func mcpAuditFromRow(r sqlc.AuraMcpAudit) MCPAuditRow {
	out := MCPAuditRow{
		ID:              uuid.UUID(r.ID.Bytes).String(),
		CreatedAt:       r.CreatedAt.Time,
		ActorIdentityID: r.ActorIdentityID,
		Action:          r.Action,
		ServerName:      r.ServerName,
	}
	if r.Reason.Valid {
		out.Reason = r.Reason.String
	}
	return out
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
