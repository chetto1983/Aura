// materialize_stage_test.go is the daemon-free unit tier for the STAGING pass: the tree tar
// builder's dest-rooting and symlink guard, the spool-to-file shape and its cleanup on every
// path, and the asymmetry that makes a SHARED source skippable while the reader's own and the
// deployment's are not. Split from materialize_test.go when the staging assertions landed —
// that file owns the mirror plan, this one owns what a source becomes.

package usersandbox

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestWriteTarDir covers the tree tar builder: dest-rooting, directory entries, and the
// symlink / non-regular guards that close the materialize escape vector.
func TestWriteTarDir(t *testing.T) {
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

		var buf bytes.Buffer
		if err := writeTarDir(&buf, root, "/skills"); err != nil {
			t.Fatalf("writeTarDir: %v", err)
		}
		names := readTarNames(t, &buf)
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
		if err := writeTarDir(io.Discard, root, "/skills"); err == nil || !strings.Contains(err.Error(), "symlink") {
			t.Fatalf("writeTarDir with symlink: want symlink-guard error, got %v", err)
		}
	})
}

// seedSkillTree writes a one-file tree under a fresh dir and returns it.
func seedSkillTree(t *testing.T, name, content string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, name), []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return root
}

// seedMalformedTree returns a tree the tar pass must refuse: a symlink beside a real file. It
// skips the calling test where the host cannot make one (Windows without privilege).
func seedMalformedTree(t *testing.T) string {
	t.Helper()
	root := seedSkillTree(t, "real.txt", "x")
	if err := os.Symlink(filepath.Join(root, "real.txt"), filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("symlink unsupported on this host: %v", err)
	}
	return root
}

// spoolEntries counts the files left in a spool dir, which is what "cleaned up on every path"
// is actually measured as.
func spoolEntries(t *testing.T, spool string) int {
	t.Helper()
	entries, err := os.ReadDir(spool)
	if err != nil {
		t.Fatalf("read spool dir: %v", err)
	}
	return len(entries)
}

// TestTarSourcesSpoolsToDiskAndRewinds is the memory-shape assertion in the only form a unit
// test can make it: the staged tar is a FILE in the spool dir (not a buffer in the process),
// it is positioned at the start so the caller can stream it, and it still carries the tree.
//
// What it does NOT prove: that the peak RSS of a real resume fell. That is a claim about a
// running deployment with a real export, and nobody has measured it.
func TestTarSourcesSpoolsToDiskAndRewinds(t *testing.T) {
	t.Parallel()
	spool := t.TempDir()
	root := seedSkillTree(t, "SKILL.md", "BODY")

	staged, err := tarSources([]MaterializeSource{{HostDir: root, Dest: "/skills"}}, spool)
	if err != nil {
		t.Fatalf("tarSources: %v", err)
	}
	if len(staged) != 1 {
		t.Fatalf("staged %d sources, want 1", len(staged))
	}
	if dir := filepath.Dir(staged[0].tar.Name()); dir != spool {
		t.Fatalf("tar spooled to %q, want it under the spool dir %q", dir, spool)
	}
	if n := spoolEntries(t, spool); n != 1 {
		t.Fatalf("spool dir holds %d files, want exactly the one staged tar", n)
	}
	if names := readTarNames(t, staged[0].tar); names["skills/SKILL.md"] != "BODY" {
		t.Fatalf("staged tar = %v, want the rewound tree at skills/SKILL.md", names)
	}

	staged.close()
	if n := spoolEntries(t, spool); n != 0 {
		t.Fatalf("spool dir holds %d files after close, want 0", n)
	}
}

// TestTarSourcesCleansUpWhenALaterSourceFails is the error-path half of the cleanup: a source
// that faults after earlier ones were already spooled must not leave their files behind. The
// spool dir is a persistent volume in production, so a leak here is a disk that fills one
// resume at a time with nothing pointing at the cause.
func TestTarSourcesCleansUpWhenALaterSourceFails(t *testing.T) {
	t.Parallel()
	spool := t.TempDir()
	good := seedSkillTree(t, "SKILL.md", "BODY")
	bad := seedMalformedTree(t)

	_, err := tarSources([]MaterializeSource{
		{HostDir: good, Dest: "/skills"},
		{HostDir: bad, Dest: "/skills"},
	}, spool)
	if err == nil {
		t.Fatal("a symlink in an OWN source must fail the whole staging pass")
	}
	if n := spoolEntries(t, spool); n != 0 {
		t.Fatalf("spool dir holds %d files after a failed staging pass, want 0", n)
	}
}

