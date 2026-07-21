package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

const migrationAdvisoryLockKey int64 = 0x415552414d494752 // "AURAMIGR"

// WithMigrationLock serializes schema/bootstrap ownership without depending on
// any Aura-owned table. The connection-level lock is released by PostgreSQL if
// the process exits before the callback completes.
func WithMigrationLock(ctx context.Context, lockURL string, run func(context.Context) error) error {
	if lockURL == "" {
		return errors.New("migration lock URL is empty")
	}
	if run == nil {
		return errors.New("migration lock callback is nil")
	}
	conn, err := pgx.Connect(ctx, lockURL)
	if err != nil {
		return fmt.Errorf("connect migration owner %s: %w", redactDSN(lockURL), err)
	}
	defer func() { _ = conn.Close(context.WithoutCancel(ctx)) }()
	if _, err := conn.Exec(ctx, `SELECT pg_advisory_lock($1)`, migrationAdvisoryLockKey); err != nil {
		return fmt.Errorf("acquire migration owner: %w", err)
	}
	return run(ctx)
}
