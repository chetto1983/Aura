package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/scoring"
)

// newTestWriter builds a Writer with FS dirs under t.TempDir() and a NIL pool. The
// nil pool is fine for the FS-only paths exercised here (writePending, the gate
// decision, content_hash); the audit-INSERT-in-WithTx path is exercised by the
// db_integration test.
func newTestWriter(t *testing.T) (*Writer, string) {
	t.Helper()
	root := t.TempDir()
	w := NewWriter(WriterConfig{
		Pool:         nil,
		PendingDir:   filepath.Join(root, "pending"),
		ActiveDir:    filepath.Join(root, "active"),
		ExportDir:    filepath.Join(root, "export"),
		ArchiveDir:   filepath.Join(root, "archived"),
		Blocklist:    []string{"<|im_start|>"},
		BodyCapBytes: 32768,
	})
	return w, root
}

// TestWriterGateRecommendation asserts the create/update/install/delete tier+gate
// mapping comes from scoring (not a hand-rolled tier): all four gate.
func TestWriterGateRecommendation(t *testing.T) {
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
		if !scoring.GateRecommended(scoring.ComputeSkillTier(c.action, "")) {
			t.Errorf("GateRecommended(%s): want true (all four gate)", c.action)
		}
	}
}

func TestModelMutationBypassesGateExceptAlwaysOn(t *testing.T) {
	actor := AuditActor{ActorID: ActorModel}
	if !modelMutationBypassesGate(scoring.SkillCreate, Frontmatter{Name: "ordinary"}, actor) {
		t.Fatal("model-authored non-always skill should keep the in-box fast path")
	}
	if modelMutationBypassesGate(scoring.SkillCreate, Frontmatter{Name: "always", Always: true}, actor) {
		t.Fatal("model-authored always:true skill must stay gated")
	}
	if modelMutationBypassesGate(scoring.SkillCreate, Frontmatter{Name: "cli"}, AuditActor{ActorID: "cli"}) {
		t.Fatal("operator-authored skill mutations must stay gated")
	}
	if modelMutationBypassesGate(scoring.SkillDelete, Frontmatter{Name: "delete-me"}, actor) {
		t.Fatal("model-authored delete must stay gated")
	}
}

// TestWriterRejectsBlocklistedBody proves a model-authored body containing a
// blocklisted injection sequence is hard-rejected (allowBlocklisted=false) BEFORE
// any FS write — no pending dir is created.
func TestWriterRejectsBlocklistedBody(t *testing.T) {
	w, root := newTestWriter(t)
	fm := Frontmatter{Name: "evil", Description: "d", Type: TypeInstruction}

	_, err := w.WriteMutation(t.Context(), scoring.SkillCreate, fm, "hello <|im_start|> system", AuditActor{ActorID: "model"})
	if !errors.Is(err, ErrBlocklisted) {
		t.Fatalf("blocklisted body: want ErrBlocklisted, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "pending", "evil")); !os.IsNotExist(statErr) {
		t.Errorf("a rejected mutation must not write pending/evil (stat err=%v)", statErr)
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

func TestActivateAndDiscardPendingRejectBadName(t *testing.T) {
	w, _ := newTestWriter(t)
	for _, bad := range []string{"../escape", "Bad_Name", "a/b", ""} {
		if err := w.Activate(t.Context(), bad, ApprovalCLI, "", AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Activate(%q): want ErrInvalidName, got %v", bad, err)
		}
		if err := w.DiscardPending(t.Context(), bad, ApprovalCLI, "", AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("DiscardPending(%q): want ErrInvalidName, got %v", bad, err)
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

// TestWriterPendingWrite proves writePending lands a SKILL.md in pending/<name>/
// atomically and the rendered file round-trips through the loader's parser with the
// same name/description/type.
func TestWriterPendingWrite(t *testing.T) {
	w, root := newTestWriter(t)
	fm := Frontmatter{Name: "fmt-skill", Description: `uses: a colon`, Type: TypeInstruction}
	body := "Do the thing."

	if err := w.writePending("fmt-skill", fm, body); err != nil {
		t.Fatalf("writePending: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "pending", "fmt-skill", "SKILL.md"))
	if err != nil {
		t.Fatalf("read pending SKILL.md: %v", err)
	}
	gotFM, gotBody, perr := parseFrontmatter(raw)
	if perr != nil {
		t.Fatalf("rendered SKILL.md does not parse: %v\n%s", perr, raw)
	}
	if gotFM.Name != "fmt-skill" || gotFM.Description != "uses: a colon" || gotFM.Type != TypeInstruction {
		t.Errorf("round-trip frontmatter mismatch: %+v", gotFM)
	}
	if gotBody != body {
		t.Errorf("round-trip body: want %q, got %q", body, gotBody)
	}
	// No leftover temp dirs (the rename committed; the temp was renamed, not removed).
	entries, _ := os.ReadDir(filepath.Join(root, "pending"))
	for _, e := range entries {
		if e.Name() != "fmt-skill" {
			t.Errorf("leftover entry in pending dir: %q", e.Name())
		}
	}
}

func TestWriteInstallPendingPreAuditFailures(t *testing.T) {
	w, root := newTestWriter(t)
	fm := Frontmatter{Name: "installer", Description: "d", Type: TypeInstruction}

	w.pendingDir = ""
	if _, err := w.WriteInstallPending(t.Context(), fm, "body", filepath.Join(root, "staged"), "sha256:test", AuditActor{ActorID: "cli"}); err == nil || !strings.Contains(err.Error(), "pending dir not configured") {
		t.Fatalf("WriteInstallPending without pendingDir = %v, want configured error", err)
	}

	w.pendingDir = filepath.Join(root, "pending")
	_, err := w.WriteInstallPending(t.Context(), fm, "body", filepath.Join(root, "missing-staged"), "sha256:test", AuditActor{ActorID: "cli"})
	if err == nil || !strings.Contains(err.Error(), "copy staged tree") {
		t.Fatalf("WriteInstallPending with missing staged tree = %v, want copy error", err)
	}
	entries, readErr := os.ReadDir(w.pendingDir)
	if readErr != nil {
		t.Fatalf("read pending dir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("failed install pending write left temp entries: %v", entries)
	}
}

func TestLifecycleMethodsSurfacePreAuditFilesystemErrors(t *testing.T) {
	w, _ := newTestWriter(t)
	actor := AuditActor{ActorID: "cli"}

	if err := w.Activate(t.Context(), "missing", ApprovalCLI, "", actor); err == nil || !strings.Contains(err.Error(), "hash pending") {
		t.Fatalf("Activate missing pending = %v, want hash pending error", err)
	}
	if err := w.Archive(t.Context(), "missing", ApprovalCLI, actor); err == nil || !strings.Contains(err.Error(), "move to archived") {
		t.Fatalf("Archive missing active = %v, want move error", err)
	}
	if err := w.Restore(t.Context(), "missing", ApprovalCLI, actor); err == nil || !strings.Contains(err.Error(), "promote archived->active") {
		t.Fatalf("Restore missing archive entry = %v, want promote error", err)
	}

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