// TestMaterializeInRemovesItsSpoolOnFailure closes the last cleanup path. MaterializeIn defers
// the close, so a failure raised AFTER staging — the too-broad-dest refusal, checked once the
// tars already exist — still leaves no files behind. Without the defer this is exactly the case
// that leaks: the error return is nowhere near the spooling.
func TestMaterializeInRemovesItsSpoolOnFailure(t *testing.T) {
	t.Parallel()
	spool := t.TempDir()
	root := seedSkillTree(t, "SKILL.md", "BODY")
	h := BoxHandle{ContainerID: "box", IdentityID: "id"}

	err := MaterializeIn(context.Background(), nil, h, []MaterializeSource{{HostDir: root, Dest: "/root"}}, spool)
	if err == nil {
		t.Fatal("want the too-broad refusal")
	}
	if n := spoolEntries(t, spool); n != 0 {
		t.Fatalf("spool dir holds %d files after a failed MaterializeIn, want 0", n)
	}
}

// TestSharedSourceIsSkippableAndOwnIsNot is the whole of the degrade-don't-deny rule, asserted
// as the ASYMMETRY it is: the same malformed tree is skipped when it arrives as a share and
// fails closed when it is the reader's own or the deployment's. Blurring the two into "skip
// everything" hides a fault in your own library; blurring them into "fail everything" is the
// state this change exists to leave, where one person's bad skill costs every grantee their box.
func TestSharedSourceIsSkippableAndOwnIsNot(t *testing.T) {
	t.Parallel()
	spool := t.TempDir()
	own := seedSkillTree(t, "SKILL.md", "MINE")
	malformed := seedMalformedTree(t)

	staged, err := tarSources([]MaterializeSource{
		{HostDir: malformed, Dest: "/skills/borrowed", SkipOnFault: true},
		{HostDir: own, Dest: "/skills"},
	}, spool)
	if err != nil {
		t.Fatalf("a malformed SHARED source must not fail the box: %v", err)
	}
	defer staged.close()
	if len(staged) != 1 || staged[0].hostDir != own {
		t.Fatalf("staged = %d sources, want only the reader's own", len(staged))
	}

	// The SAME tree, arriving as the reader's own source (SkipOnFault unset, which is also how
	// the deployment's export arrives): fails closed.
	if _, err := tarSources([]MaterializeSource{{HostDir: malformed, Dest: "/skills"}}, spool); err == nil {
		t.Fatal("a malformed OWN or deployment source must fail closed — a fault in your own tree is yours to see")
	}
}

// TestSharedSourceSkipIsPerSource proves the skip is individual: a second, well-formed share
// still reaches the box when the first one is dropped.
func TestSharedSourceSkipIsPerSource(t *testing.T) {
	t.Parallel()
	spool := t.TempDir()
	malformed := seedMalformedTree(t)
	fine := seedSkillTree(t, "SKILL.md", "SHARED")

	staged, err := tarSources([]MaterializeSource{
		{HostDir: malformed, Dest: "/skills/broken", SkipOnFault: true},
		{HostDir: fine, Dest: "/skills/fine", SkipOnFault: true},
	}, spool)
	if err != nil {
		t.Fatalf("tarSources: %v", err)
	}
	defer staged.close()
	if len(staged) != 1 || staged[0].dest != "/skills/fine" {
		t.Fatalf("staged = %+v, want only the well-formed share", staged)
	}
}

// TestTarSourcesCreatesTheSpoolDir proves the spool dir does not have to pre-exist: the run dir
// is created by the daemon, but a resume must not fail because a sweep removed it.
func TestTarSourcesCreatesTheSpoolDir(t *testing.T) {
	t.Parallel()
	spool := filepath.Join(t.TempDir(), "runs", "spool")
	root := seedSkillTree(t, "SKILL.md", "BODY")

	staged, err := tarSources([]MaterializeSource{{HostDir: root, Dest: "/skills"}}, spool)
	if err != nil {
		t.Fatalf("tarSources with an absent spool dir: %v", err)
	}
	defer staged.close()
	if spoolEntries(t, spool) != 1 {
		t.Fatal("the staged tar did not land in the created spool dir")
	}
}

