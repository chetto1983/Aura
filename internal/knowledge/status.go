// Status reads the applied-Cypher-migration audit trail from Postgres
// (aura.knowledge_migrations) via the Slice 0.5 sqlc bindings, for `aura neo4j
// status` tabulation. Postgres is the source of truth for migration history;
// Neo4j Community has no migration tracker of its own.
package knowledge

import (
	"context"
	"fmt"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5/pgxpool"
)

// MigrationRow is one applied-migration audit record for CLI display.
type MigrationRow struct {
	Version   int32
	Name      string
	Checksum  string
	AppliedAt time.Time
}

// Status returns the applied Cypher migrations ordered by version ascending.
func Status(ctx context.Context, pool *pgxpool.Pool) ([]MigrationRow, error) {
	applied, err := sqlc.New(pool).ListAppliedKnowledgeMigrations(ctx)
	if err != nil {
		return nil, fmt.Errorf("list applied knowledge migrations: %w", err)
	}
	rows := make([]MigrationRow, 0, len(applied))
	for _, a := range applied {
		rows = append(rows, MigrationRow{
			Version:   a.Version,
			Name:      a.Name,
			Checksum:  a.Checksum,
			AppliedAt: a.AppliedAt.Time,
		})
	}
	return rows, nil
}
