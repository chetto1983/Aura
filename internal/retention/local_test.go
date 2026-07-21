package retention

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
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

func TestLocalArtifactsRevalidatesTrustedOwnedEntries(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "crash", "artifact-id")
	writeOldFile(t, path, time.Now().Add(-48*time.Hour))
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	adapter := LocalArtifacts{Root: root, IdentityID: "owner"}
	candidate := Candidate{
		IdentityID: "owner", ArtifactID: "artifact-id", Version: localVersion(info),
		Action: ActionDeleteArtifact, Class: ClassCrashArtifact, Bytes: info.Size(),
	}
	got, err := adapter.Revalidate(context.Background(), candidate)
	if err != nil || !got.Exists || !got.Owned || got.Version != candidate.Version {
		t.Fatalf("Revalidate() = %+v, %v", got, err)
	}
	foreign := candidate
	foreign.IdentityID = "foreign"
	got, err = adapter.Revalidate(context.Background(), foreign)
	if err != nil || !got.Exists || got.Owned {
		t.Fatalf("foreign Revalidate() = %+v, %v", got, err)
	}
	missing := candidate
	missing.ArtifactID = "absent"
	got, err = adapter.Revalidate(context.Background(), missing)
	if err != nil || got.Exists || !got.Owned {
		t.Fatalf("missing Revalidate() = %+v, %v", got, err)
	}
	if _, err := adapter.Remove(context.Background(), missing); err != nil {
		t.Fatalf("absent Remove() = %v", err)
	}
	if err := adapter.Finalize(context.Background(), candidate); err != nil {
		t.Fatalf("Finalize() = %v", err)
	}
}

func TestLocalArtifactsBoundsAndErrorPaths(t *testing.T) {
	policy := DefaultPolicy(EnvironmentProduction)
	if _, err := (LocalArtifacts{}).Candidates(context.Background(), policy, time.Now()); err == nil {
		t.Fatal("unconfigured adapter enumerated candidates")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (LocalArtifacts{Root: t.TempDir(), IdentityID: "owner"}).Candidates(ctx, policy, time.Now()); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Candidates error = %v", err)
	}
	adapter := LocalArtifacts{Root: t.TempDir(), IdentityID: "owner"}
	unsafe := Candidate{IdentityID: "owner", ArtifactID: "../escape", Action: ActionDeleteArtifact, Class: ClassTemporary}
	if _, err := adapter.Revalidate(context.Background(), unsafe); err == nil {
		t.Fatal("unsafe candidate revalidated")
	}
	if _, err := adapter.Remove(context.Background(), unsafe); err == nil {
		t.Fatal("unsafe candidate removed")
	}
	unsupported := Candidate{IdentityID: "owner", ArtifactID: "artifact", Action: ActionDeleteArtifact, Class: ClassConversation}
	if _, err := adapter.Revalidate(context.Background(), unsupported); err == nil {
		t.Fatal("unsupported class revalidated")
	}
	if _, err := adapter.Remove(ctx, unsupported); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled Remove error = %v", err)
	}
	if nonNegativeSize(-1) != 0 || nonNegativeSize(1) != 1 {
		t.Fatal("nonNegativeSize boundary mismatch")
	}
}

func TestLocalArtifactsEnumeratesEveryPolicyRootAndSkipsFreshOrInvalid(t *testing.T) {
	root := t.TempDir()
	old := time.Now().Add(-20 * 24 * time.Hour)
	for _, relative := range []string{"tmp/temp-id", "crash/crash-id", "traces/full/full-id", "traces/metadata/meta-id"} {
		writeOldFile(t, filepath.Join(root, filepath.FromSlash(relative)), old)
	}
	writeOldFile(t, filepath.Join(root, "tmp", "fresh-id"), time.Now())
	writeOldFile(t, filepath.Join(root, "tmp", strings.Repeat("x", 201)), old)
	adapter := LocalArtifacts{Root: root, IdentityID: "owner"}
	candidates, err := adapter.Candidates(context.Background(), DefaultPolicy(EnvironmentProduction), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 4 {
		t.Fatalf("Candidates() = %+v", candidates)
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
