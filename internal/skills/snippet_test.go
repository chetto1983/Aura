package skills

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chetto1983/aura/internal/scoring"
)

// TestValidSnippetLanguage asserts the language enum gate (D-20): the three canon
// languages + their common aliases pass; anything else is ErrInvalidStructure.
func TestValidSnippetLanguage(t *testing.T) {
	t.Parallel()
	good := map[string]SnippetLanguage{
		"python": LangPython, "py": LangPython, "python3": LangPython,
		"shell": LangShell, "bash": LangShell,
		"js": LangJS, "javascript": LangJS, "node": LangJS,
	}
	for in, want := range good {
		got, _, err := validSnippetLanguage(in)
		if err != nil {
			t.Fatalf("validSnippetLanguage(%q) errored: %v", in, err)
		}
		if got != want {
			t.Fatalf("validSnippetLanguage(%q) = %q, want %q", in, got, want)
		}
	}
	for _, bad := range []string{"ruby", "go", "", "perl", "c++"} {
		if _, _, err := validSnippetLanguage(bad); !errors.Is(err, ErrInvalidStructure) {
			t.Fatalf("validSnippetLanguage(%q) = %v, want ErrInvalidStructure", bad, err)
		}
	}
}

// TestSnippetSandboxPath asserts the stable in-sandbox path is under /skills/ with the
// language-correct extension (spike 005 by-path target).
func TestSnippetSandboxPath(t *testing.T) {
	t.Parallel()
	cases := map[SnippetLanguage]string{
		LangPython: "/skills/foo/foo.py",
		LangShell:  "/skills/foo/foo.sh",
		LangJS:     "/skills/foo/foo.js",
	}
	for lang, want := range cases {
		got, err := SnippetSandboxPath("foo", lang)
		if err != nil {
			t.Fatalf("SnippetSandboxPath(foo,%q): %v", lang, err)
		}
		if got != want {
			t.Fatalf("SnippetSandboxPath(foo,%q) = %q, want %q", lang, got, want)
		}
	}
}

// TestSnippetHostPath asserts the host-path resolver joins the export dir with the
// language-correct extension via filepath.Join (OS-correct separators). It is the
// host-primary (D-01) mirror of SnippetSandboxPath: same ext map, but rooted at
// AURA_SKILL_EXPORT_DIR instead of the in-container /skills mount.
func TestSnippetHostPath(t *testing.T) {
	t.Parallel()
	export := filepath.Join("home", "u", ".aura", "skills", "export")
	cases := map[SnippetLanguage]string{
		LangPython: filepath.Join(export, "calc", "calc.py"),
		LangShell:  filepath.Join(export, "calc", "calc.sh"),
		LangJS:     filepath.Join(export, "calc", "calc.js"),
	}
	for lang, want := range cases {
		got, err := SnippetHostPath("calc", lang, export)
		if err != nil {
			t.Fatalf("SnippetHostPath(calc,%q): %v", lang, err)
		}
		if got != want {
			t.Fatalf("SnippetHostPath(calc,%q) = %q, want %q", lang, got, want)
		}
	}
	// An unknown language returns the same structured error SnippetSandboxPath returns.
	if _, err := SnippetHostPath("calc", SnippetLanguage("ruby"), export); !errors.Is(err, ErrInvalidStructure) {
		t.Fatalf("SnippetHostPath(ruby) = %v, want ErrInvalidStructure", err)
	}
}

// TestSnippetInvocationResolvesInterpreter asserts SnippetInvocation maps the (aliased)
// language to the right interpreter + path.
func TestSnippetInvocationResolvesInterpreter(t *testing.T) {
	t.Parallel()
	path, interp, err := SnippetInvocation("calc", "py")
	if err != nil {
		t.Fatalf("SnippetInvocation: %v", err)
	}
	if path != "/skills/calc/calc.py" || interp != "python3" {
		t.Fatalf("SnippetInvocation = (%q,%q), want (/skills/calc/calc.py, python3)", path, interp)
	}
	if _, _, err := SnippetInvocation("x", "ruby"); !errors.Is(err, ErrInvalidStructure) {
		t.Fatalf("SnippetInvocation(ruby) = %v, want ErrInvalidStructure", err)
	}
}

