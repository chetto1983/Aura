package sweep_test

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/aura/aura/internal/storage/sweep"
)

func TestSweepSessionsEmpty(t *testing.T) {
	base := t.TempDir()
	if err := sweep.SweepSessions(base); err != nil {
		t.Fatalf("empty base: unexpected error: %v", err)
	}
}

func TestSweepSessionsNonExistent(t *testing.T) {
	if err := sweep.SweepSessions(filepath.Join(t.TempDir(), "nodir")); err != nil {
		t.Fatalf("nonexistent base: unexpected error: %v", err)
	}
}

// TestSweepSessionsLRU verifies that creating 33 session directories and then
// calling SweepSessions evicts the oldest one, leaving exactly MaxSessions=32.
func TestSweepSessionsLRU(t *testing.T) {
	base := t.TempDir()

	// Create MaxSessions+1 directories with staggered mtimes so we can
	// predict which one is oldest.
	now := time.Now()
	for i := 0; i <= sweep.MaxSessions; i++ {
		dir := filepath.Join(base, "session-"+strconv.Itoa(i))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("mkdir session-%d: %v", i, err)
		}
		// session-0 is oldest (furthest in the past within MaxAge).
		mtime := now.Add(-time.Duration(sweep.MaxSessions-i+1) * time.Minute)
		if err := os.Chtimes(dir, mtime, mtime); err != nil {
			t.Fatalf("chtimes session-%d: %v", i, err)
		}
	}

	entries, _ := os.ReadDir(base)
	if len(entries) != sweep.MaxSessions+1 {
		t.Fatalf("expected %d dirs before sweep, got %d", sweep.MaxSessions+1, len(entries))
	}

	if err := sweep.SweepSessions(base); err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}

	entries, _ = os.ReadDir(base)
	if len(entries) != sweep.MaxSessions {
		t.Fatalf("expected %d dirs after sweep, got %d", sweep.MaxSessions, len(entries))
	}
	// session-0 (oldest) must be gone.
	if _, err := os.Stat(filepath.Join(base, "session-0")); !os.IsNotExist(err) {
		t.Fatal("expected oldest session-0 to be evicted, but it still exists")
	}
	// session-32 (newest) must survive.
	if _, err := os.Stat(filepath.Join(base, "session-"+strconv.Itoa(sweep.MaxSessions))); err != nil {
		t.Fatalf("expected newest session-%d to survive: %v", sweep.MaxSessions, err)
	}
}

// TestSweepSessionsTTL verifies that a directory older than MaxAge is evicted
// regardless of session count.
func TestSweepSessionsTTL(t *testing.T) {
	base := t.TempDir()

	// One fresh directory and one stale one.
	fresh := filepath.Join(base, "fresh")
	stale := filepath.Join(base, "stale")
	for _, d := range []string{fresh, stale} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	oldTime := time.Now().Add(-(sweep.MaxAge + 24*time.Hour))
	if err := os.Chtimes(stale, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes stale: %v", err)
	}

	if err := sweep.SweepSessions(base); err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}

	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("expected stale directory to be evicted, but it still exists")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatalf("expected fresh directory to survive: %v", err)
	}
}

// TestSweepSessionsFilesIgnored confirms that plain files in baseDir are not
// treated as session directories and are left untouched.
func TestSweepSessionsFilesIgnored(t *testing.T) {
	base := t.TempDir()
	file := filepath.Join(base, "readme.txt")
	if err := os.WriteFile(file, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := sweep.SweepSessions(base); err != nil {
		t.Fatalf("SweepSessions: %v", err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatalf("plain file should be untouched: %v", err)
	}
}
