package tools

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/identityctx"
)

func mediaCtx(t *testing.T, runDir string) context.Context {
	t.Helper()
	return WithToolCallContext(identityctx.WithIdentityID(context.Background(), "owner-1"), "thread", "call-image", runDir, 8192)
}

func stagedMediaDirs(t *testing.T, runDir string) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(runDir, "tmp"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var dirs []string
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "media-") {
			dirs = append(dirs, entry.Name())
		}
	}
	return dirs
}

// The extension must round-trip through guessDeliveryMIME: ingestForDelivery types the
// stored asset from the staged filename, so a staged .jfif would store image/jpeg only
// on hosts whose MIME table happens to agree.
func TestMediaDeliveryStagesEachGeneratedTypeUnderTheRunDirectory(t *testing.T) {
	for mimeType, wantName := range map[string]string{
		"image/png":     "generated.png",
		"image/jpeg":    "generated.jpg",
		"image/webp":    "generated.webp",
		"image/svg+xml": "generated.svg",
	} {
		t.Run(mimeType, func(t *testing.T) {
			runDir := t.TempDir()
			data := []byte("bytes of " + mimeType)
			path, filename, err := stageMedia(mediaCtx(t, runDir), data, mimeType)
			if err != nil {
				t.Fatalf("stageMedia: %v", err)
			}
			if filename != wantName || filepath.Base(path) != wantName {
				t.Fatalf("staged %q as %q, want fixed basename %q", path, filename, wantName)
			}
			if got := guessDeliveryMIME(filename); got != mimeType {
				t.Fatalf("guessDeliveryMIME(%q) = %q, want %q", filename, got, mimeType)
			}
			root, err := filepath.EvalSymlinks(filepath.Join(runDir, "tmp"))
			if err != nil {
				t.Fatal(err)
			}
			if dir := filepath.Dir(filepath.Dir(path)); dir != root {
				t.Fatalf("staged under %q, want a media- directory directly under %q", filepath.Dir(path), root)
			}
			if !strings.HasPrefix(filepath.Base(filepath.Dir(path)), "media-") {
				t.Fatalf("staging directory %q lacks the media- prefix", filepath.Dir(path))
			}
			got, err := os.ReadFile(path)
			if err != nil || string(got) != string(data) {
				t.Fatalf("staged bytes = %q, %v; want %q", got, err, data)
			}
			if runtime.GOOS == "windows" {
				return
			}
			fileInfo, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			dirInfo, err := os.Stat(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			if fileInfo.Mode().Perm() != 0o600 || dirInfo.Mode().Perm() != 0o700 {
				t.Fatalf("modes file=%v dir=%v, want 0600 and 0700", fileInfo.Mode().Perm(), dirInfo.Mode().Perm())
			}
		})
	}
}

func TestMediaDeliveryRefusesAnUnknownTypeWithoutCreatingAnything(t *testing.T) {
	runDir := t.TempDir()
	if _, _, err := stageMedia(mediaCtx(t, runDir), []byte("<html>"), "text/html"); err == nil {
		t.Fatal("stageMedia accepted a type generation never returns")
	}
	if dirs := stagedMediaDirs(t, runDir); len(dirs) != 0 {
		t.Fatalf("refused staging left %v behind", dirs)
	}
}

func TestMediaDeliveryRefusesAnUnusableRunDirectory(t *testing.T) {
	file := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(file, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	tmpIsFile := t.TempDir()
	if err := os.WriteFile(filepath.Join(tmpIsFile, "tmp"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := map[string]context.Context{
		"no tool-call context": identityctx.WithIdentityID(context.Background(), "owner-1"),
		"empty run directory":  mediaCtx(t, ""),
		"relative run dir":     mediaCtx(t, filepath.Join("relative", "run")),
		"missing run dir":      mediaCtx(t, filepath.Join(t.TempDir(), "gone")),
		"run dir is a file":    mediaCtx(t, file),
		"tmp is a file":        mediaCtx(t, tmpIsFile),
	}
	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			if _, _, err := stageMedia(ctx, []byte("png"), "image/png"); err == nil {
				t.Fatal("stageMedia staged without a usable run directory")
			}
		})
	}
	if _, err := os.Stat(filepath.Join("relative", "run")); !os.IsNotExist(err) {
		t.Fatalf("a relative run directory was created under the working directory: %v", err)
	}
}

func TestMediaDeliveryRefusesAStagingRootThatEscapesTheRunDirectory(t *testing.T) {
	runDir := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(runDir, "tmp")); err != nil {
		t.Skipf("symlinks unavailable on this platform: %v", err)
	}
	if _, _, err := stageMedia(mediaCtx(t, runDir), []byte("png"), "image/png"); err == nil {
		t.Fatal("stageMedia followed a tmp symlink out of the run directory")
	}
	entries, err := os.ReadDir(outside)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("staging wrote %d entries outside the run directory", len(entries))
	}
}

