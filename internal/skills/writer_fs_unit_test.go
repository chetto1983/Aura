package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/scoring"
)

// TestWriteMutationByNameRejectsInvalidName proves the string-keyed entry points the CLI
// and tool adapter share (WriteMutationByName / WriteMutationCLI) funnel through the same
// ValidateForWrite chokepoint: an invalid name is a hard reject BEFORE any FS write or
// audit tx, so the nil-pool writer never reaches db.WithTx.
func TestWriteMutationByNameRejectsInvalidName(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	for _, bad := range []string{"Bad_Name", "../escape", "a/b", ""} {
		if _, err := w.WriteMutationByName(t.Context(), "create", bad, "d", "body", false, AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("WriteMutationByName(%q) = %v, want ErrInvalidName", bad, err)
		}
		if _, err := w.WriteMutationCLI(t.Context(), "create", bad, "d", "body", false); !errors.Is(err, ErrInvalidName) {
			t.Errorf("WriteMutationCLI(%q) = %v, want ErrInvalidName", bad, err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "pending")); len(entries) != 0 {
		t.Fatalf("a rejected mutation must not write into pending, got %v", entries)
	}
}

// TestWriteMutationCLIRejectsBlocklisted proves the CLI convenience path (actor "cli")
// still hard-rejects a blocklisted body — the operator string entry point does NOT pass
// allowBlocklisted (that override is the separate gated path), so a blocklist hit returns
// ErrBlocklisted before any FS/tx work.
func TestWriteMutationCLIRejectsBlocklisted(t *testing.T) {
	t.Parallel()
	w, _ := newTestWriter(t)
	if _, err := w.WriteMutationCLI(t.Context(), "create", "evil", "d", "x <|im_start|> y", false); !errors.Is(err, ErrBlocklisted) {
		t.Fatalf("WriteMutationCLI blocklisted body = %v, want ErrBlocklisted", err)
	}
}

// TestWriteMutationPendingDirUnconfigured proves WriteMutation surfaces the writePending
// "pending dir not configured" error for a valid, gated, non-delete mutation when the
// writer has no pending dir — the FS-write failure returns before the audit tx (nil pool
// never touched).
func TestWriteMutationPendingDirUnconfigured(t *testing.T) {
	t.Parallel()
	w, _ := newTestWriter(t)
	w.pendingDir = ""
	fm := Frontmatter{Name: "calc", Description: "d", Type: TypeInstruction}
	// Operator actor + non-always create still gates (only model+non-always bypasses), so
	// the path reaches writePending then returns its unconfigured error pre-tx.
	_, err := w.WriteMutation(t.Context(), scoring.SkillCreate, fm, "body", AuditActor{ActorID: "cli"})
	if err == nil || !strings.Contains(err.Error(), "pending dir not configured") {
		t.Fatalf("WriteMutation w/o pending dir = %v, want configured error", err)
	}
}

// TestWriteMutationDeleteRejectsInvalidName proves the delete branch of WriteMutation
// self-guards the name grammar through Delete's SanitizeName (a traversal name never
// reaches os.RemoveAll); the gated-delete-pending audit branch is db_integration-gated,
// but the bad-name reject is pre-tx.
func TestWriteMutationDeleteRejectsInvalidName(t *testing.T) {
	t.Parallel()
	w, _ := newTestWriter(t)
	fm := Frontmatter{Name: "../escape", Description: "d", Type: TypeInstruction}
	// Model-authored delete stays gated, so the gated branch is taken; but the upstream
	// ValidateForWrite name check rejects "../escape" first.
	if _, err := w.WriteMutation(t.Context(), scoring.SkillDelete, fm, "body", AuditActor{ActorID: ActorModel}); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("WriteMutation delete with bad name = %v, want ErrInvalidName", err)
	}
}

// TestWritePendingUnconfigured proves writePending refuses to write without a pending dir
// (the explicit guard the WriteMutation/SaveSnippet paths rely on).
func TestWritePendingUnconfigured(t *testing.T) {
	t.Parallel()
	w, _ := newTestWriter(t)
	w.pendingDir = ""
	if err := w.writePending("calc", Frontmatter{Name: "calc"}, "body"); err == nil || !strings.Contains(err.Error(), "pending dir not configured") {
		t.Fatalf("writePending w/o dir = %v, want configured error", err)
	}
}