// TestWithSpoolDirReachesTheMaterializePass is the wiring assertion the option had none of: a
// spool dir stored on the backend and never threaded through to the staging pass would leave
// every tar in os.TempDir() — which in this deployment is a 64 MiB tmpfs, i.e. exactly the
// memory the spool exists to stop using — and no test would notice.
//
// The proof is that the directory EXISTS afterwards: only tarSources creates it, so its
// presence means b.spoolDir reached MaterializeIn. A nil client is safe because the too-broad
// dest is refused after staging and before any exec is created.
func TestWithSpoolDirReachesTheMaterializePass(t *testing.T) {
	t.Parallel()
	spool := filepath.Join(t.TempDir(), "runs", "tmp")
	root := seedSkillTree(t, "SKILL.md", "BODY")

	b := NewDockerBackend(nil, "box-image", Resources{},
		WithSpoolDir(spool),
		WithMaterializeSources(func(context.Context, string) ([]MaterializeSource, error) {
			return []MaterializeSource{{HostDir: root, Dest: "/root"}}, nil
		}))
	if b.spoolDir != spool {
		t.Fatalf("WithSpoolDir stored %q, want %q", b.spoolDir, spool)
	}
	if err := b.materializeInputs(context.Background(), BoxHandle{ContainerID: "box", IdentityID: "id"}); err == nil {
		t.Fatal("want the too-broad refusal")
	}
	if _, err := os.Stat(spool); err != nil {
		t.Fatalf("the staging pass never used the wired spool dir: %v", err)
	}
	if n := spoolEntries(t, spool); n != 0 {
		t.Fatalf("spool dir holds %d files after a failed materialize, want 0", n)
	}
}

// TestTarSourcesWithNoSpoolDirUsesTheSystemTemp pins the documented default. It is the shape a
// test composition and the CLI get, and it must still produce a rewound file that the caller
// can stream and close — not a silent no-op and not a panic.
func TestTarSourcesWithNoSpoolDirUsesTheSystemTemp(t *testing.T) {
	t.Parallel()
	root := seedSkillTree(t, "SKILL.md", "BODY")

	staged, err := tarSources([]MaterializeSource{{HostDir: root, Dest: "/skills"}}, "")
	if err != nil {
		t.Fatalf("tarSources with no spool dir: %v", err)
	}
	if len(staged) != 1 {
		t.Fatalf("staged %d sources, want 1", len(staged))
	}
	name := staged[0].tar.Name()
	if dir := filepath.Dir(name); dir != filepath.Clean(os.TempDir()) {
		t.Fatalf("tar spooled to %q, want the system temp %q", dir, os.TempDir())
	}
	if names := readTarNames(t, staged[0].tar); names["skills/SKILL.md"] != "BODY" {
		t.Fatalf("staged tar = %v, want the rewound tree", names)
	}
	staged.close()
	if _, err := os.Stat(name); !os.IsNotExist(err) {
		t.Fatalf("close left %q behind (stat err = %v)", name, err)
	}
}

// TestTarSourcesFailsClosedOnAnUnusableSpoolDir is the other half of "degrades sanely": a
// spool dir that cannot be created is an ERROR, never a quiet fall-back to os.TempDir(). The
// difference matters here and nowhere else — the fall-back would put the tars on the 64 MiB
// tmpfs the wiring exists to avoid, and the resume would look healthy until a tree outgrew it.
func TestTarSourcesFailsClosedOnAnUnusableSpoolDir(t *testing.T) {
	t.Parallel()
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocker: %v", err)
	}
	root := seedSkillTree(t, "SKILL.md", "BODY")

	staged, err := tarSources([]MaterializeSource{{HostDir: root, Dest: "/skills"}}, filepath.Join(blocker, "spool"))
	if err == nil {
		staged.close()
		t.Fatal("a spool dir that cannot be created must fail the staging pass, not fall back")
	}
	if len(staged) != 0 {
		t.Fatalf("staged %d sources on the failure path, want none", len(staged))
	}
}

// TestTarSourcesSkipsAHostDirThatIsGone is D-217-3's other half seen from the staging pass. A
// source that has VANISHED is not a fault at any ownership: it is a reader who has written no
// skill of their own, a deployment with no export yet, or — the one that matters — a share
// revoked since the resolver ran. All three stage nothing and let the mirror clear the dest,
// which is how a revoked body leaves the box at the next resume.
func TestTarSourcesSkipsAHostDirThatIsGone(t *testing.T) {
	t.Parallel()
	spool := t.TempDir()
	gone := filepath.Join(t.TempDir(), "revoked-share")
	present := seedSkillTree(t, "SKILL.md", "MINE")

	staged, err := tarSources([]MaterializeSource{
		{HostDir: gone, Dest: "/skills/revoked"},
		{HostDir: present, Dest: "/skills"},
	}, spool)
	if err != nil {
		t.Fatalf("a host dir that is gone must not be a fault: %v", err)
	}
	defer staged.close()
	if len(staged) != 1 || staged[0].hostDir != present {
		t.Fatalf("staged = %+v, want only the source that still exists", staged)
	}
}

// TestStagedSourceCloseOnAZeroValue covers the guard the error paths lean on: spoolSource
// closes a half-built stagedSource, and tarSources closes the whole plan, so close has to
// tolerate a source that never acquired a file.
func TestStagedSourceCloseOnAZeroValue(t *testing.T) {
	t.Parallel()
	stagedSource{}.close()
	stagedSources{{}, {}}.close()
}
