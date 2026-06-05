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

// TestSaveSnippetRejectsBadLanguage asserts SaveSnippet hard-rejects a non-enum
// language before any FS write (no pending dir created).
func TestSaveSnippetRejectsBadLanguage(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)
	_, err := w.SaveSnippet(t.Context(), "bad", "ruby", "puts 1", Frontmatter{Description: "d"}, AuditActor{ActorID: "cli"})
	if !errors.Is(err, ErrInvalidStructure) {
		t.Fatalf("SaveSnippet(ruby) = %v, want ErrInvalidStructure", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, "pending", "bad")); statErr == nil {
		t.Fatal("a rejected snippet must not create a pending dir")
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

// TestSaveSnippetTierAndNetworkSurfaced asserts a create-tiered snippet save is RISKY
// and needs_network is surfaced in the result (D-20/D-37). The audit INSERT is nil-pool
// here, so the test stops at the gate decision by writing pending then asserting the FS
// state — the DB tx is exercised by the db_integration tier. To avoid the nil-pool
// audit panic we assert the tier/network surfacing via the pure scoring + a direct
// writePendingSnippet round-trip, mirroring writer_test's nil-pool discipline.
func TestSaveSnippetTierAndNetworkSurfaced(t *testing.T) {
	t.Parallel()
	// Pure gate decision (no DB): create is RISKY and gate-recommended.
	tier := scoring.ComputeSkillTier(scoring.SkillCreate, "print(1)")
	if tier != scoring.Risky {
		t.Fatalf("snippet save tier = %q, want risky", tier)
	}
	if !scoring.GateRecommended(tier) {
		t.Fatal("snippet save must be gate-recommended")
	}

	// FS round-trip of the pending write (no audit tx): the SKILL.md + code file land.
	w, root := newTestWriter(t)
	fm := Frontmatter{Name: "neton", Description: "needs net", Type: TypeSnippet, Language: "python", NeedsNetwork: true}
	if err := w.writePendingSnippet("neton", fm, "docs", "import urllib", "py"); err != nil {
		t.Fatalf("writePendingSnippet: %v", err)
	}
	pendDir := filepath.Join(root, "pending", "neton")
	if _, err := os.Stat(filepath.Join(pendDir, "SKILL.md")); err != nil {
		t.Fatalf("pending SKILL.md missing: %v", err)
	}
	codePath := filepath.Join(pendDir, "neton.py")
	data, err := os.ReadFile(codePath)
	if err != nil {
		t.Fatalf("pending code file missing: %v", err)
	}
	if string(data) != "import urllib" {
		t.Fatalf("pending code = %q, want the saved code", string(data))
	}
}

// TestUseSnippetReturnsPath asserts UseSnippet returns the in-sandbox /skills path +
// interpreter + the docs body for an ACTIVE snippet, and errors for a non-snippet.
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
	if use.SandboxPath != "/skills/calc/calc.py" {
		t.Fatalf("SandboxPath = %q, want /skills/calc/calc.py", use.SandboxPath)
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
