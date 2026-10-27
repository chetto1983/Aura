//go:build db_integration

package manager

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

// NewMCPAuditStore builds an MCPAuditStore over an open pool.
func NewMCPAuditStore(pool *pgxpool.Pool) *MCPAuditStore {
	return &MCPAuditStore{q: sqlc.New(pool)}
}

// InsertMCPAudit appends one audit row via the pool in db_integration builds.
// Production code uses InsertMCPAuditTx so audit rows participate in the config
// mutation's transaction.
func (s *MCPAuditStore) InsertMCPAudit(ctx context.Context, in MCPAuditInsert) (MCPAuditRow, error) {
	row, err := s.q.InsertMcpAudit(ctx, in.toParams())
	if err != nil {
		return MCPAuditRow{}, fmt.Errorf("insert mcp audit %q: %w", in.ServerName, classifyMCPAuditErr(err))
	}
	return mcpAuditFromRow(row), nil
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
