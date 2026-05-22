package migrations

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

func migrateMistralAPIKeyToSecrets(ctx context.Context, tx *sql.Tx) error {
	var plaintext string
	err := tx.QueryRowContext(ctx,
		`SELECT value FROM settings WHERE key = 'MISTRAL_API_KEY'`,
	).Scan(&plaintext)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("migrations: read mistral setting: %w", err)
	}
	if plaintext != "" {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO secrets (key, value, updated_at) VALUES (?, ?, ?)`,
			"mistral_api_key", plaintext, time.Now().UTC().Format(time.RFC3339Nano),
		); err != nil {
			return fmt.Errorf("migrations: insert mistral secret: %w", err)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = 'MISTRAL_API_KEY'`); err != nil {
		return fmt.Errorf("migrations: clear mistral setting: %w", err)
	}
	return nil
}
