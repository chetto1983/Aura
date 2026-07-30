package conversations

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/chetto1983/aura/internal/db"
	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// tmpTTL is the age past which $AURA_RUN_DIR/tmp/* entries are swept at boot (SPEC
// Req#12). A scratch artifact older than this is assumed abandoned.
const tmpTTL = 24 * time.Hour

// sidecarOrphanGrace mirrors tmpTTL (D-09 / LOOP-09 / F-040): a <seq>.content file
// whose turn never committed — a crash between the sidecar write and the DB commit —
// is reconciled away only once it is older than this AND unreferenced by any
// committed row. The grace window keeps an in-flight spill (written seconds ago,
// commit still pending) safe from a concurrent scan.
const sidecarOrphanGrace = tmpTTL

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
			continue
		}
		// A LIVE dir: reconcile crash-orphaned <seq>.content files inside it (F-040).
		// Whole-orphan removal above never reaches these — a crash between the sidecar
		// write and the DB commit leaves an unreferenced sidecar in a dir that stays.
		if err := reconcileLiveConversationSidecars(ctx, q, full, name); err != nil {
			return fmt.Errorf("scan orphans: reconcile %q: %w", name, err)
		}
	}
	return nil
}

// reconcileLiveConversationSidecars removes crash-orphaned <seq>.content files from a
// LIVE conversation dir (D-09 / LOOP-09 / F-040): a <seq>.content whose seq is NOT in
// the committed referenced set AND whose mtime predates the grace cutoff. It matches
// STRICTLY the .content suffix — co-located <spillID>.result tool-output sidecars
// (sessionID == conversationID) and any stray file survive — and never follows or
// removes a symlink (Lstat guard, like the whole-dir scan). The referenced set comes
// from the committed rows (ListSpilledSeqsForConversation), never a directory-only
// heuristic; a DB failure ABORTS the reconcile rather than risk deleting a referenced
// sidecar against an unknown/partial set. Per-entry rm/lstat failures are WARN-logged
// and recovered next scan (the same posture as removeOrphan).
func reconcileLiveConversationSidecars(ctx context.Context, q *sqlc.Queries, dir, conversationID string) error {
	id, err := db.ParseUUID("id", conversationID)
	if err != nil {
		return nil // validateID already passed upstream; a non-uuid here cannot be a live row
	}
	seqs, err := q.ListSpilledSeqsForConversation(ctx, id)
	if err != nil {
		return fmt.Errorf("list spilled seqs %s: %w", conversationID, err)
	}
	referenced := make(map[int]struct{}, len(seqs))
	for _, s := range seqs {
		referenced[int(s)] = struct{}{}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		slog.Warn("sidecar reconcile: read live dir failed (retry next scan)", "path", dir, "err", err)
		return nil
	}
	cutoff := time.Now().Add(-sidecarOrphanGrace)
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".content") {
			continue // .result tool sidecars + any stray file are out of scope
		}
		full := filepath.Join(dir, name)
		info, lerr := os.Lstat(full)
		if lerr != nil {
			slog.Warn("sidecar reconcile: lstat failed", "path", full, "err", lerr)
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			continue // never follow/remove a symlink at a .content leaf; skip non-regular
		}
		seq, ok := parseContentSeq(name)
		if !ok {
			continue // not a clean <seq>.content name — leave it
		}
		if _, refd := referenced[seq]; refd {
			continue // a committed turn owns this sidecar (survives regardless of age)
		}
		if info.ModTime().After(cutoff) {
			continue // within the grace window — an in-flight spill may still commit
		}
		if rmErr := os.Remove(full); rmErr != nil {
			slog.Warn("sidecar reconcile: remove failed (retry next scan)", "path", full, "err", rmErr)
			continue
		}
		slog.Info("sidecar reconcile: removed crash-orphaned turn sidecar", "path", full, "seq", seq)
	}
	return nil
}

// parseContentSeq extracts the leading <seq> from a "<seq>.content" filename,
// reporting false for any name that is not exactly that shape — so a <spillID>.result
// tool sidecar or a malformed name is never mistaken for a turn sidecar.
func parseContentSeq(name string) (int, bool) {
	base, ok := strings.CutSuffix(name, ".content")
	if !ok || base == "" {
		return 0, false
	}
	seq, err := strconv.Atoi(base)
	if err != nil || seq <= 0 {
		return 0, false
	}
	return seq, true
}

// conversationExists reports whether a conversations row exists for the id. A
// missing row (pgx.ErrNoRows) is "does not exist", not an error.
func conversationExists(ctx context.Context, q *sqlc.Queries, conversationID string) (bool, error) {
	id, err := db.ParseUUID("id", conversationID)
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
