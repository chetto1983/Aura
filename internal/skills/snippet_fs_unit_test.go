package skills

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestSaveSnippetWriteFailureSurfaces proves SaveSnippet returns writeActive's error (no
// audit tx reached) when the active root is unconfigured — a valid language + clean code
// pass the pre-write gates, then the staging step fails pre-tx.
func TestSaveSnippetWriteFailureSurfaces(t *testing.T) {
	t.Parallel()
	w, _ := newTestWriter(t)
	w.activeDir = ""
	_, err := w.SaveSnippet(t.Context(), "calc", "python", "print(1)", Frontmatter{Description: "adds"}, AuditActor{ActorID: "cli"})
	if err == nil || !strings.Contains(err.Error(), "active dir not configured") {
		t.Fatalf("SaveSnippet w/o active dir = %v, want configured error", err)
	}
}

// TestSaveSnippetRejectsOversizedCode proves the write-boundary body cap runs on the CODE
// (not the docs frame): a snippet whose code exceeds the writer's bodyCap is a structured
// rejection before any FS write.
func TestSaveSnippetRejectsOversizedCode(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	w.bodyCapBytes = 16
	_, err := w.SaveSnippet(t.Context(), "big", "python", strings.Repeat("a=1\n", 100), Frontmatter{Description: "d"}, AuditActor{ActorID: "cli"})
	if !errors.Is(err, ErrInvalidStructure) {
		t.Fatalf("SaveSnippet oversized code = %v, want ErrInvalidStructure", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "active", "big")); statErr == nil {
		t.Fatal("a rejected oversized snippet must not land in the active root")
	}
}

// TestRenderSnippetDocsRendersAllFields drives renderSnippetDocs across every optional
// field branch (outputs/deps/tags/needs_network/needs_workspace), and the minimal case
// (no optional fields) — the docs frame the model reads at use-time.
func TestRenderSnippetDocsRendersAllFields(t *testing.T) {
	t.Parallel()
	full := Frontmatter{
		Language:       "python",
		OutputsDesc:    "a JSON object",
		Deps:           []string{"pandas", "numpy"},
		Tags:           []string{"data", "csv"},
		NeedsNetwork:   true,
		NeedsWorkspace: true,
	}
	out := renderSnippetDocs(full, snippetMeta{ext: "py", interpreter: "python3"})
	for _, want := range []string{"Executable snippet (python)", "python3", "Outputs: a JSON object", "Deps: pandas, numpy", "Tags: data, csv", "needs_network: true", "needs_workspace: true"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderSnippetDocs missing %q:\n%s", want, out)
		}
	}

	// Minimal snippet: only the lead line, no optional sections.
	min := renderSnippetDocs(Frontmatter{Language: "shell"}, snippetMeta{ext: "sh", interpreter: "sh"})
	if strings.Contains(min, "Outputs:") || strings.Contains(min, "Deps:") || strings.Contains(min, "Tags:") || strings.Contains(min, "needs_") {
		t.Fatalf("minimal snippet docs must omit optional sections:\n%s", min)
	}
	if !strings.Contains(min, "Executable snippet (shell)") {
		t.Fatalf("minimal snippet docs must still carry the lead line:\n%s", min)
	}
}

