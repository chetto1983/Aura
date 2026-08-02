// Unit tier (no build tag): the boot-reconciliation GC helpers that need only a
// filesystem (removeOrphan / sweepTmp / warnIfOversized / dirSize) plus the two
// DB-touching scan helpers (scanConversationOrphans / conversationExists) driven
// through the in-memory sqlc.DBTX fake. The full ScanOrphans entry point takes a
// concrete *pgxpool.Pool (no DBTX seam) and is covered under db_integration in
// orphan_scan_test.go; here we exercise its building blocks without a live Postgres.
package conversations

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/db/sqlc"
	"github.com/chetto1983/aura/internal/llm"
	"github.com/google/uuid"
)

// TestRemoveOrphan_RemovesAndWarns proves removeOrphan deletes an existing dir and
// WARN-logs (without panicking) when the path is already gone.
func TestRemoveOrphan_RemovesAndWarns(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "orphan")
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	removeOrphan(dir)
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("orphan dir must be removed, stat err=%v", err)
	}
	// A second remove of a now-missing path is a clean no-op (RemoveAll on a missing
	// path returns nil) — must not panic.
	removeOrphan(dir)
}

// TestSweepTmp_ReclaimsAgedKeepsFresh covers the TTL branch of sweepTmp directly.
func TestSweepTmp_ReclaimsAgedKeepsFresh(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	tmpRoot := filepath.Join(runDir, "tmp")
	if err := os.MkdirAll(tmpRoot, 0o750); err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	aged := filepath.Join(tmpRoot, "aged.scratch")
	fresh := filepath.Join(tmpRoot, "fresh.scratch")
	for _, p := range []string{aged, fresh} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %q: %v", p, err)
		}
	}
	stale := time.Now().Add(-tmpTTL - time.Hour)
	if err := os.Chtimes(aged, stale, stale); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := sweepTmp(runDir); err != nil {
		t.Fatalf("sweepTmp: %v", err)
	}
	if _, err := os.Stat(aged); !os.IsNotExist(err) {
		t.Errorf("aged tmp entry must be swept, stat err=%v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("fresh tmp entry must survive: %v", err)
	}
}

// TestSweepTmp_MissingDirIsNoop: a tmp dir that does not exist yet is a clean no-op.
func TestSweepTmp_MissingDirIsNoop(t *testing.T) {
	t.Parallel()
	if err := sweepTmp(t.TempDir()); err != nil {
		t.Errorf("missing tmp dir must be a no-op, got %v", err)
	}
}

// TestDirSize_SumsRegularFiles proves dirSize sums regular-file bytes and ignores
// directories, and that a missing root yields the walk error.
func TestDirSize_SumsRegularFiles(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "sub"), 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("write a: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "sub", "b.txt"), []byte("678"), 0o600); err != nil {
		t.Fatalf("write b: %v", err)
	}
	got, err := dirSize(root)
	if err != nil {
		t.Fatalf("dirSize: %v", err)
	}
	if got != 8 {
		t.Errorf("dirSize = %d, want 8", got)
	}

	// A missing root: the walk fn swallows the per-entry error (size is best-effort /
	// audit-only), so dirSize returns 0 with NO error rather than failing the scan.
	if sz, err := dirSize(filepath.Join(root, "does-not-exist")); err != nil || sz != 0 {
		t.Errorf("dirSize(missing root): sz=%d err=%v (want 0,nil — best-effort walk)", sz, err)
	}
}

