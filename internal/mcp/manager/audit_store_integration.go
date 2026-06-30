//go:build db_integration

package manager

import (
	"context"
	"fmt"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgxpool"
)

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
