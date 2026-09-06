package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/scoring"
)

// newTestWriter builds a Writer with FS dirs under t.TempDir() and a NIL pool. The nil
// pool is fine for everything that fails BEFORE writeActive's audit tx (validation, the
// name chokepoint, staging, content_hash); the tx itself and everything after it is
// exercised by the db_integration tests.
func newTestWriter(t *testing.T) (*Writer, string) {
	t.Helper()
	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:         nil,
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    filepath.Join(root, "export"),
		ArchiveDir:   filepath.Join(root, "archived"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})
	return w, root
}

// TestWriterRiskTier asserts the create/update/install/delete tier mapping still comes
// from scoring (not a hand-rolled tier). Since amendment #97 the tier no longer selects
// a code path in the writer — it is reported to the operator/model — so this pins the
// classification only.
func TestWriterRiskTier(t *testing.T) {
	cases := []struct {
		action scoring.SkillAction
		tier   scoring.RiskTier
	}{
		{scoring.SkillCreate, scoring.Risky},
		{scoring.SkillUpdate, scoring.Risky},
		{scoring.SkillInstall, scoring.Risky},
		{scoring.SkillDelete, scoring.Destructive},
	}
	for _, c := range cases {
		if got := scoring.ComputeSkillTier(c.action, ""); got != c.tier {
			t.Errorf("ComputeSkillTier(%s): want %s, got %s", c.action, c.tier, got)
		}
	}
}

// TestWriterRejectsBlocklistedBody proves a model-authored body containing a
// blocklisted injection sequence is hard-rejected (allowBlocklisted=false) BEFORE any
// FS write. With the write now immediate, "before any FS write" means the skill must
// appear in NEITHER the active root the loader scans NOR the export dir the sandbox
// mounts — the blocklist is one of the two controls left standing.
func TestWriterRejectsBlocklistedBody(t *testing.T) {
	w, root := newTestWriter(t)
	fm := Frontmatter{Name: "evil", Description: "d", Type: TypeInstruction}

	_, err := w.WriteMutation(t.Context(), scoring.SkillCreate, fm, "hello <|im_start|> system", AuditActor{ActorID: "model"})
	if !errors.Is(err, ErrBlocklisted) {
		t.Fatalf("blocklisted body: want ErrBlocklisted, got %v", err)
	}
	for _, dir := range []string{"active", "export"} {
		if _, statErr := os.Stat(filepath.Join(root, dir, "evil")); !os.IsNotExist(statErr) {
			t.Errorf("a rejected mutation must not write %s/evil (stat err=%v)", dir, statErr)
		}
	}
}

// TestSetAlwaysRejectsBadName proves SetAlways validates the name grammar BEFORE any
// FS/audit work (D-30 chokepoint): a traversal-shaped name is rejected up front, so
// the nil-pool test never reaches the audit tx.
func TestSetAlwaysRejectsBadName(t *testing.T) {
	w, _ := newTestWriter(t)
	for _, bad := range []string{"../escape", "Bad_Name", "a/b", ""} {
		if err := w.SetAlways(t.Context(), bad, true, AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("SetAlways(%q): want ErrInvalidName, got %v", bad, err)
		}
	}
}

// TestArchiveRejectsBadName is the CR-01 regression: Archive validates the name
// grammar BEFORE joining it into a path (D-30 chokepoint), so a "../" traversal name
// can never reach Dematerialize/os.RemoveAll outside the export dir. A sentinel tree
// outside the export dir must survive an archive("../<sentinel>") attempt.
func TestArchiveRejectsBadName(t *testing.T) {
	w, root := newTestWriter(t)
	for _, bad := range []string{"../escape", "Bad_Name", "a/b", "", "../../tmp/x"} {
		if err := w.Archive(t.Context(), bad, ApprovalCLI, AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Archive(%q): want ErrInvalidName, got %v", bad, err)
		}
	}

	// A sibling tree of the export dir must survive a traversal archive attempt.
	sentinel := filepath.Join(root, "victim")
	if err := os.MkdirAll(sentinel, 0o750); err != nil {
		t.Fatalf("seed sentinel: %v", err)
	}
	_ = w.Archive(t.Context(), "../victim", ApprovalCLI, AuditActor{ActorID: "cli"})
	if _, err := os.Stat(sentinel); err != nil {
		t.Fatalf("traversal archive must not delete the sibling sentinel: %v", err)
	}
}