func TestWritePendingRejectsInvalidNameAtFSBoundary(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	for _, bad := range []string{"../escape", "a/b", `a\b`, "Bad_Name", ""} {
		if err := w.writePending(bad, Frontmatter{Name: bad}, "body"); !errors.Is(err, ErrInvalidName) {
			t.Errorf("writePending(%q) = %v, want ErrInvalidName", bad, err)
		}
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "pending")); len(entries) != 0 {
		t.Fatalf("invalid direct writePending calls must not write into pending, got %v", entries)
	}
}

func TestWriteInstallPendingRejectsInvalidNameAtFSBoundary(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	_, err := w.WriteInstallPending(
		t.Context(),
		Frontmatter{Name: "../escape", Description: "d", Type: TypeInstruction},
		"body",
		filepath.Join(root, "stage"),
		"sha256:test",
		AuditActor{ActorID: "cli"},
	)
	if !errors.Is(err, ErrInvalidName) {
		t.Fatalf("WriteInstallPending traversal name = %v, want ErrInvalidName", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "pending")); len(entries) != 0 {
		t.Fatalf("invalid install must not write into pending, got %v", entries)
	}
}

// TestSkillFileBytesRendersSnippetLanguage proves the rendered SKILL.md round-trips a
// snippet's load-bearing language line and a quote-needing description (yamlScalar) and
// re-parses to the same frontmatter, while an instruction omits the language line.
func TestSkillFileBytesRendersSnippetLanguage(t *testing.T) {
	t.Parallel()
	snip := Frontmatter{Name: "calc", Description: "needs: a colon", Type: TypeSnippet, Language: "python", Always: true}
	raw := skillFileBytes(snip, "docs body")
	if !strings.Contains(string(raw), "language: python\n") {
		t.Fatalf("snippet SKILL.md must carry the language line:\n%s", raw)
	}
	if !strings.Contains(string(raw), "always: true\n") {
		t.Fatalf("always:true must render:\n%s", raw)
	}
	gotFM, gotBody, err := parseFrontmatter(raw)
	if err != nil {
		t.Fatalf("rendered snippet SKILL.md does not parse: %v", err)
	}
	if gotFM.Language != "python" || gotFM.Type != TypeSnippet || gotFM.Description != "needs: a colon" || !gotFM.Always {
		t.Fatalf("snippet round-trip mismatch: %+v", gotFM)
	}
	if gotBody != "docs body" {
		t.Fatalf("body round-trip = %q", gotBody)
	}

	// An instruction skill with an (ignored) language must NOT emit a language line.
	instr := skillFileBytes(Frontmatter{Name: "doc", Description: "plain", Type: TypeInstruction, Language: "python"}, "b")
	if strings.Contains(string(instr), "language:") {
		t.Fatalf("instruction SKILL.md must not carry a language line:\n%s", instr)
	}
}