func TestMediaDeliveryPreflightNeedsIdentityThreadCallAndRunDirectory(t *testing.T) {
	runDir := t.TempDir()
	base := identityctx.WithIdentityID(context.Background(), "owner-1")
	cases := map[string]context.Context{
		"no identity":  WithToolCallContext(context.Background(), "thread", "call", runDir, 8192),
		"no thread":    WithToolCallContext(base, "", "call", runDir, 8192),
		"no tool call": WithToolCallContext(base, "thread", "", runDir, 8192),
		"no run dir":   WithToolCallContext(base, "thread", "call", "", 8192),
		"no tool ctx":  base,
		"missing root": WithToolCallContext(base, "thread", "call", filepath.Join(runDir, "gone"), 8192),
	}
	for name, ctx := range cases {
		t.Run(name, func(t *testing.T) {
			if owner, ok := mediaDeliveryOwner(ctx); ok || owner != "" {
				t.Fatalf("mediaDeliveryOwner = %q, %v; want refusal", owner, ok)
			}
		})
	}
	owner, ok := mediaDeliveryOwner(WithToolCallContext(base, "thread", "call", runDir, 8192))
	if !ok || owner != "owner-1" {
		t.Fatalf("mediaDeliveryOwner = %q, %v; want owner-1", owner, ok)
	}
}

func TestMediaDeliveryArtifactResultCarriesTheDescriptorAndPreview(t *testing.T) {
	ctx := mediaCtx(t, t.TempDir())
	preview := map[string]any{"asset_id": "asset-9", "adjustments": []string{}}
	res := mediaArtifactResult(ctx, "/run/tmp/media-1/generated.png", "generated.png", "image/png", "asset-9", "città 🌙", 42, preview)

	want := map[string]any{
		"path": "/run/tmp/media-1/generated.png", "filename": "generated.png", "mime_type": "image/png",
		"asset_id": "asset-9", "caption": "città 🌙", "tool_call_id": "call-image", "size_bytes": int64(42),
	}
	got := artifactMap(t, res)
	if len(got) != len(want) {
		t.Fatalf("descriptor = %#v, want exactly %#v", got, want)
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("descriptor[%q] = %#v, want %#v", key, got[key], value)
		}
	}
	if res.Preview != `{"adjustments":[],"asset_id":"asset-9"}` || res.Bytes != len(res.Preview) {
		t.Fatalf("preview = %q (%d bytes)", res.Preview, res.Bytes)
	}
}

func TestMediaDeliveryArtifactResultRefusesAnUnencodablePreview(t *testing.T) {
	res := mediaArtifactResult(mediaCtx(t, t.TempDir()), "/p", "generated.png", "image/png", "a", "p", 1, func() {})
	if res.Meta != nil {
		t.Fatal("an unencodable preview still produced an artifact")
	}
	var body map[string]string
	if err := json.Unmarshal([]byte(res.Preview), &body); err != nil || body["error"] != "job_failed" {
		t.Fatalf("preview = %q, want a job_failed error result", res.Preview)
	}
}
