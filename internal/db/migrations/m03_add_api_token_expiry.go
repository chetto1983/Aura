package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

func addAPITokenExpiry(ctx context.Context, tx *sql.Tx) error {
	if err := addMissingColumns(ctx, tx, "api_tokens", []columnDef{
		{Name: "expires_at", SQL: "TEXT"},
	}); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT token_hash, issued_at FROM api_tokens WHERE expires_at IS NULL OR expires_at = ''`)
	if err != nil {
		return fmt.Errorf("migrations: query api token expiry backfill: %w", err)
	}
	defer func() { _ = rows.Close() }()

	type tokenExpiry struct {
		hash      string
		expiresAt string
	}
	var updates []tokenExpiry
	for rows.Next() {
		var hash, issuedAtRaw string
		if err := rows.Scan(&hash, &issuedAtRaw); err != nil {
			return fmt.Errorf("migrations: scan api token expiry backfill: %w", err)
		}
		issuedAt, err := parseStoredTime(issuedAtRaw)
		if err != nil {
			return fmt.Errorf("migrations: parse api_tokens.issued_at %q: %w", issuedAtRaw, err)
		}
		updates = append(updates, tokenExpiry{
			hash:      hash,
			expiresAt: issuedAt.Add(30 * 24 * time.Hour).UTC().Format(time.RFC3339),
		})
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("migrations: iterate api token expiry backfill: %w", err)
	}
	for _, update := range updates {
		if _, err := tx.ExecContext(ctx, `UPDATE api_tokens SET expires_at = ? WHERE token_hash = ?`, update.expiresAt, update.hash); err != nil {
			return fmt.Errorf("migrations: update api token expiry: %w", err)
		}
	}
	return nil
}