// TestDeleteRejectsBadName proves Delete self-guards the name grammar (defense in
// depth: WriteMutation already sanitizes upstream, but the method must not reach
// os.RemoveAll with a traversal name if a future caller bypasses WriteMutation).
func TestDeleteRejectsBadName(t *testing.T) {
	w, _ := newTestWriter(t)
	for _, bad := range []string{"../escape", "a/b", ""} {
		if _, err := w.Delete(t.Context(), bad, AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Delete(%q): want ErrInvalidName, got %v", bad, err)
		}
	}
}

// TestWriteInstallPreAuditFailures proves a WriteInstall that fails BEFORE the audit tx
// (an unconfigured active root, a missing staged tree) surfaces its own diagnostic and
// leaves nothing behind: the staging dir is swept, so a failed install can never leave a
// half-copied tree where the loader would find it on the next scan.
func TestWriteInstallPreAuditFailures(t *testing.T) {
	w, root := newTestWriter(t)
	fm := Frontmatter{Name: "installer", Description: "d", Type: TypeInstruction}

	w.activeDir = ""
	if _, err := w.WriteInstall(t.Context(), fm, filepath.Join(root, "staged"), "sha256:test", AuditActor{ActorID: "cli"}); err == nil || !strings.Contains(err.Error(), "active dir not configured") {
		t.Fatalf("WriteInstall without activeDir = %v, want configured error", err)
	}

	w.activeDir = filepath.Join(root, "active")
	_, err := w.WriteInstall(t.Context(), fm, filepath.Join(root, "missing-staged"), "sha256:test", AuditActor{ActorID: "cli"})
	if err == nil || !strings.Contains(err.Error(), "copy staged tree") {
		t.Fatalf("WriteInstall with a missing staged tree = %v, want copy error", err)
	}
	staging, readErr := os.ReadDir(filepath.Join(w.activeDir, stagingDirName))
	if readErr != nil {
		t.Fatalf("read staging dir: %v", readErr)
	}
	if len(staging) != 0 {
		t.Fatalf("a failed install left staged entries behind: %v", staging)
	}
	if entries, _ := os.ReadDir(w.activeDir); len(entries) != 1 {
		t.Fatalf("a failed install must leave only the (empty) staging root in active/, got %v", entries)
	}
}

// An unknown NAME is the caller's mistake, not a filesystem fault, and it must arrive as the
// sentinel the package declares for it.
//
// This test previously asserted the opposite — that Archive and Restore surface promoteDir's
// own "move to archived" / "promote archived->active" rename failure. That was the defect,
// pinned: measured 2026-09-06 through skill_manage, archiving a name that does not exist
// handed the model two absolute host paths and "no such file or directory". The assertions
// are inverted here deliberately, not relaxed: the errors must now be errors.Is-checkable and
// must NOT carry a host path.
func TestLifecycleMethodsRejectAnUnknownNameWithTheSentinel(t *testing.T) {
	w, root := newTestWriter(t)
	actor := AuditActor{ActorID: "cli"}

	for _, test := range []struct {
		name string
		call func() error
	}{
		{"archive", func() error { return w.Archive(t.Context(), "missing", ApprovalCLI, actor) }},
		{"restore", func() error { return w.Restore(t.Context(), "missing", ApprovalCLI, actor) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := test.call()
			if !errors.Is(err, ErrUnknownSkill) {
				t.Fatalf("%s of an absent skill = %v, want ErrUnknownSkill", test.name, err)
			}
			if strings.Contains(err.Error(), root) {
				t.Fatalf("%s error leaks a host path: %v", test.name, err)
			}
		})
	}
}

