package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

func addSecretsTable(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS secrets (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL,
  updated_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
);
`)
	if err != nil {
		return fmt.Errorf("migrations: add secrets table: %w", err)
	}
	return nil
}
