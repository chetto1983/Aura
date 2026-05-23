package main

// app_adapters.go collects the small adapter types that bridge cmd/aura-level
// deps (search.Searcher, memoryindex.Store, sql.DB) to the cron and wiki
// interfaces. Kept in a separate file so app_wire.go stays under 600 LOC.

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/cron"
	auradb "github.com/aura/aura/internal/db"
	"github.com/aura/aura/internal/dbrecovery"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/search"
	"github.com/aura/aura/internal/wiki"
)

// searcherDedupAdapter adapts search.Searcher to wiki.DedupSearcher so
// DeduplicateEntities can query nearest neighbors using the existing Qdrant
// backend without a second embedding client. ScoreVector (pure cosine from
// Qdrant) is preferred over the hybrid Score when populated.
type searcherDedupAdapter struct {
	searcher search.Searcher
}

func (a *searcherDedupAdapter) SearchNeighbors(ctx context.Context, entityText string, topK int) ([]wiki.DedupHit, error) {
	results, err := a.searcher.Search(ctx, entityText, topK+1)
	if err != nil {
		return nil, err
	}
	hits := make([]wiki.DedupHit, 0, len(results))
	for _, r := range results {
		score := r.ScoreVector
		if score == 0 {
			score = r.Score
		}
		hits = append(hits, wiki.DedupHit{
			Slug:     r.Slug,
			Category: r.Category,
			Score:    score,
		})
	}
	return hits, nil
}

type memoryDecayAdapter struct {
	store  *memoryindex.Store
	logger *slog.Logger
}

func (a *memoryDecayAdapter) Decay(ctx context.Context) (scanned, deleted, kept int, err error) {
	summary, err := agent.DecayCycle(ctx, a.store, time.Now().UTC(), a.logger)
	return summary.Scanned, summary.Deleted, summary.Kept, err
}

// backupVerifyAdapter implements cron.BackupVerifier by snapshotting the live
// DB into a temp file, verifying it with dbrecovery.VerifyBackup, then cleaning up.
// Uses the shared sql.DB pool (not the dbPath) so the VACUUM INTO runs through
// the pool's existing connection — opening a second sql.DB on the same file
// triggers SQLITE_CANTOPEN (error 14) in DELETE-mode contention with the
// rollback journal. See live debug 2026-05-18.
type backupVerifyAdapter struct {
	db *sql.DB
}

// walCheckpointAdapter implements cron.WALCheckpointer by running
// PRAGMA wal_checkpoint(TRUNCATE) on the live database pool.
type walCheckpointAdapter struct {
	db *sql.DB
}

func (a *walCheckpointAdapter) Checkpoint(ctx context.Context) error {
	return auradb.RetryOnBusy(ctx, func() error {
		_, err := a.db.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
		return err
	})
}

func (a *backupVerifyAdapter) Verify(ctx context.Context) (cron.BackupVerifyResult, error) {
	tmp, err := os.CreateTemp("", "aura-backup-verify-*.db")
	if err != nil {
		return cron.BackupVerifyResult{}, fmt.Errorf("create temp file: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer func() {
		_ = os.Remove(tmpPath)
		_ = os.Remove(tmpPath + "-wal")
		_ = os.Remove(tmpPath + "-shm")
	}()

	if err := auradb.SnapshotFromPool(ctx, a.db, tmpPath); err != nil {
		return cron.BackupVerifyResult{}, fmt.Errorf("snapshot: %w", err)
	}

	minRows := map[string]int{
		"principals":      0,
		"actors":          0,
		"conversations":   0,
		"scheduled_tasks": 1,
	}
	report, err := dbrecovery.VerifyBackup(ctx, tmpPath, minRows)
	if err != nil {
		return cron.BackupVerifyResult{}, err
	}
	return cron.BackupVerifyResult{
		IntegrityStatus: report.IntegrityStatus,
		RowCounts:       report.RowCounts,
	}, nil
}
