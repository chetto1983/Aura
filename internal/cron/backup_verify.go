package cron

import (
	"context"
	"fmt"
)

// BackupVerifier verifies a SQLite database backup for integrity and row counts.
// The implementation (cmd/aura) snapshots the live DB to a temp file, calls
// dbrecovery.VerifyBackup, then cleans up the snapshot.
type BackupVerifier interface {
	Verify(ctx context.Context) (BackupVerifyResult, error)
}

// BackupVerifyResult holds the outcome of a verification pass.
type BackupVerifyResult struct {
	IntegrityStatus string
	RowCounts       map[string]int
}

func (h *Handler) dispatchBackupVerify(ctx context.Context) error {
	if h.backupVerifier == nil {
		return fmt.Errorf("backup verify: verifier unavailable")
	}
	result, err := h.backupVerifier.Verify(ctx)
	if err != nil {
		if h.notifier != nil {
			h.notifier.NotifyOwners(ctx, fmt.Sprintf("Aura backup verification FAILED: %v", err))
		}
		return fmt.Errorf("backup verify: %w", err)
	}
	h.logger.Info("backup verification ok",
		"integrity", result.IntegrityStatus,
		"row_counts", fmt.Sprintf("%v", result.RowCounts),
	)
	return nil
}
