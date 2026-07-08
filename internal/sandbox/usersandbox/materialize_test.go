package usersandbox

import (
	"archive/tar"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// materialize_test.go is the daemon-free unit tier for the docker-cp bridge's PURE logic: the
// tar builders (tarDir/tarSingleFile — the symlink-escape and path-traversal guards) and
// MaterializeIn's validation branches, all reachable without a live daemon. The happy-path
// CopyToContainer round-trip is the docker_integration tier's job (docker_backend_integration_test.go).

// readTarNames drains a tar stream into a name->content map so a test can assert BOTH the
// dest-rooted entry names and the file bodies survive the build.
func readTarNames(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	out := map[string]string{}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar read %q: %v", hdr.Name, err)
		}
		out[hdr.Name] = string(b)
	}
	return out
}

// TestTarSingleFile covers the one-entry tar builder: the POSIX name clean, the default mode,
// and the traversal-shaped rejections (no escape above the box root).
func TestTarSingleFile(t *testing.T) {
	t.Parallel()

	t.Run("valid paths build a single dest-rooted entry", func(t *testing.T) {
		t.Parallel()
		for _, tc := range []struct {
			boxPath, wantName string
		}{
			{"/workspace/f.txt", "workspace/f.txt"},
			{"workspace/f.txt", "workspace/f.txt"},
			{"/workspace/a/../b.txt", "workspace/b.txt"}, // Clean collapses the interior ..
			// A leading .. does NOT escape: "/"-prefix + Clean clamps it AT the box root
			// ("/../etc/passwd" -> "/etc/passwd" -> "etc/passwd"), never above it.
			{"../etc/passwd", "etc/passwd"},
			{"/../escape", "escape"},
		} {
			r, err := tarSingleFile(tc.boxPath, []byte("payload"), 0o600)
			if err != nil {
				t.Fatalf("tarSingleFile(%q): %v", tc.boxPath, err)
			}
			names := readTarNames(t, r)
			if got, ok := names[tc.wantName]; !ok || got != "payload" {
				t.Fatalf("tarSingleFile(%q) entries = %v, want %q->payload", tc.boxPath, names, tc.wantName)
			}
		}
	})

	t.Run("mode 0 defaults to 0644", func(t *testing.T) {
		t.Parallel()
		r, err := tarSingleFile("/workspace/f.txt", []byte("x"), 0)
		if err != nil {
			t.Fatalf("tarSingleFile: %v", err)
		}
		tr := tar.NewReader(r)
		hdr, err := tr.Next()
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		if hdr.Mode != 0o644 {
			t.Fatalf("default mode = %o, want 0644", hdr.Mode)
		}
	})

	t.Run("traversal-shaped and empty paths are rejected", func(t *testing.T) {
		t.Parallel()
		// Only inputs that Clean to the box root itself (empty name) are rejected; a leading
		// .. is clamped (asserted above), not rejected.
		for _, bad := range []string{"/", "", "   ", ".."} {
			if _, err := tarSingleFile(bad, []byte("x"), 0o644); err == nil {
				t.Fatalf("tarSingleFile(%q): want error, got nil", bad)
			}
		}
	})
}

// TestTarDir covers the tree tar builder: dest-rooting, directory entries, and the symlink /
// non-regular guards that close the materialize escape vector.
func TestTarDir(t *testing.T) {
	t.Parallel()

	t.Run("nested tree is dest-rooted with dir + file entries", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if err := os.MkdirAll(filepath.Join(root, "calc"), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "top.txt"), []byte("TOP"), 0o644); err != nil {
			t.Fatalf("write top: %v", err)
		}
		if err := os.WriteFile(filepath.Join(root, "calc", "calc.py"), []byte("CALC"), 0o644); err != nil {
			t.Fatalf("write calc: %v", err)
		}

		r, err := tarDir(root, "/skills")
		if err != nil {
			t.Fatalf("tarDir: %v", err)
		}
		names := readTarNames(t, r)
		if names["skills/top.txt"] != "TOP" {
			t.Fatalf("missing/incorrect skills/top.txt in %v", names)
		}
		if names["skills/calc/calc.py"] != "CALC" {
			t.Fatalf("missing/incorrect skills/calc/calc.py in %v", names)
		}
		if _, ok := names["skills/calc/"]; !ok {
			t.Fatalf("directory entry skills/calc/ not emitted: %v", names)
		}
	})

	t.Run("symlink is rejected (escape guard)", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")); err != nil {
			t.Skipf("symlink unsupported on this host: %v", err) // Windows without privilege
		}
		if _, err := tarDir(root, "/skills"); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("tarDir with symlink: want symlink-guard error, got %v", err)
		}
	})
}

// TestMaterializeIn_ValidationBranches covers the pre-daemon validation legs (empty/missing/
// non-dir source + a tarDir failure), all of which return BEFORE CopyToContainer — so a nil
// client is safe. The happy-path cp is the docker_integration tier's job.
func TestMaterializeIn_ValidationBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	h := BoxHandle{ContainerID: "box", IdentityID: "id"}

	t.Run("empty and missing sources are skipped (no client call)", func(t *testing.T) {
		t.Parallel()
		srcs := []MaterializeSource{
			{HostDir: "", Dest: "/skills"},
			{HostDir: "/x", Dest: ""},
			{HostDir: filepath.Join(t.TempDir(), "does-not-exist"), Dest: "/skills"},
		}
		if err := MaterializeIn(ctx, nil, h, srcs); err != nil {
			t.Fatalf("MaterializeIn skip-branches: %v", err)
		}
	})

	t.Run("a non-directory source is a closed error", func(t *testing.T) {
		t.Parallel()
		f := filepath.Join(t.TempDir(), "file.txt")
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := MaterializeIn(ctx, nil, h, []MaterializeSource{{HostDir: f, Dest: "/skills"}}); err == nil {
			t.Fatal("MaterializeIn on a non-dir source: want error, got nil")
		}
	})

	t.Run("a tar failure (symlink in tree) fails closed before the cp", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "real.txt"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")); err != nil {
			t.Skipf("symlink unsupported on this host: %v", err)
		}
		if err := MaterializeIn(ctx, nil, h, []MaterializeSource{{HostDir: root, Dest: "/skills"}}); err == nil {
			t.Fatal("MaterializeIn with a symlink source: want tar error, got nil")
		}
	})
}