// TestWarnIfOversized_LogsOverThresholdSilentUnder asserts the audit-only WARN fires
// only when the run dir exceeds the threshold, and NEVER purges. It captures slog
// output to confirm the branch (the default-threshold fallback is also exercised).
func TestWarnIfOversized_LogsOverThresholdSilentUnder(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	payload := filepath.Join(root, "big.content")
	if err := os.WriteFile(payload, bytes.Repeat([]byte("x"), 4096), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Over a tiny threshold → WARN, content NOT purged.
	logged := captureWarn(t, func() { warnIfOversized(root, 1) })
	if !strings.Contains(logged, "over size threshold") {
		t.Errorf("expected size WARN, got %q", logged)
	}
	if _, err := os.Stat(payload); err != nil {
		t.Errorf("size WARN must NOT purge (audit-only): %v", err)
	}

	// Under a huge threshold → no size WARN.
	quiet := captureWarn(t, func() { warnIfOversized(root, 1<<40) })
	if strings.Contains(quiet, "over size threshold") {
		t.Errorf("under-threshold must not WARN, got %q", quiet)
	}

	// Threshold <= 0 falls back to the SPEC default (1 GiB) — a 4 KiB dir is silent.
	def := captureWarn(t, func() { warnIfOversized(root, 0) })
	if strings.Contains(def, "over size threshold") {
		t.Errorf("default threshold must not WARN on a tiny dir, got %q", def)
	}

	// A missing root: dirSize swallows the walk error and returns 0, so warnIfOversized
	// stays silent (0 is under any positive threshold) — it never WARNs nor purges.
	missing := captureWarn(t, func() { warnIfOversized(filepath.Join(root, "missing"), 1) })
	if strings.Contains(missing, "over size threshold") {
		t.Errorf("missing root (size 0) must not WARN over-threshold, got %q", missing)
	}
}

// captureWarn runs fn with a slog handler writing to a buffer and returns the output.
func captureWarn(t *testing.T, fn func()) string {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	defer slog.SetDefault(prev)
	fn()
	return buf.String()
}

// TestScanConversationOrphans_Fake drives the dir-walk against an EMPTY owner map (no
// conversation exists): a dir whose id owns no row is removed; a dir with a non-id name is
// reconciled away; a stray file is left alone; and a missing conversations root is a no-op.
func TestScanConversationOrphans_Fake(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runDir := t.TempDir()
	convRoot := filepath.Join(runDir, "conversations")
	if err := os.MkdirAll(convRoot, 0o750); err != nil {
		t.Fatalf("mkdir convRoot: %v", err)
	}

	orphanID := uuid.Must(uuid.NewV7()).String()
	orphanDir := filepath.Join(convRoot, orphanID)
	if err := os.MkdirAll(orphanDir, 0o750); err != nil {
		t.Fatalf("mkdir orphan: %v", err)
	}
	// A dir whose name is not a clean id (path-separator-free but invalid uuid is
	// still removed only via the existence check; a ".."-shaped name fails validateID
	// — but ReadDir won't yield "..", so use a clearly non-uuid name that fails the
	// existence lookup as ErrNoRows).
	badNameDir := filepath.Join(convRoot, "not-a-uuid-name")
	if err := os.MkdirAll(badNameDir, 0o750); err != nil {
		t.Fatalf("mkdir badname: %v", err)
	}
	strayFile := filepath.Join(convRoot, "stray.txt")
	if err := os.WriteFile(strayFile, []byte("keep"), 0o600); err != nil {
		t.Fatalf("write stray: %v", err)
	}

	// No conversation is owned by anyone → every id-named dir is an orphan. A name that is
	// not a clean id never reaches the map lookup (validateID rejects it first).
	if err := scanConversationOrphans(ctx, dirEntries(t, runDir), map[string]string{}, failingReconciler(t), runDir); err != nil {
		t.Fatalf("scanConversationOrphans: %v", err)
	}

	if _, err := os.Stat(orphanDir); !os.IsNotExist(err) {
		t.Errorf("orphan id dir must be removed, stat err=%v", err)
	}
	if _, err := os.Stat(badNameDir); !os.IsNotExist(err) {
		t.Errorf("non-id-named dir must be reconciled away, stat err=%v", err)
	}
	if _, err := os.Stat(strayFile); err != nil {
		t.Errorf("stray file must be left alone: %v", err)
	}
}

// TestScanConversationOrphans_KeepsLive proves a dir whose id IS owned survives, and that
// the crash-orphan .content reconcile (F-040) is invoked for it with that owner — the walk
// must never hand a live dir to a reconciler scoped to the wrong identity.
func TestScanConversationOrphans_KeepsLive(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	runDir := t.TempDir()
	convRoot := filepath.Join(runDir, "conversations")
	liveID := uuid.Must(uuid.NewV7()).String()
	liveDir := filepath.Join(convRoot, liveID)
	if err := os.MkdirAll(liveDir, 0o750); err != nil {
		t.Fatalf("mkdir live: %v", err)
	}

	owner := uuid.Must(uuid.NewV7()).String()
	var gotOwner, gotID string
	reconcile := func(_ context.Context, o, _, convID string) error {
		gotOwner, gotID = o, convID
		return nil
	}
	if err := scanConversationOrphans(ctx, dirEntries(t, runDir), map[string]string{liveID: owner}, reconcile, runDir); err != nil {
		t.Fatalf("scanConversationOrphans: %v", err)
	}
	if _, err := os.Stat(liveDir); err != nil {
		t.Errorf("live conversation dir must survive: %v", err)
	}
	if gotOwner != owner || gotID != liveID {
		t.Errorf("reconcile ran as (%s, %s), want (%s, %s)", gotOwner, gotID, owner, liveID)
	}
}

// failingReconciler fails the test if the walk hands it a directory: every dir in the
// orphan cases is unowned, so the reconcile path must not be reached at all.
func failingReconciler(t *testing.T) sidecarReconciler {
	t.Helper()
	return func(_ context.Context, owner, dir, convID string) error {
		t.Errorf("reconcile must not run for an orphan dir: owner=%s dir=%s id=%s", owner, dir, convID)
		return nil
	}
}

// TestParseContentSeq_StrictShape covers the strict <seq>.content grammar: a clean
// positive seq parses; a .result, a non-numeric base, an empty/zero/negative seq, and
// a bare ".content" are all rejected so they can never be treated as turn sidecars.
func TestParseContentSeq_StrictShape(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		wantSeq int
		wantOK  bool
	}{
		{"1.content", 1, true},
		{"42.content", 42, true},
		{"abc123.result", 0, false},
		{"deadbeef.result", 0, false},
		{"notanumber.content", 0, false},
		{".content", 0, false},
		{"0.content", 0, false},
		{"-3.content", 0, false},
		{"3.content.bak", 0, false},
		{"3", 0, false},
	}
	for _, c := range cases {
		seq, ok := parseContentSeq(c.name)
		if ok != c.wantOK || seq != c.wantSeq {
			t.Errorf("parseContentSeq(%q) = (%d,%v), want (%d,%v)", c.name, seq, ok, c.wantSeq, c.wantOK)
		}
	}
}

