package learning

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// SweepStaleProposals deletes proposed_updates rows with status='pending'
// whose created_at is older than maxAge. Approved/rejected rows are never
// purged regardless of age.
//
// Returns the count of purged rows and any storage error. A nil error with
// purged=0 means no stale rows existed — this is the expected steady-state.
func SweepStaleProposals(ctx context.Context, db *sql.DB, maxAge time.Duration) (purged int, err error) {
	cutoff := time.Now().UTC().Add(-maxAge).Format("2006-01-02 15:04:05")
	result, execErr := db.ExecContext(ctx, `
		DELETE FROM proposed_updates
		WHERE status = 'pending'
		  AND created_at < ?`,
		cutoff,
	)
	if execErr != nil {
		return 0, fmt.Errorf("sweep stale proposals: %w", execErr)
	}
	n, rowsErr := result.RowsAffected()
	if rowsErr != nil {
		return 0, fmt.Errorf("sweep stale proposals: rows affected: %w", rowsErr)
	}
	return int(n), nil
}
