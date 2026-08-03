package tools

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// send_file_sandbox_test.go unit-proves stageBoxArtifact's extraction WITHOUT a daemon: archive
// names never influence the staged path, alongside the non-regular skip and empty-stream error.
// The live copy-out is the docker_integration tier's job (shell_exec_sandbox_docker_test.go).

// tarStream builds a tar of the given (name, typeflag, body) entries — the CopyArtifactsOut wire
// shape stageBoxArtifact consumes.
func tarStream(t *testing.T, entries ...tarEntry) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, e := range entries {
		hdr := &tar.Header{Name: e.name, Typeflag: e.typ, Mode: 0o644, Size: int64(len(e.body))}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatalf("tar header %q: %v", e.name, err)
		}
		if len(e.body) > 0 {
			if _, err := tw.Write([]byte(e.body)); err != nil {
				t.Fatalf("tar write %q: %v", e.name, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("tar close: %v", err)
	}
	return &buf
}

type tarEntry struct {
	name string
	typ  byte
	body string
}

func stageCtx(t *testing.T) context.Context {
	t.Helper()
	return ctxWithRunDir("sess-stage", "call-stage", t.TempDir())
}

// TestStageBoxArtifact_ExtractsRegularFile covers the happy path: the first regular entry is
// staged under a fresh dir with its content intact.
func TestStageBoxArtifact_ExtractsRegularFile(t *testing.T) {
	t.Parallel()
	stageDir, staged, err := stageBoxArtifact(stageCtx(t), tarStream(t, tarEntry{"report.xlsx", tar.TypeReg, "XLSXDATA"}), "fallback.bin")
	if err != nil {
		t.Fatalf("stageBoxArtifact: %v", err)
	}
	if filepath.Base(staged) != "fallback.bin" {
		t.Fatalf("staged basename = %q, want fallback.bin", filepath.Base(staged))
	}
	if !strings.HasPrefix(staged, stageDir) {
		t.Fatalf("staged %q not under stageDir %q", staged, stageDir)
	}
	got, err := os.ReadFile(staged)
	if err != nil || string(got) != "XLSXDATA" {
		t.Fatalf("staged content = %q (err %v), want XLSXDATA", got, err)
	}
	info, err := os.Stat(staged)
	if err != nil {
		t.Fatalf("stat staged file: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("staged mode = %v, want 0600", info.Mode().Perm())
	}
}

// Staging under $AURA_RUN_DIR/tmp is what makes a delivered artifact reclaimable: that subtree is
// the one conversations.ScanOrphans sweeps (24h TTL, at boot and on every sweeper tick). Staging at
// the run-dir ROOT, as this did while only the strict profile routed, left a full copy of every
// delivered file there forever — and with one execution path every delivery stages. The staged copy
// cannot simply be deleted when Execute returns: the descriptor hands the channel a PATH it opens
// later, so an eager delete would deliver an empty file.
func TestStageBoxArtifact_StagesUnderTheSweptTmpRoot(t *testing.T) {
	t.Parallel()
	runDir := t.TempDir()
	ctx := ctxWithRunDir("sess-tmp", "call-tmp", runDir)

	stageDir, staged, err := stageBoxArtifact(ctx, tarStream(t, tarEntry{"r.xlsx", tar.TypeReg, "X"}), "fallback.bin")
	if err != nil {
		t.Fatalf("stageBoxArtifact: %v", err)
	}
	tmpRoot := filepath.Join(runDir, "tmp")
	if !strings.HasPrefix(stageDir, tmpRoot+string(os.PathSeparator)) {
		t.Fatalf("staging dir %q is not under the swept root %q — nothing will ever reclaim it", stageDir, tmpRoot)
	}
	if !strings.HasPrefix(staged, stageDir+string(os.PathSeparator)) {
		t.Fatalf("staged file %q escaped its staging dir %q", staged, stageDir)
	}
}

// With no run dir configured (the manifest/CLI contexts) staging falls back to the OS temp dir,
// which the OS reclaims — never to the process working directory.
func TestStageBoxArtifact_NoRunDirFallsBackToOSTemp(t *testing.T) {
	t.Parallel()
	stageDir, _, err := stageBoxArtifact(context.Background(), tarStream(t, tarEntry{"r.txt", tar.TypeReg, "X"}), "fallback.bin")
	if err != nil {
		t.Fatalf("stageBoxArtifact: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stageDir) })
	if !strings.HasPrefix(stageDir, os.TempDir()) {
		t.Fatalf("staging dir %q is not under the OS temp dir %q", stageDir, os.TempDir())
	}
}

// TestStageBoxArtifact_ArchiveNameNeverReachesTheHostPath proves a traversal-shaped entry can
// contribute bytes but never its attacker-controlled name.
func TestStageBoxArtifact_ArchiveNameNeverReachesTheHostPath(t *testing.T) {
	t.Parallel()
	for _, evil := range []string{"../../etc/passwd", "/etc/shadow", "../escape.txt", `..\..\evil.txt`} {
		stageDir, staged, err := stageBoxArtifact(stageCtx(t), tarStream(t, tarEntry{evil, tar.TypeReg, "x"}), "requested.bin")
		if err != nil {
			t.Fatalf("stageBoxArtifact(%q): %v", evil, err)
		}
		if filepath.Base(staged) != "requested.bin" {
			t.Fatalf("entry %q influenced staged path %q", evil, staged)
		}
		rel, rerr := filepath.Rel(stageDir, staged)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			t.Fatalf("entry %q escaped staging dir: staged=%q rel=%q", evil, staged, rel)
		}
	}
}

func TestExtractFirstRegularFileRejectsUnsafeOrExistingDestination(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"", ".", "../escape", "dir/file"} {
		stageDir := t.TempDir()
		stream := tar.NewReader(tarStream(t, tarEntry{"entry", tar.TypeReg, "x"}))
		if _, err := extractFirstRegularFile(stream, stageDir, name); err == nil {
			t.Fatalf("fallback %q: want rejection", name)
		}
	}

	stageDir := t.TempDir()
	stream := func() *tar.Reader {
		return tar.NewReader(tarStream(t, tarEntry{"entry", tar.TypeReg, "x"}))
	}
	if _, err := extractFirstRegularFile(stream(), stageDir, "requested.bin"); err != nil {
		t.Fatalf("first extraction: %v", err)
	}
	if _, err := extractFirstRegularFile(stream(), stageDir, "requested.bin"); err == nil {
		t.Fatal("second extraction overwrote an existing staged file")
	}
}

// TestStageBoxArtifact_SkipsNonRegularAndEmpty covers the non-regular skip (a dir entry before a
// regular one) and the no-regular-file error.
func TestStageBoxArtifact_SkipsNonRegularAndEmpty(t *testing.T) {
	t.Parallel()

	// A directory entry is skipped; the following regular file is staged.
	_, staged, err := stageBoxArtifact(stageCtx(t),
		tarStream(t, tarEntry{"sub/", tar.TypeDir, ""}, tarEntry{"data.txt", tar.TypeReg, "OK"}), "fallback.bin")
	if err != nil {
		t.Fatalf("stageBoxArtifact (dir then file): %v", err)
	}
	if filepath.Base(staged) != "fallback.bin" {
		t.Fatalf("staged = %q, want fallback.bin", filepath.Base(staged))
	}

	// A stream with no regular file is a clean error.
	if _, _, err := stageBoxArtifact(stageCtx(t), tarStream(t, tarEntry{"onlydir/", tar.TypeDir, ""}), "fallback.bin"); err == nil {
		t.Fatal("stageBoxArtifact with no regular file: want error, got nil")
	}
}