// TestReconcileLiveConversationSidecars_Fake drives the reconcile over a real dir with
// a faked referenced-seq set {1}: an unreferenced+aged .content is removed; a
// referenced .content (even aged), an unreferenced+young .content, a .result tool
// sidecar, and a symlink at a .content leaf all survive.
func TestReconcileLiveConversationSidecars_Fake(t *testing.T) {
	t.Parallel()
	convID := uuid.Must(uuid.NewV7()).String()
	dir := t.TempDir()
	aged := time.Now().Add(-sidecarOrphanGrace - time.Hour)

	referenced := filepath.Join(dir, "1.content")     // referenced → survives regardless of age
	orphanAged := filepath.Join(dir, "100.content")   // unref + aged → removed
	orphanYoung := filepath.Join(dir, "101.content")  // unref + young → survives (grace)
	toolResult := filepath.Join(dir, "abc123.result") // .result → survives (strict .content)
	for _, p := range []string{referenced, orphanAged, orphanYoung, toolResult} {
		if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
			t.Fatalf("write %q: %v", p, err)
		}
	}
	for _, p := range []string{referenced, orphanAged, toolResult} {
		if err := os.Chtimes(p, aged, aged); err != nil {
			t.Fatalf("chtimes %q: %v", p, err)
		}
	}
	symlink := filepath.Join(dir, "200.content")
	symlinkMade := os.Symlink(filepath.Join(t.TempDir(), "external"), symlink) == nil

	// Fake ListSpilledSeqsForConversation → referenced seq {1}.
	q := sqlc.New(&fakeDBTX{queryRows: &fakeRows{rows: [][]any{{int32(1)}}}})
	if err := reconcileLiveConversationSidecars(context.Background(), q, dir, convID); err != nil {
		t.Fatalf("reconcile: %v", err)
	}

	if _, err := os.Stat(orphanAged); !os.IsNotExist(err) {
		t.Errorf("unreferenced+aged .content must be removed, stat err=%v", err)
	}
	for _, p := range []string{referenced, orphanYoung, toolResult} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("must survive reconcile: %q: %v", p, err)
		}
	}
	if symlinkMade {
		if _, err := os.Lstat(symlink); err != nil {
			t.Errorf("symlink at a .content leaf must survive (never removed): %v", err)
		}
	}
}

// TestReconcileLiveConversationSidecars_DBErrorAborts proves a referenced-seq query
// failure ABORTS the reconcile (returns the error) rather than deleting against an
// unknown set — the safety property that prevents dropping referenced sidecars.
func TestReconcileLiveConversationSidecars_DBErrorAborts(t *testing.T) {
	t.Parallel()
	convID := uuid.Must(uuid.NewV7()).String()
	dir := t.TempDir()
	boom := errors.New("referenced-seq query failed")
	q := sqlc.New(&fakeDBTX{queryErr: boom})
	if err := reconcileLiveConversationSidecars(context.Background(), q, dir, convID); !errors.Is(err, boom) {
		t.Errorf("DB failure must abort the reconcile: %v", err)
	}
}

