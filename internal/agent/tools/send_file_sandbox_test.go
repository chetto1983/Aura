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

// send_file_sandbox_test.go unit-proves stageBoxArtifact's extraction WITHOUT a daemon: the
// zipslip guard (a traversal-shaped tar entry is basename-reduced and never escapes the staging
// dir), the non-regular skip, and the empty-stream error. The live copy-out is the
// docker_integration tier's job (shell_exec_sandbox_docker_test.go).

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
	if filepath.Base(staged) != "report.xlsx" {
		t.Fatalf("staged basename = %q, want report.xlsx", filepath.Base(staged))
	}
	if !strings.HasPrefix(staged, stageDir) {
		t.Fatalf("staged %q not under stageDir %q", staged, stageDir)
	}
	got, err := os.ReadFile(staged)
	if err != nil || string(got) != "XLSXDATA" {
		t.Fatalf("staged content = %q (err %v), want XLSXDATA", got, err)
	}
}

// TestStageBoxArtifact_ZipslipContained proves a traversal-shaped entry cannot escape: the name
// is basename-reduced and the staged file stays strictly under the staging dir.
func TestStageBoxArtifact_ZipslipContained(t *testing.T) {
	t.Parallel()
	for _, evil := range []string{"../../etc/passwd", "/etc/shadow", "../escape.txt"} {
		stageDir, staged, err := stageBoxArtifact(stageCtx(t), tarStream(t, tarEntry{evil, tar.TypeReg, "x"}), "fallback.bin")
		if err != nil {
			t.Fatalf("stageBoxArtifact(%q): %v", evil, err)
		}
		rel, rerr := filepath.Rel(stageDir, staged)
		if rerr != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
			t.Fatalf("entry %q escaped staging dir: staged=%q rel=%q", evil, staged, rel)
		}
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
	if filepath.Base(staged) != "data.txt" {
		t.Fatalf("staged = %q, want data.txt", filepath.Base(staged))
	}

	// A stream with no regular file is a clean error.
	if _, _, err := stageBoxArtifact(stageCtx(t), tarStream(t, tarEntry{"onlydir/", tar.TypeDir, ""}), "fallback.bin"); err == nil {
		t.Fatal("stageBoxArtifact with no regular file: want error, got nil")
	}
}
