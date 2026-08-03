package usersandbox

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"go.uber.org/goleak"
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

// TestWriteTarEntryDeclaredLength covers the contract the STREAMED copy-in rests on. tar has no
// length-suffixed framing, so CopyFileInStream declares the size before it has seen a byte of the
// document; a source that then delivers a different count must fail rather than land a file whose
// length contradicts the catalog record it was opened from. A truncated spreadsheet that looks
// whole is worse than no file — the agent computes a confident wrong answer from it.
func TestWriteTarEntryDeclaredLength(t *testing.T) {
	t.Parallel()

	entry := func(declared int64, body string) (map[string]string, error) {
		hdr, err := singleFileHeader("/workspace/documents/d.xlsx", declared, 0o644)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := writeTarEntry(&buf, hdr, strings.NewReader(body)); err != nil {
			return nil, err
		}
		return readTarNames(t, &buf), nil
	}

	t.Run("an exact source builds the entry", func(t *testing.T) {
		t.Parallel()
		names, err := entry(5, "hello")
		if err != nil {
			t.Fatalf("writeTarEntry: %v", err)
		}
		if names["workspace/documents/d.xlsx"] != "hello" {
			t.Fatalf("entries = %v", names)
		}
	})

	t.Run("a short source is refused", func(t *testing.T) {
		t.Parallel()
		_, err := entry(64, "hello")
		if err == nil || !strings.Contains(err.Error(), "declared 64") {
			t.Fatalf("err = %v, want a refusal naming the declared length", err)
		}
	})

	t.Run("a long source is refused", func(t *testing.T) {
		t.Parallel()
		_, err := entry(2, "hello")
		if err == nil || !strings.Contains(err.Error(), "longer than the declared 2") {
			t.Fatalf("err = %v, want a refusal naming the declared length", err)
		}
	})

	t.Run("a traversal-shaped path never reaches the writer", func(t *testing.T) {
		t.Parallel()
		if _, err := singleFileHeader("/", 0, 0o644); err == nil {
			t.Fatal("singleFileHeader(\"/\"): want error, got nil")
		}
	})
}

// TestPipeTarEntry covers the streamed copy-in's concurrency, which no daemon test can pin: which
// of the two errors survives, and that neither outcome strands the producer goroutine. goleak is
// asserted per-test because the package's TestMain gate is docker_integration-only.
func TestPipeTarEntry(t *testing.T) {
	hdr := func(size int64) *tar.Header {
		h, err := singleFileHeader("/workspace/documents/d.xlsx", size, 0o644)
		if err != nil {
			t.Fatalf("singleFileHeader: %v", err)
		}
		return h
	}

	t.Run("the sink receives the whole tar", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		var got map[string]string
		err := pipeTarEntry(hdr(5), strings.NewReader("hello"), func(r io.Reader) error {
			got = readTarNames(t, r)
			return nil
		})
		if err != nil {
			t.Fatalf("pipeTarEntry: %v", err)
		}
		if got["workspace/documents/d.xlsx"] != "hello" {
			t.Fatalf("sink saw %v", got)
		}
	})

	t.Run("a dead source wins over the sink's truncated-body error", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		err := pipeTarEntry(hdr(4096), iotest.TimeoutReader(strings.NewReader("hello")), func(r io.Reader) error {
			_, _ = io.Copy(io.Discard, r)
			return errors.New("unexpected EOF from the daemon")
		})
		// The daemon's view of a dead source is a truncated request body; reporting THAT would
		// send an operator looking at the sandbox for an object-store outage.
		if err == nil || !strings.Contains(err.Error(), iotest.ErrTimeout.Error()) {
			t.Fatalf("err = %v, want the source's own failure", err)
		}
	})

	t.Run("a sink that hangs up early wins, and strands nothing", func(t *testing.T) {
		defer goleak.VerifyNone(t)
		// The sink never drains, so the producer blocks mid-body — exactly the shape that leaks a
		// goroutine if the read end is not closed before its result is awaited.
		err := pipeTarEntry(hdr(1<<20), strings.NewReader(strings.Repeat("x", 1<<20)), func(io.Reader) error {
			return errors.New("daemon refused the copy")
		})
		if err == nil || !strings.Contains(err.Error(), "daemon refused the copy") {
			t.Fatalf("err = %v, want the sink's own failure, not pipe noise", err)
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

	t.Run("a too-broad dest fails before any client call", func(t *testing.T) {
		t.Parallel()
		root := t.TempDir()
		if err := os.WriteFile(filepath.Join(root, "SKILL.md"), []byte("x"), 0o644); err != nil {
			t.Fatalf("write: %v", err)
		}
		// A nil client is the assertion: the guard must reject the dest BEFORE the rm -rf
		// exec is created, so reaching the daemon at all would panic here.
		err := MaterializeIn(ctx, nil, h, []MaterializeSource{{HostDir: root, Dest: "/root"}})
		if err == nil || !strings.Contains(err.Error(), "too broad") {
			t.Fatalf("MaterializeIn with dest=/root = %v, want a too-broad refusal", err)
		}
	})
}

// TestDestTooBroadToClear pins the guard standing between a SourceResolver typo and an
// `rm -rf` that erases the box: the box root and the system/home directories are refused,
// while the Aura-owned trees the backend actually materializes into are allowed.
//
// "/skills" is the load-bearing case in BOTH directions — it is the live production dest, so
// a guard that refuses it silently disables skills in the sandbox, and a depth-based rule
// would do exactly that.
func TestDestTooBroadToClear(t *testing.T) {
	t.Parallel()
	refused := []string{"", "/", "/root", "/tmp", "/workspace", "/etc", "/usr", "/var", "/home", "/skills/../etc"}
	for _, p := range refused {
		if !destTooBroadToClear(p) {
			t.Errorf("destTooBroadToClear(%q) = false, want it refused", p)
		}
	}
	allowed := []string{"/skills", "/root/.aura/agents", "/root/.aura/pyscripts", "/opt/aura/skills"}
	for _, p := range allowed {
		if destTooBroadToClear(p) {
			t.Errorf("destTooBroadToClear(%q) = true, want it allowed", p)
		}
	}
}
