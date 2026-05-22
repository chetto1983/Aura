package migrations

import (
	"context"
	"database/sql"
	"fmt"
)

// addEmbedCacheOutputDim extends the embedding_cache primary key with
// output_dim so that vectors at different MRL truncation sizes are stored
// separately. Old rows get output_dim=0 (sentinel = "unknown dim"); the
// cache treats them as misses for any non-zero configured dim, preventing
// wrong-dimension vectors from being served after a dim change.
func addEmbedCacheOutputDim(ctx context.Context, tx *sql.Tx) error {
	exists, err := txTableExists(ctx, tx, "embedding_cache")
	if err != nil {
		return fmt.Errorf("migrations: check embedding_cache exists: %w", err)
	}
	if !exists {
		return nil
	}
	steps := []struct {
		desc string
		sql  string
	}{
		{"rename", `ALTER TABLE embedding_cache RENAME TO _embedding_cache_bak_mig26`},
		{"recreate", `CREATE TABLE embedding_cache (
  content_sha TEXT NOT NULL,
  model       TEXT NOT NULL,
  output_dim  INTEGER NOT NULL DEFAULT 0,
  embedding   BLOB NOT NULL,
  created_at  TIMESTAMP NOT NULL,
  PRIMARY KEY (content_sha, model, output_dim)
)`},
		// output_dim omitted → DEFAULT 0 applied to all legacy rows.
		{"copy", `INSERT INTO embedding_cache (content_sha, model, embedding, created_at)
  SELECT content_sha, model, embedding, created_at FROM _embedding_cache_bak_mig26`},
		{"drop", `DROP TABLE _embedding_cache_bak_mig26`},
	}
	for _, step := range steps {
		if _, err := tx.ExecContext(ctx, step.sql); err != nil {
			return fmt.Errorf("migrations: embed_cache_output_dim %s: %w", step.desc, err)
		}
	}
	return nil
}