// TestUseSnippetErrorBranches drives UseSnippet's error paths with no DB: a traversal name
// is rejected up front (SanitizeName), a missing active SKILL.md surfaces a read error, a
// malformed-frontmatter SKILL.md surfaces a parse error, and an ACTIVE snippet whose
// language is unknown surfaces the structured language error.
func TestUseSnippetErrorBranches(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)

	if _, err := w.UseSnippet("../escape"); !errors.Is(err, ErrInvalidName) {
		t.Fatalf("UseSnippet bad name = %v, want ErrInvalidName", err)
	}
	if _, err := w.UseSnippet("ghost"); err == nil || !strings.Contains(err.Error(), "read active SKILL.md") {
		t.Fatalf("UseSnippet missing active = %v, want read error", err)
	}

	// Malformed SKILL.md (no frontmatter fence) → parse error.
	badDir := filepath.Join(root, "active", "broken")
	if err := os.MkdirAll(badDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, "SKILL.md"), []byte("no frontmatter here"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.UseSnippet("broken"); err == nil || !strings.Contains(err.Error(), "parse") {
		t.Fatalf("UseSnippet malformed = %v, want parse error", err)
	}

	// A snippet whose language is unknown (corrupt frontmatter that passed the type gate).
	badLangDir := filepath.Join(root, "active", "badlang")
	if err := os.MkdirAll(badLangDir, 0o750); err != nil {
		t.Fatal(err)
	}
	raw := "---\nname: badlang\ndescription: d\ntype: snippet\nlanguage: cobol\n---\nrun by path\n"
	if err := os.WriteFile(filepath.Join(badLangDir, "SKILL.md"), []byte(raw), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.UseSnippet("badlang"); !errors.Is(err, ErrInvalidStructure) {
		t.Fatalf("UseSnippet unknown language = %v, want ErrInvalidStructure", err)
	}
}

// TestSnippetCodeFileRejectsUnknownLanguage pins the SnippetCodeFile structured error for
// an out-of-enum SnippetLanguage (the by-path code-file resolver).
func TestSnippetCodeFileRejectsUnknownLanguage(t *testing.T) {
	t.Parallel()
	if _, err := SnippetCodeFile("calc", SnippetLanguage("cobol")); !errors.Is(err, ErrInvalidStructure) {
		t.Fatalf("SnippetCodeFile(cobol) = %v, want ErrInvalidStructure", err)
	}
	if _, err := SnippetSandboxPath("calc", SnippetLanguage("cobol")); !errors.Is(err, ErrInvalidStructure) {
		t.Fatalf("SnippetSandboxPath(cobol) = %v, want ErrInvalidStructure", err)
	}
}

// TestSweepExpiredSnippetsNoopBranches drives the non-archiving SweepExpiredSnippets
// branches with no DB: ttl<=0 disables the sweep, an unconfigured active root disables it,
// and a missing active root returns an empty result (no error).
func TestSweepExpiredSnippetsNoopBranches(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	now := time.Now()

	res, err := w.SweepExpiredSnippets(t.Context(), 0, now, AuditActor{ActorID: "system"})
	if err != nil || len(res.Archived) != 0 || len(res.Kept) != 0 {
		t.Fatalf("ttl<=0 sweep = (%+v, %v), want empty no-op", res, err)
	}

	w2, _ := newTestWriter(t)
	w2.activeDir = ""
	if res, err := w2.SweepExpiredSnippets(t.Context(), time.Hour, now, AuditActor{ActorID: "system"}); err != nil || len(res.Archived) != 0 {
		t.Fatalf("unconfigured active root sweep = (%+v, %v), want empty no-op", res, err)
	}

	// A configured-but-absent active root is not an error (the dir was never created).
	if err := os.RemoveAll(filepath.Join(root, "active")); err != nil {
		t.Fatal(err)
	}
	if res, err := w.SweepExpiredSnippets(t.Context(), time.Hour, now, AuditActor{ActorID: "system"}); err != nil || len(res.Archived) != 0 {
		t.Fatalf("absent active root sweep = (%+v, %v), want empty no-op", res, err)
	}
}

// TestReadUsageSidecarMalformedJSON proves a corrupt usage sidecar surfaces a JSON error
// (not a silent zero-value), and an absent sidecar returns the zero-value active state.
func TestReadUsageSidecarMalformedJSON(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	name := "calc"
	dir := filepath.Join(root, "active", name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".usage.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := w.ReadUsage(name); err == nil || !strings.Contains(err.Error(), "read usage") {
		t.Fatalf("ReadUsage on corrupt sidecar = %v, want a JSON read error", err)
	}

	// A status-less but otherwise valid sidecar defaults Status to active.
	if err := os.WriteFile(filepath.Join(dir, ".usage.json"), []byte(`{"use_count":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	u, err := w.ReadUsage(name)
	if err != nil {
		t.Fatalf("ReadUsage valid-no-status: %v", err)
	}
	if u.Status != "active" || u.UseCount != 3 {
		t.Fatalf("status-less sidecar = %+v, want {active,3}", u)
	}
}

// TestStampAndSetStatusPropagateReadError proves StampUsage and SetUsageStatus surface the
// ReadUsage error (corrupt sidecar) instead of clobbering the file.
func TestStampAndSetStatusPropagateReadError(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	name := "calc"
	dir := filepath.Join(root, "active", name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".usage.json"), []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := w.StampUsage(name, time.Now()); err == nil {
		t.Fatal("StampUsage on a corrupt sidecar must surface the read error")
	}
	if err := w.SetUsageStatus(name, "archived"); err == nil {
		t.Fatal("SetUsageStatus on a corrupt sidecar must surface the read error")
	}
}

// TestWriteUsageAtomicInDirNoCreateErrors drives the createDir=false validation branch:
// a missing dir and a non-directory parent both surface structured errors (the TTL-sweep
// archived-sidecar path must not create a ghost dir).
func TestWriteUsageAtomicInDirNoCreateErrors(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	// Missing dir: stat fails.
	if err := writeUsageAtomicInDir(filepath.Join(root, "missing"), "x", UsageSidecar{Status: "archived"}, false); err == nil || !strings.Contains(err.Error(), "stat dir") {
		t.Fatalf("writeUsageAtomicInDir missing dir = %v, want stat error", err)
	}

	// A file where a dir is expected: not-a-directory.
	filePath := filepath.Join(root, "afile")
	writeFile(t, filePath, "data")
	if err := writeUsageAtomicInDir(filePath, "x", UsageSidecar{Status: "archived"}, false); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("writeUsageAtomicInDir non-dir parent = %v, want not-a-directory error", err)
	}
}

// TestSetUsageStatusInRootMissingSidecar proves setUsageStatusInRoot creates an active
// sidecar from the zero-value default when none exists yet, then stamps the new status
// (the writeUsageAtomicInDir createDir=false path over a present dir).
func TestSetUsageStatusInRootMissingSidecar(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	name := "calc"
	dir := filepath.Join(root, "active", name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := w.setUsageStatusInRoot(filepath.Join(root, "active"), name, "archived"); err != nil {
		t.Fatalf("setUsageStatusInRoot: %v", err)
	}
	u, err := w.ReadUsage(name)
	if err != nil {
		t.Fatalf("ReadUsage: %v", err)
	}
	if u.Status != "archived" {
		t.Fatalf("status = %q, want archived", u.Status)
	}
}

// TestSnippetIsStaleConservativeKeeps proves snippetIsStale keeps a snippet of unknown age
// (no usage sidecar AND the SKILL.md mtime forced to zero is not reproducible, so this
// asserts the never-used fallback-to-mtime keep when fresh). A never-used fresh snippet is
// kept; this complements the existing stale cases.
func TestSnippetIsStaleNeverUsedFallsBackToMtime(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	now := time.Now()
	cutoff := now.Add(-24 * time.Hour)

	dir := filepath.Join(root, "active", "neverused")
	fm := Frontmatter{Name: "neverused", Description: "d", Type: TypeSnippet, Language: "python"}
	writeFile(t, filepath.Join(dir, "SKILL.md"), string(skillFileBytes(fm, "docs")))
	// No usage sidecar written: staleness falls back to the SKILL.md mtime (just now → fresh).
	stale, ok := w.snippetIsStale("neverused", cutoff)
	if !ok || stale {
		t.Fatalf("never-used fresh snippet: stale=%v ok=%v, want stale=false ok=true", stale, ok)
	}
}

// TestResumeAndDiscardPendingRejectBadName proves the ask_user resume channel funnels both
// accept and decline through the name chokepoint: an invalid name is rejected before any
// activation/discard FS or audit work (the accept path delegates to Activate, decline to
// DiscardPending — both SanitizeName first).
func TestResumeAndDiscardPendingRejectBadName(t *testing.T) {
	t.Parallel()
	w, _ := newTestWriter(t)
	h := NewResumeHandler(w)
	for _, bad := range []string{"../escape", "Bad_Name", "a/b", ""} {
		if err := h.Resume(t.Context(), ResumeAccept, bad, "tok", AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Resume(accept,%q) = %v, want ErrInvalidName", bad, err)
		}
		if err := h.Resume(t.Context(), ResumeDecline, bad, "tok", AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("Resume(decline,%q) = %v, want ErrInvalidName", bad, err)
		}
		if err := w.DiscardPending(t.Context(), bad, ApprovalCLI, "", AuditActor{ActorID: "cli"}); !errors.Is(err, ErrInvalidName) {
			t.Errorf("DiscardPending(%q) = %v, want ErrInvalidName", bad, err)
		}
	}
}