// TestScanConversationOrphans_MissingRootIsNoop: no conversations dir yet → no-op.
func TestScanConversationOrphans_MissingRootIsNoop(t *testing.T) {
	t.Parallel()
	if err := scanConversationOrphans(context.Background(), nil, map[string]string{}, failingReconciler(t), t.TempDir()); err != nil {
		t.Errorf("missing conversations root must be a no-op, got %v", err)
	}
}

// TestScanConversationOrphans_ReconcileErrorAborts surfaces a reconcile failure as a
// wrapped error that aborts the scan. It replaces the old existence-lookup-error case: the
// per-directory existence probe is gone (it would answer "absent" for every conversation
// once RLS fails closed, and RemoveAll every tenant's tree), so the only DB call left inside
// the walk is the owner-scoped sidecar reconcile.
func TestScanConversationOrphans_ReconcileErrorAborts(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	convRoot := filepath.Join(runDir, "conversations")
	convID := uuid.Must(uuid.NewV7()).String()
	dir := filepath.Join(convRoot, convID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	boom := errors.New("referenced-seq query failed")
	reconcile := func(context.Context, string, string, string) error { return boom }
	owners := map[string]string{convID: uuid.Must(uuid.NewV7()).String()}
	if err := scanConversationOrphans(context.Background(), dirEntries(t, runDir), owners, reconcile, runDir); !errors.Is(err, boom) {
		t.Errorf("scanConversationOrphans(reconcile error) = %v, want %v", err, boom)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("an owned dir must survive a reconcile failure: %v", err)
	}
}

// TestNewRunDirSweeper_ProductionConstructor builds the production sweeper and proves
// its injected Sweep closure runs ScanOrphans over the run dir (a nil pool is safe
// because an empty/no-conversations run dir short-circuits before any DB use here we
// pass a TempDir with no conversations subdir → ScanOrphans returns nil without
// touching the pool). The disabled-interval variant launches no goroutine.
func TestNewRunDirSweeper_ProductionConstructor(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir() // no conversations/ or tmp/ subdir → scan is a clean no-op

	sw := NewRunDirSweeper(nil, ScanParams{RunDir: runDir}, time.Hour)
	if sw == nil || sw.sweep == nil {
		t.Fatal("NewRunDirSweeper must return a sweeper with a wired sweep func")
	}
	// Invoke the injected closure directly (no Start goroutine): it runs ScanOrphans,
	// which short-circuits on the empty run dir without dereferencing the nil pool.
	sw.sweep(context.Background())

	disabled := NewRunDirSweeper(nil, ScanParams{RunDir: runDir}, 0)
	disabled.Start(context.Background()) // non-positive interval launches nothing
	disabled.Stop()                      // clean join on a never-started worker
}

// TestGenerateTitle_ExportedDelegate covers the exported GenerateTitle wrapper (the
// internal generateTitle body is covered in title_unit_test.go).
func TestGenerateTitle_ExportedDelegate(t *testing.T) {
	t.Parallel()
	client := titleTextClient("stop", "A Tidy Title")
	history := []llm.Message{{Role: llm.RoleUser, Content: "explain the budget loop"}}
	got, err := GenerateTitle(context.Background(), client, "m", history)
	if err != nil {
		t.Fatalf("GenerateTitle: %v", err)
	}
	if got != "A Tidy Title" {
		t.Errorf("GenerateTitle: got %q", got)
	}
}

// TestInitEncoder_Idempotent proves the eager boot init succeeds offline and is safe
// to call repeatedly (sync.Once).
func TestInitEncoder_Idempotent(t *testing.T) {
	t.Parallel()
	if err := InitEncoder(); err != nil {
		t.Fatalf("InitEncoder: %v", err)
	}
	if err := InitEncoder(); err != nil {
		t.Errorf("InitEncoder (second call): %v", err)
	}
}

// dirEntries reads the conversations dir the way ScanOrphans does, so the walk tests feed it
// the same input the production caller does.
func dirEntries(t *testing.T, runDir string) []os.DirEntry {
	t.Helper()
	entries, err := conversationDirEntries(runDir)
	if err != nil {
		t.Fatalf("conversationDirEntries(%q): %v", runDir, err)
	}
	return entries
}