// TestSnippetHostInvocationResolvesInterpreter pins the host-primary mirror of
// SnippetInvocation: aliases normalize once, then return an OS-correct export path
// plus the structured interpreter.
func TestSnippetHostInvocationResolvesInterpreter(t *testing.T) {
	t.Parallel()
	export := filepath.Join("tmp", "aura-skills")
	cases := []struct {
		name     string
		language string
		wantPath string
		wantExec string
	}{
		{name: "python alias", language: "python3", wantPath: filepath.Join(export, "calc", "calc.py"), wantExec: "python3"},
		{name: "shell alias", language: "bash", wantPath: filepath.Join(export, "calc", "calc.sh"), wantExec: "sh"},
		{name: "js alias", language: "javascript", wantPath: filepath.Join(export, "calc", "calc.js"), wantExec: "node"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path, interp, err := SnippetHostInvocation("calc", tc.language, export)
			if err != nil {
				t.Fatalf("SnippetHostInvocation(%q): %v", tc.language, err)
			}
			if path != tc.wantPath || interp != tc.wantExec {
				t.Fatalf("SnippetHostInvocation(%q) = (%q,%q), want (%q,%q)",
					tc.language, path, interp, tc.wantPath, tc.wantExec)
			}
		})
	}
	if _, _, err := SnippetHostInvocation("calc", "ruby", export); !errors.Is(err, ErrInvalidStructure) {
		t.Fatalf("SnippetHostInvocation(ruby) = %v, want ErrInvalidStructure", err)
	}
}

// TestSaveSnippetRejectsBadLanguage asserts SaveSnippet hard-rejects a non-enum
// language before any FS write (no pending dir created).
func TestSaveSnippetRejectsBadLanguage(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	_, err := w.SaveSnippet(t.Context(), "bad", "ruby", "puts 1", Frontmatter{Description: "d"}, AuditActor{ActorID: "cli"})
	if !errors.Is(err, ErrInvalidStructure) {
		t.Fatalf("SaveSnippet(ruby) = %v, want ErrInvalidStructure", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "active", "bad")); statErr == nil {
		t.Fatal("a rejected snippet must not land in the active root")
	}
}

// TestSaveSnippetRejectsBlocklistedCode asserts the write-boundary blocklist runs on
// the CODE (the executable surface), hard-rejecting a model-authored injection (no
// allowBlocklisted escape, T-11-03-E1). The test writer's blocklist is "<|im_start|>".
func TestSaveSnippetRejectsBlocklistedCode(t *testing.T) {
	t.Parallel()
	w, _ := newTestWriter(t)
	_, err := w.SaveSnippet(t.Context(), "evil", "python", "print('<|im_start|>')", Frontmatter{Description: "d"}, AuditActor{ActorID: "model"})
	if !errors.Is(err, ErrBlocklisted) {
		t.Fatalf("SaveSnippet with blocklisted code = %v, want ErrBlocklisted", err)
	}
}