func TestLifecycleMethodsSurfacePreAuditFilesystemErrors(t *testing.T) {
	w, _ := newTestWriter(t)
	actor := AuditActor{ActorID: "cli"}

	w.archiveDir = ""
	if err := w.Restore(t.Context(), "missing", ApprovalCLI, actor); err == nil || !strings.Contains(err.Error(), "archive dir not configured") {
		t.Fatalf("Restore without archive dir = %v, want configured error", err)
	}
	if err := w.SetAlways(t.Context(), "missing", true, actor); err == nil || !strings.Contains(err.Error(), "read active SKILL.md") {
		t.Fatalf("SetAlways missing active skill = %v, want read error", err)
	}
}

// TestContentHashDeterministic proves the canonical hash is stable across map
// iteration order and distinguishes path vs content boundaries.
func TestContentHashDeterministic(t *testing.T) {
	a := HashSkillFiles(map[string][]byte{"SKILL.md": []byte("ab"), "x/y.py": []byte("c")})
	b := HashSkillFiles(map[string][]byte{"x/y.py": []byte("c"), "SKILL.md": []byte("ab")})
	if a != b {
		t.Errorf("hash not order-stable: %s vs %s", a, b)
	}
	// A path/content boundary shift must change the hash (collision resistance):
	// {"a":"bc"} vs {"ab":"c"} would collide under naive concatenation.
	h1 := HashSkillFiles(map[string][]byte{"a": []byte("bc")})
	h2 := HashSkillFiles(map[string][]byte{"ab": []byte("c")})
	if h1 == h2 {
		t.Errorf("hash collides across the path/content boundary: %s", h1)
	}
}

// TestHashSkillDirMatchesCanonicalFileHash proves an installer-style tree hashes to
// the same canonical pin as the writer's explicit file map, including nested slash
// normalized paths.
func TestHashSkillDirMatchesCanonicalFileHash(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "SKILL.md"), "skill body")
	writeFile(t, filepath.Join(root, "scripts", "run.py"), "print(42)")

	got, err := HashSkillDir(root)
	if err != nil {
		t.Fatalf("HashSkillDir: %v", err)
	}
	want := HashSkillFiles(map[string][]byte{
		"SKILL.md":       []byte("skill body"),
		"scripts/run.py": []byte("print(42)"),
	})
	if got != want {
		t.Fatalf("HashSkillDir = %s, want %s", got, want)
	}
}

func TestPromoteDirReplacesDestination(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "pending", "demo")
	dst := filepath.Join(root, "active", "demo")
	writeFile(t, filepath.Join(src, "SKILL.md"), "new")
	writeFile(t, filepath.Join(dst, "stale.txt"), "old")

	if err := promoteDir(src, dst); err != nil {
		t.Fatalf("promoteDir: %v", err)
	}
	if _, err := os.Stat(src); !os.IsNotExist(err) {
		t.Fatalf("promoted source should be gone (stat err=%v)", err)
	}
	if got := readFile(t, filepath.Join(dst, "SKILL.md")); got != "new" {
		t.Fatalf("promoted SKILL.md = %q, want new", got)
	}
	if _, err := os.Stat(filepath.Join(dst, "stale.txt")); !os.IsNotExist(err) {
		t.Fatalf("stale destination file should be replaced (stat err=%v)", err)
	}
}

func TestAuditActionFor(t *testing.T) {
	cases := []struct {
		name string
		in   scoring.SkillAction
		want AuditAction
	}{
		{name: "create", in: scoring.SkillCreate, want: AuditCreate},
		{name: "update", in: scoring.SkillUpdate, want: AuditUpdate},
		{name: "install", in: scoring.SkillInstall, want: AuditInstall},
		{name: "delete", in: scoring.SkillDelete, want: AuditDelete},
		{name: "unknown defaults to update", in: scoring.SkillAction("other"), want: AuditUpdate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := auditActionFor(tc.in); got != tc.want {
				t.Fatalf("auditActionFor(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
