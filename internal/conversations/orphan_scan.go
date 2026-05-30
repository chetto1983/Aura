package conversations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// tmpTTL is the age past which $AURA_RUN_DIR/tmp/* entries are swept at boot (SPEC
// Req#12). A scratch artifact older than this is assumed abandoned.
const tmpTTL = 24 * time.Hour

// defaultRunDirWarnThreshold is the audit-only size-WARN threshold when the caller
// passes 0 (SPEC default 1 GiB). Over this, boot logs a WARN — it NEVER auto-purges.
const defaultRunDirWarnThreshold = int64(1) << 30

// ScanParams carries the boot-scan inputs. WarnThresholdBytes <= 0 falls back to
// the SPEC default. The pool is read-only here (existence lookups only).
type ScanParams struct {
	RunDir             string
	WarnThresholdBytes int64
}

// ScanOrphans is the boot reconciliation GC (D-A5-02), run after db.Open and before
// serving (the 04-05 composition root calls it). It:
//
//   - removes $AURA_RUN_DIR/conversations/<id> dirs with NO matching conversations
//     row (session_id == conversation_id, D-26), under an O_NOFOLLOW/Lstat symlink
//     guard so a malicious symlink cannot redirect RemoveAll outside runDir;
//   - sweeps $AURA_RUN_DIR/tmp/* entries older than 24h;
//   - logs an audit-only WARN if the run dir exceeds the threshold (NEVER purges).
//
// Individual rm failures are WARN-logged and recovered at the next boot scan; they
// do not abort the scan. Only a structural failure (cannot read the runDir) returns
// a wrapped error.
func ScanOrphans(ctx context.Context, pool *pgxpool.Pool, p ScanParams) error {
	if p.RunDir == "" {
		return nil // no run dir configured → nothing to reconcile
	}
	q := sqlc.New(pool)

	if err := scanConversationOrphans(ctx, q, p.RunDir); err != nil {
		return err
	}
	if err := sweepTmp(p.RunDir); err != nil {
		return err
	}
	warnIfOversized(p.RunDir, p.WarnThresholdBytes)
	return nil
}

// scanConversationOrphans removes conversations/<id> dirs with no DB row.
func scanConversationOrphans(ctx context.Context, q *sqlc.Queries, runDir string) error {
	convRoot := filepath.Join(runDir, "conversations")
	entries, err := os.ReadDir(convRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing persisted yet
		}
		return fmt.Errorf("scan orphans: read %q: %w", convRoot, err)
	}
	for _, e := range entries {
		name := e.Name()
		full := filepath.Join(convRoot, name)

		// Symlink guard (D-A5-02): Lstat (does NOT follow) — a symlink entry is
		// removed as a link without ever RemoveAll'ing through it to an external
		// target. This is beyond validateID's string check.
		info, lerr := os.Lstat(full)
		if lerr != nil {
			slog.Warn("orphan scan: lstat failed", "path", full, "err", lerr)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// Unlink the symlink itself; never traverse it.
			if rmErr := os.Remove(full); rmErr != nil {
				slog.Warn("orphan scan: remove symlink failed", "path", full, "err", rmErr)
			}
			continue
		}
		if !info.IsDir() {
			continue // stray file, not a conversation dir — leave it
		}
		// A dir name that is not a clean id cannot match a row; reconcile it away.
		if vErr := validateID("conversation_id", name); vErr != nil {
			removeOrphan(full)
			continue
		}
		exists, err := conversationExists(ctx, q, name)
		if err != nil {
			return fmt.Errorf("scan orphans: existence %q: %w", name, err)
		}
		if !exists {
			removeOrphan(full)
		}
	}
	return nil
}

// conversationExists reports whether a conversations row exists for the id. A
// missing row (pgx.ErrNoRows) is "does not exist", not an error.
func conversationExists(ctx context.Context, q *sqlc.Queries, conversationID string) (bool, error) {
	id, err := parseUUID("id", conversationID)
	if err != nil {
		return false, nil // unparseable → cannot be a live row → treat as orphan
	}
	if _, err := q.GetConversation(ctx, id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// removeOrphan RemoveAll's an orphan dir, WARN-logging a failure (recovered next boot).
func removeOrphan(path string) {
	if err := os.RemoveAll(path); err != nil {
		slog.Warn("orphan scan: remove orphan dir failed (will retry next boot)", "path", path, "err", err)
		return
	}
	slog.Info("orphan scan: removed orphan conversation dir", "path", path)
}

// sweepTmp removes $AURA_RUN_DIR/tmp/* entries older than tmpTTL.
func sweepTmp(runDir string) error {
	tmpRoot := filepath.Join(runDir, "tmp")
	entries, err := os.ReadDir(tmpRoot)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("sweep tmp: read %q: %w", tmpRoot, err)
	}
	cutoff := time.Now().Add(-tmpTTL)
	for _, e := range entries {
		full := filepath.Join(tmpRoot, e.Name())
		info, lerr := os.Lstat(full)
		if lerr != nil {
			slog.Warn("tmp sweep: lstat failed", "path", full, "err", lerr)
			continue
		}
		if info.ModTime().Before(cutoff) {
			if rmErr := os.RemoveAll(full); rmErr != nil {
				slog.Warn("tmp sweep: remove failed", "path", full, "err", rmErr)
			}
		}
	}
	return nil
}

// warnIfOversized logs an audit-only WARN when the run dir exceeds the threshold.
// It NEVER purges (D-A5-02 — operator-driven cleanup is out of scope this phase).
func warnIfOversized(runDir string, threshold int64) {
	if threshold <= 0 {
		threshold = defaultRunDirWarnThreshold
	}
	size, err := dirSize(runDir)
	if err != nil {
		slog.Warn("run-dir size check failed", "path", runDir, "err", err)
		return
	}
	if size > threshold {
		slog.Warn("run-dir over size threshold (audit-only, not purged)",
			"path", runDir, "size_bytes", size, "threshold_bytes", threshold)
	}
}

// dirSize sums the regular-file sizes under root, NOT following symlinks (Lstat).
func dirSize(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // skip unreadable entries — size is best-effort/audit-only
		}
		if d.IsDir() {
			return nil
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		if info.Mode().IsRegular() {
			total += info.Size()
		}
		return nil
	})
	return total, err
}