// TestSaveSnippetTierAndNetworkSurfaced asserts a snippet save is classified RISKY and
// that needs_network is surfaced (D-20/D-37). Since amendment #97 neither branches — the
// tier is reported to the operator/model, not enforced — so this pins the classification
// and the two-file write shape. The audit tx is the db_integration tier's job, so the FS
// half is asserted through the staging fill directly, mirroring writer_test's nil-pool
// discipline.
func TestSaveSnippetTierAndNetworkSurfaced(t *testing.T) {
	t.Parallel()
	if tier := scoring.ComputeSkillTier(scoring.SkillCreate, "print(1)"); tier != scoring.Risky {
		t.Fatalf("snippet save tier = %q, want risky", tier)
	}

	fm := Frontmatter{Name: "neton", Description: "needs net", Type: TypeSnippet, Language: "python", NeedsNetwork: true}
	dir := t.TempDir()
	fill := writeFilesInto(map[string][]byte{
		"SKILL.md": skillFileBytes(fm, "docs"),
		"neton.py": []byte("import urllib"),
	})
	if err := fill(dir); err != nil {
		t.Fatalf("stage snippet files: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "SKILL.md")); err != nil {
		t.Fatalf("staged SKILL.md missing: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "neton.py"))
	if err != nil {
		t.Fatalf("staged code file missing: %v", err)
	}
	if string(data) != "import urllib" {
		t.Fatalf("staged code = %q, want the saved code", string(data))
	}
}

// TestUseSnippetReturnsPath asserts UseSnippet returns the HOST export-dir path (the
// D-01 host-primary by-path target the model runs via shell_exec) + interpreter + the
// docs body for an ACTIVE snippet, and errors for a non-snippet. The in-box path stays
// populated because a strict profile hands that one out instead.
func TestUseSnippetReturnsPath(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	// Materialize an active snippet by hand (the activate path is db_integration-gated).
	activeDir := filepath.Join(root, "active", "calc")
	if err := os.MkdirAll(activeDir, 0o750); err != nil {
		t.Fatal(err)
	}
	fm := Frontmatter{Name: "calc", Description: "adds", Type: TypeSnippet, Language: "python"}
	if err := os.WriteFile(filepath.Join(activeDir, "SKILL.md"), skillFileBytes(fm, "run it by path"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(activeDir, "calc.py"), []byte("print(40+2)"), 0o600); err != nil {
		t.Fatal(err)
	}

	use, err := w.UseSnippet("calc")
	if err != nil {
		t.Fatalf("UseSnippet: %v", err)
	}
	// D-01 host-primary: the primary by-path target is the HOST export-dir path the
	// model runs via shell_exec, derived fresh from the Writer's export dir.
	wantHost := filepath.Join(root, "export", "calc", "calc.py")
	if use.HostPath != wantHost {
		t.Fatalf("HostPath = %q, want %q", use.HostPath, wantHost)
	}
	// The in-box path stays populated: a strict profile hands this one to the model.
	if use.SandboxPath != "/skills/calc/calc.py" {
		t.Fatalf("SandboxPath = %q, want /skills/calc/calc.py (in-box path preserved)", use.SandboxPath)
	}
	if use.Interpreter != "python3" {
		t.Fatalf("Interpreter = %q, want python3", use.Interpreter)
	}
	if use.Instructions != "run it by path" {
		t.Fatalf("Instructions = %q, want the docs body", use.Instructions)
	}

	// A non-snippet skill is rejected.
	instrDir := filepath.Join(root, "active", "doc")
	_ = os.MkdirAll(instrDir, 0o750)
	ifm := Frontmatter{Name: "doc", Description: "d", Type: TypeInstruction}
	_ = os.WriteFile(filepath.Join(instrDir, "SKILL.md"), skillFileBytes(ifm, "an instruction"), 0o600)
	if _, err := w.UseSnippet("doc"); err == nil {
		t.Fatal("UseSnippet on an instruction skill must error")
	}
}

// TestUsageSidecarAtomicWriteAndStamp asserts the usage sidecar is written atomically
// (no stray temp file left) and StampUsage bumps use_count + last_used_at (D-19).
func TestUsageSidecarAtomicWriteAndStamp(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	name := "calc"

	// First read of an absent sidecar returns a zero-value active state (no error).
	u0, err := w.ReadUsage(name)
	if err != nil {
		t.Fatalf("ReadUsage(absent): %v", err)
	}
	if u0.UseCount != 0 || u0.Status != "active" {
		t.Fatalf("absent sidecar = %+v, want {active,0}", u0)
	}

	now := time.Date(2026, 6, 5, 12, 0, 0, 0, time.UTC)
	if err := w.StampUsage(name, now); err != nil {
		t.Fatalf("StampUsage 1: %v", err)
	}
	if err := w.StampUsage(name, now.Add(time.Hour)); err != nil {
		t.Fatalf("StampUsage 2: %v", err)
	}

	u, err := w.ReadUsage(name)
	if err != nil {
		t.Fatalf("ReadUsage: %v", err)
	}
	if u.UseCount != 2 {
		t.Fatalf("use_count = %d, want 2", u.UseCount)
	}
	if !u.LastUsedAt.Equal(now.Add(time.Hour)) {
		t.Fatalf("last_used_at = %s, want %s", u.LastUsedAt, now.Add(time.Hour))
	}

	// Atomic-write leaves no .usage-tmp-* residue in the skill dir.
	entries, _ := os.ReadDir(filepath.Join(root, "active", name))
	for _, e := range entries {
		if len(e.Name()) >= len(".usage-tmp-") && e.Name()[:len(".usage-tmp-")] == ".usage-tmp-" {
			t.Fatalf("stray temp file left after atomic write: %s", e.Name())
		}
	}

	// The on-disk sidecar is valid JSON with the expected shape.
	raw, err := os.ReadFile(w.usageSidecarPath(name))
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	var got UsageSidecar
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("sidecar is not valid JSON: %v", err)
	}
	if got.UseCount != 2 {
		t.Fatalf("on-disk use_count = %d, want 2", got.UseCount)
	}
}

// TestSetUsageStatus asserts the status field is rewritten while the counters survive
// (the TTL sweep marks a de-materialized snippet archived).
func TestSetUsageStatus(t *testing.T) {
	t.Parallel()
	w, _ := newTestWriter(t)
	name := "calc"
	if err := w.StampUsage(name, time.Now()); err != nil {
		t.Fatalf("StampUsage: %v", err)
	}
	if err := w.SetUsageStatus(name, "archived"); err != nil {
		t.Fatalf("SetUsageStatus: %v", err)
	}
	u, err := w.ReadUsage(name)
	if err != nil {
		t.Fatalf("ReadUsage: %v", err)
	}
	if u.Status != "archived" {
		t.Fatalf("status = %q, want archived", u.Status)
	}
	if u.UseCount != 1 {
		t.Fatalf("use_count = %d, want 1 (preserved across status change)", u.UseCount)
	}
}

// TestArchivedUsageStatusUpdatesMovedSidecarWithoutActiveGhost pins the TTL-sweep
// archive bug: after active/<name> is moved to archived/<name>, marking the moved
// sidecar archived must not recreate active/<name> as a ghost dir containing only
// .usage.json.
func TestArchivedUsageStatusUpdatesMovedSidecarWithoutActiveGhost(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	name := "calc"

	if err := w.StampUsage(name, time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC)); err != nil {
		t.Fatalf("StampUsage: %v", err)
	}
	activeDir := filepath.Join(root, "active", name)
	archivedDir := filepath.Join(root, "archived", name)
	if err := os.MkdirAll(filepath.Dir(archivedDir), 0o750); err != nil {
		t.Fatalf("mkdir archived root: %v", err)
	}
	if err := os.Rename(activeDir, archivedDir); err != nil {
		t.Fatalf("move active to archived: %v", err)
	}

	if err := w.setUsageStatusInRoot(w.archiveDir, name, "archived"); err != nil {
		t.Fatalf("set archived usage status: %v", err)
	}
	if _, statErr := os.Stat(activeDir); !os.IsNotExist(statErr) {
		t.Fatalf("marking archived sidecar must not recreate active ghost dir (stat err=%v)", statErr)
	}

	raw, err := os.ReadFile(filepath.Join(archivedDir, ".usage.json"))
	if err != nil {
		t.Fatalf("read archived sidecar: %v", err)
	}
	var u UsageSidecar
	if err := json.Unmarshal(raw, &u); err != nil {
		t.Fatalf("archived sidecar JSON: %v", err)
	}
	if u.Status != "archived" {
		t.Fatalf("archived sidecar status = %q, want archived", u.Status)
	}
	if u.UseCount != 1 {
		t.Fatalf("archived sidecar use_count = %d, want preserved count 1", u.UseCount)
	}
}

func TestSnippetIsStaleCases(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-24 * time.Hour)

	seed := func(name string, typ string) string {
		t.Helper()
		dir := filepath.Join(root, "active", name)
		fm := Frontmatter{Name: name, Description: "d", Type: typ, Language: "python"}
		writeFile(t, filepath.Join(dir, "SKILL.md"), string(skillFileBytes(fm, "docs")))
		return filepath.Join(dir, "SKILL.md")
	}

	seed("fresh", TypeSnippet)
	if err := w.writeUsageAtomic("fresh", UsageSidecar{Status: "active", LastUsedAt: cutoff.Add(time.Minute), UseCount: 1}); err != nil {
		t.Fatalf("write fresh usage: %v", err)
	}
	if stale, ok := w.snippetIsStale("fresh", cutoff); !ok || stale {
		t.Fatalf("fresh snippet stale=%v ok=%v, want stale=false ok=true", stale, ok)
	}

	seed("old", TypeSnippet)
	if err := w.writeUsageAtomic("old", UsageSidecar{Status: "active", LastUsedAt: cutoff.Add(-time.Minute), UseCount: 1}); err != nil {
		t.Fatalf("write old usage: %v", err)
	}
	if stale, ok := w.snippetIsStale("old", cutoff); !ok || !stale {
		t.Fatalf("old snippet stale=%v ok=%v, want stale=true ok=true", stale, ok)
	}

	seed("archived", TypeSnippet)
	if err := w.writeUsageAtomic("archived", UsageSidecar{Status: "archived", LastUsedAt: cutoff.Add(-time.Hour)}); err != nil {
		t.Fatalf("write archived usage: %v", err)
	}
	if stale, ok := w.snippetIsStale("archived", cutoff); ok || stale {
		t.Fatalf("archived snippet stale=%v ok=%v, want stale=false ok=false", stale, ok)
	}

	seed("instruction", TypeInstruction)
	if stale, ok := w.snippetIsStale("instruction", cutoff); ok || stale {
		t.Fatalf("instruction stale=%v ok=%v, want stale=false ok=false", stale, ok)
	}

	mtimed := seed("mtime-old", TypeSnippet)
	oldTime := cutoff.Add(-time.Hour)
	if err := os.Chtimes(mtimed, oldTime, oldTime); err != nil {
		t.Fatalf("chtimes mtime-old: %v", err)
	}
	if stale, ok := w.snippetIsStale("mtime-old", cutoff); !ok || !stale {
		t.Fatalf("mtime-old stale=%v ok=%v, want stale=true ok=true", stale, ok)
	}
}

func TestSweepExpiredSnippetsKeepsFreshActiveSnippets(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	now := time.Date(2026, 6, 12, 12, 0, 0, 0, time.UTC)

	freshDir := filepath.Join(root, "active", "fresh")
	fm := Frontmatter{Name: "fresh", Description: "d", Type: TypeSnippet, Language: "python"}
	writeFile(t, filepath.Join(freshDir, "SKILL.md"), string(skillFileBytes(fm, "docs")))
	if err := w.writeUsageAtomic("fresh", UsageSidecar{Status: "active", LastUsedAt: now.Add(-time.Hour)}); err != nil {
		t.Fatalf("write usage: %v", err)
	}

	writeFile(t, filepath.Join(root, "active", "doc", "SKILL.md"),
		string(skillFileBytes(Frontmatter{Name: "doc", Description: "d", Type: TypeInstruction}, "docs")))
	if err := os.MkdirAll(filepath.Join(root, "active", "pending"), 0o750); err != nil {
		t.Fatalf("mkdir pending sentinel: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "active", "Bad_Name"), 0o750); err != nil {
		t.Fatalf("mkdir invalid sentinel: %v", err)
	}

	res, err := w.SweepExpiredSnippets(t.Context(), 24*time.Hour, now, AuditActor{ActorID: "system"})
	if err != nil {
		t.Fatalf("SweepExpiredSnippets: %v", err)
	}
	if len(res.Archived) != 0 {
		t.Fatalf("Archived = %v, want none for fresh-only sweep", res.Archived)
	}
	if len(res.Kept) != 1 || res.Kept[0] != "fresh" {
		t.Fatalf("Kept = %v, want [fresh]", res.Kept)
	}
}
