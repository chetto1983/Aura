package retention

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestLocalArtifactsDeletesOwnedEntryAndNeverScansTempo(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-48 * time.Hour)
	writeOldFile(t, filepath.Join(root, "tmp", "expired.bin"), old)
	writeOldFile(t, filepath.Join(root, "tempo", "blocks", "keep.bin"), old)
	adapter := LocalArtifacts{Root: root, IdentityID: "local"}
	policy := DefaultPolicy(EnvironmentProduction)
	candidates, err := adapter.Candidates(context.Background(), policy, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 1 || candidates[0].ArtifactID != "expired.bin" {
		t.Fatalf("candidates = %+v", candidates)
	}
	if _, err := adapter.Remove(context.Background(), candidates[0]); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "tmp", "expired.bin")); !os.IsNotExist(err) {
		t.Fatalf("expired file still present: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "tempo", "blocks", "keep.bin")); err != nil {
		t.Fatalf("Tempo block changed: %v", err)
	}
}

func TestLocalArtifactsRejectsSymlinkReplacement(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows test environments may not permit symlink creation")
	}
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "outside.bin")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "tmp", "candidate.bin")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	adapter := LocalArtifacts{Root: root, IdentityID: "local"}
	candidate := Candidate{IdentityID: "local", ArtifactID: "candidate.bin", Version: 1, Action: ActionDeleteArtifact, Class: ClassTemporary}
	if _, err := adapter.Revalidate(context.Background(), candidate); err == nil {
		t.Fatal("symlink revalidation succeeded")
	}
	if _, err := adapter.Remove(context.Background(), candidate); err == nil {
		t.Fatal("symlink removal succeeded")
	}
	if body, err := os.ReadFile(target); err != nil || string(body) != "keep" {
		t.Fatalf("external target changed: %q, %v", body, err)
	}
}

func writeOldFile(t *testing.T, path string, modTime time.Time) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, modTime, modTime); err != nil {
		t.Fatal(err)
	}
}
