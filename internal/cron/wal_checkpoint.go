package cron

import (
	"context"
	"fmt"
)

// WALCheckpointer flushes the SQLite WAL file to prevent unbounded growth.
// The production adapter (cmd/aura) executes PRAGMA wal_checkpoint(TRUNCATE)
// on the live database connection pool.
type WALCheckpointer interface {
	Checkpoint(ctx context.Context) error
}

func (h *Handler) dispatchWALCheckpoint(ctx context.Context) error {
	if h.walCheckpointer == nil {
		return fmt.Errorf("wal checkpoint: checkpointer unavailable")
	}
	if err := h.walCheckpointer.Checkpoint(ctx); err != nil {
		return fmt.Errorf("wal checkpoint: %w", err)
	}
	h.logger.Info("wal checkpoint complete")
	return nil
}