// TestYamlScalarQuotingBoundaries pins yamlScalar's quote-or-bare decision and inner
// escaping across the special-character set the goccy round-trip depends on.
func TestYamlScalarQuotingBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want string
	}{
		{in: "plain words", want: "plain words"},
		{in: "has: colon", want: `"has: colon"`},
		{in: `has "quote"`, want: `"has \"quote\""`},
		{in: "has # hash", want: `"has # hash"`},
		{in: "", want: `""`},
		{in: " leading space", want: `" leading space"`},
		{in: "*anchor", want: `"*anchor"`},
		{in: "&amp", want: `"&amp"`},
		{in: "!bang", want: `"!bang"`},
		// A backslash alone does not force quoting (it is not in the trigger set); but
		// when quoting fires for another reason, an inner backslash is escaped.
		{in: `back\slash`, want: `back\slash`},
		{in: `quote " and \ backslash`, want: `"quote \" and \\ backslash"`},
	}
	for _, c := range cases {
		if got := yamlScalar(c.in); got != c.want {
			t.Errorf("yamlScalar(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMaterializeRequiresAllArgs proves the required-arg guard returns an error when any
// of name/skillDir/exportDir is empty (no partial materialization).
func TestMaterializeRequiresAllArgs(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, c := range []struct{ name, skillDir, export string }{
		{"", dir, dir},
		{"x", "", dir},
		{"x", dir, ""},
	} {
		if err := Materialize(c.name, c.skillDir, c.export); err == nil || !strings.Contains(err.Error(), "required") {
			t.Errorf("Materialize(%q,%q,%q) = %v, want required-args error", c.name, c.skillDir, c.export, err)
		}
	}
}

// TestMaterializeMissingSourceErrors proves copyTreeNoSymlinks surfaces a read error when
// the active skill dir is absent (the destination is cleared+made first, then the copy
// fails).
func TestMaterializeMissingSourceErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := Materialize("ghost", filepath.Join(root, "no-such-skill"), filepath.Join(root, "export")); err == nil {
		t.Fatal("Materialize from a missing source dir must error")
	}
}

// TestDematerializeNoopAndError proves Dematerialize is a no-op for empty args and
// removes a present subtree, with idempotency on an absent one (the early-return guard +
// the os.RemoveAll path).
func TestDematerializeNoopAndError(t *testing.T) {
	t.Parallel()
	if err := Dematerialize("", "anything"); err != nil {
		t.Fatalf("Dematerialize empty name must be a no-op, got %v", err)
	}
	if err := Dematerialize("x", ""); err != nil {
		t.Fatalf("Dematerialize empty exportDir must be a no-op, got %v", err)
	}
	export := t.TempDir()
	writeFile(t, filepath.Join(export, "x", "SKILL.md"), "data")
	if err := Dematerialize("x", export); err != nil {
		t.Fatalf("Dematerialize present subtree: %v", err)
	}
	if _, err := os.Stat(filepath.Join(export, "x")); !os.IsNotExist(err) {
		t.Fatalf("subtree survived dematerialize (err=%v)", err)
	}
}

// TestCopyTreeNoSymlinksNestedDirs proves the recursive copy descends real subdirectories
// and copies nested regular files (the IsDir recursion + IsRegular leaf branches).
func TestCopyTreeNoSymlinksNestedDirs(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	src := filepath.Join(root, "src")
	dst := filepath.Join(root, "dst")
	writeFile(t, filepath.Join(src, "SKILL.md"), "top")
	writeFile(t, filepath.Join(src, "a", "b", "deep.py"), "print(1)")
	if err := os.MkdirAll(dst, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := copyTreeNoSymlinks(src, dst); err != nil {
		t.Fatalf("copyTreeNoSymlinks: %v", err)
	}
	if got := readFile(t, filepath.Join(dst, "a", "b", "deep.py")); got != "print(1)" {
		t.Fatalf("nested file = %q, want print(1)", got)
	}
}

// TestHashSkillDirMissingDirErrors proves HashSkillDir surfaces collectFilesNoSymlinks'
// read error on a nonexistent dir (the recovery-pin caller's failure mode).
func TestHashSkillDirMissingDirErrors(t *testing.T) {
	t.Parallel()
	if _, err := HashSkillDir(filepath.Join(t.TempDir(), "no-such-dir")); err == nil {
		t.Fatal("HashSkillDir on a missing dir must error")
	}
}

// TestMaterializeBuiltinsEmptyDirNoop proves MaterializeBuiltins is a no-op for an empty
// dir (the composition-root guard) and the changed-content rewrite path of writeIfChanged.
func TestMaterializeBuiltinsEmptyDirNoop(t *testing.T) {
	t.Parallel()
	if err := MaterializeBuiltins(""); err != nil {
		t.Fatalf("MaterializeBuiltins(\"\") must be a no-op, got %v", err)
	}

	// writeIfChanged rewrites when the on-disk content differs from the embedded bytes.
	dir := t.TempDir()
	target := filepath.Join(dir, "skill-creator", "SKILL.md")
	writeFile(t, target, "STALE OPERATOR EDIT THAT DIFFERS FROM THE EMBEDDED BYTES")
	if err := MaterializeBuiltins(dir); err != nil {
		t.Fatalf("MaterializeBuiltins: %v", err)
	}
	got := readFile(t, target)
	if got == "STALE OPERATOR EDIT THAT DIFFERS FROM THE EMBEDDED BYTES" {
		t.Fatal("writeIfChanged must rewrite a file whose content differs from the embedded bytes")
	}
}

// TestParseFrontmatterMalformedYAML pins the yaml.Unmarshal error branch: a valid fence
// surrounding malformed YAML (a tab-indented mapping goccy rejects) surfaces a parse
// error, distinct from the missing-fence errors splitFrontmatter raises.
func TestParseFrontmatterMalformedYAML(t *testing.T) {
	t.Parallel()
	// A scalar where a mapping is expected: "name" is a sequence value, not a string.
	raw := "---\nname:\n  - not\n  - a\n  - string-but-a-sequence: {bad\n---\nbody\n"
	if _, _, err := parseFrontmatter([]byte(raw)); err == nil || !strings.Contains(err.Error(), "parse frontmatter yaml") {
		t.Fatalf("malformed-YAML parse = %v, want a yaml parse error", err)
	}
}

// TestValidateStructureBranches drives validateStructure's individual structural
// rejections (the loader's per-skill gate) — each branch returns a distinct error and a
// fully-valid skill passes.
func TestValidateStructureBranches(t *testing.T) {
	t.Parallel()
	const cap = 100
	cases := []struct {
		name    string
		fm      Frontmatter
		dir     string
		body    string
		wantSub string
	}{
		{name: "missing name", fm: Frontmatter{Description: "d", Type: TypeInstruction}, dir: "x", body: "b", wantSub: "missing name"},
		{name: "name dir mismatch", fm: Frontmatter{Name: "other", Description: "d", Type: TypeInstruction}, dir: "mismatch", body: "b", wantSub: "invalid skill name"},
		{name: "missing description", fm: Frontmatter{Name: "x", Type: TypeInstruction}, dir: "x", body: "b", wantSub: "missing description"},
		{name: "invalid type", fm: Frontmatter{Name: "x", Description: "d", Type: "weird"}, dir: "x", body: "b", wantSub: "invalid type"},
		{name: "empty body", fm: Frontmatter{Name: "x", Description: "d", Type: TypeInstruction}, dir: "x", body: "   \n\t", wantSub: "empty body"},
		{name: "body over cap", fm: Frontmatter{Name: "x", Description: "d", Type: TypeInstruction}, dir: "x", body: strings.Repeat("z", cap+1), wantSub: "exceeds cap"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			err := validateStructure(c.fm, c.dir, c.body, cap)
			if err == nil || !strings.Contains(err.Error(), c.wantSub) {
				t.Fatalf("validateStructure(%s) = %v, want substring %q", c.name, err, c.wantSub)
			}
		})
	}
	if err := validateStructure(Frontmatter{Name: "ok", Description: "d", Type: TypeSnippet}, "ok", "body", cap); err != nil {
		t.Fatalf("a fully-valid snippet must pass validateStructure, got %v", err)
	}
}

// TestScanRootSkipsNonDirAndMissingRoot drives scanRoot's non-skill branches: a plain
// file at the root (not a dir) is skipped, a dir without a SKILL.md is silently skipped,
// and a missing root is a no-op (not fatal). A real skill alongside still loads.
func TestScanRootSkipsNonDirAndMissingRoot(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeSkill(t, root, "real", skillMD("real", "A real skill."))
	// A plain file directly under the root (not a directory) — scanRoot skips it.
	writeFile(t, filepath.Join(root, "loose.txt"), "i am not a skill dir")
	// A directory with no SKILL.md — loadSkillDir returns ok=false (silent).
	if err := os.MkdirAll(filepath.Join(root, "empty-dir"), 0o750); err != nil {
		t.Fatal(err)
	}

	// A second, entirely missing root must not be fatal (no-op return).
	l := NewLoader(Config{Roots: []string{root, filepath.Join(root, "does-not-exist")}})
	got := l.List()
	if len(got) != 1 || got[0].Name != "real" {
		t.Fatalf("List = %v, want [real] (non-dir + empty-dir + missing root all skipped)", names(got))
	}
}

// TestRenderManifestOverflowFirstLineAlwaysRendered proves a single oversized first line
// is still emitted (rendered==0 guard) with the overflow tail when more remain — the cap
// never produces an empty manifest while skills exist.
func TestRenderManifestOverflowFirstLineAlwaysRendered(t *testing.T) {
	t.Parallel()
	in := []Skill{
		{Name: "aaa", Description: strings.Repeat("x", 200)},
		{Name: "bbb", Description: "second"},
	}
	out := RenderManifest(in, 10) // cap below even the first line
	if !strings.Contains(out, "- aaa:") {
		t.Fatalf("the first line must always render even past the cap:\n%s", out)
	}
	if !strings.Contains(out, "1 more") {
		t.Fatalf("the overflow tail must report the un-rendered remainder:\n%s", out)
	}
}
