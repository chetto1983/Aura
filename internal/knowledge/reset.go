// Reset is the dev-only destructive teardown behind `aura neo4j reset --yes`.
// It drops the D-08 schema objects, deletes all nodes, clears the Postgres
// audit trail so versions re-apply, then re-runs Migrate to a clean baseline.
// Schema DROPs and the bulk delete both go through the driver-backed
// SchemaExecutor (auto-commit) for the same reason migrate does — MCP cannot run
// schema DDL. Never wire this into a non-dev path; the CLI dispatcher owns the
// --yes + AURA_RESET_YES guard.
package knowledge

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Reset drops indexes/constraints + all nodes, clears the audit table, and
// re-applies migrations. Dev only.
func Reset(ctx context.Context, schema *SchemaExecutor, pool *pgxpool.Pool) error {
	teardown := []string{
		"DROP INDEX chunk_embedding IF EXISTS",
		"DROP INDEX chunk_text IF EXISTS",
		"DROP CONSTRAINT chunk_id IF EXISTS",
		"MATCH (n) DETACH DELETE n",
	}
	for _, stmt := range teardown {
		if err := schema.Exec(ctx, stmt); err != nil {
			return fmt.Errorf("reset cypher %q: %w", stmt, err)
		}
	}
	if _, err := pool.Exec(ctx, "DELETE FROM aura.knowledge_migrations"); err != nil {
		return fmt.Errorf("clear knowledge_migrations audit: %w", err)
	}
	if _, err := Migrate(ctx, schema, pool); err != nil {
		return fmt.Errorf("re-migrate after reset: %w", err)
	}
	return nil
}
