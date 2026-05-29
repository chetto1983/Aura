// Schema DDL executor backed by the official Neo4j Go driver. This is the
// CLAUDE.md-sanctioned "Go driver native (fallback se mcp-neo4j-cypher non
// sufficiente)" path, used ONLY for schema operations (CREATE/DROP
// CONSTRAINT|INDEX). The MCP subprocess (client.go) remains the LLM-facing
// runtime interface for data reads/writes.
//
// Why a separate path: Neo4j forbids schema commands inside an explicit
// transaction, and mcp-neo4j-cypher's write tool always wraps queries in a
// managed write-tx — so it rejects schema DDL ("Only write queries are allowed").
// The industrial pattern (golang-migrate's neo4j driver, Neo4j-Migrations) runs
// schema via the driver in AUTO-COMMIT mode: session.Run, never ExecuteWrite.
// D-06 spirit is preserved: a connectivity failure errors out (no restart, no
// graceful degrade).
package knowledge

import (
	"context"
	"fmt"

	"github.com/neo4j/neo4j-go-driver/v5/neo4j"
)

// SchemaExecutor runs Cypher schema DDL via auto-commit driver sessions.
type SchemaExecutor struct {
	driver neo4j.DriverWithContext
	db     string
}

// OpenSchema dials Neo4j over bolt and verifies connectivity. Caller must Close.
func OpenSchema(ctx context.Context, cfg *Config) (*SchemaExecutor, error) {
	driver, err := neo4j.NewDriverWithContext(cfg.BoltURL, neo4j.BasicAuth(cfg.User, cfg.Password, ""))
	if err != nil {
		return nil, fmt.Errorf("neo4j driver init: %w", err)
	}
	if err := driver.VerifyConnectivity(ctx); err != nil {
		_ = driver.Close(ctx)
		return nil, fmt.Errorf("neo4j connectivity (%s): %w", cfg.BoltURL, err)
	}
	return &SchemaExecutor{driver: driver, db: cfg.Database}, nil
}

// Exec runs one schema statement in an auto-commit transaction. Using session.Run
// (NOT ExecuteWrite) is mandatory: schema DDL is rejected inside an explicit tx.
func (s *SchemaExecutor) Exec(ctx context.Context, stmt string) error {
	session := s.driver.NewSession(ctx, neo4j.SessionConfig{DatabaseName: s.db})
	defer func() { _ = session.Close(ctx) }()
	result, err := session.Run(ctx, stmt, nil)
	if err != nil {
		return err
	}
	// Drain so the auto-commit transaction is fully applied before returning.
	if _, err := result.Consume(ctx); err != nil {
		return err
	}
	return nil
}

// Close releases the driver connection pool.
func (s *SchemaExecutor) Close(ctx context.Context) error {
	return s.driver.Close(ctx)
}
