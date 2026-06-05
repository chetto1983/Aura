package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// smoke_test.go holds the two CI-runnable Phase-11 smokes the D-35 dual gate pairs with
// the (operator-run, paid) xlsx North-Star E2E: the haiku always-on flow and the snippet
// save→by-path reuse flow. Both avoid the LLM and the live sandbox so they run in the
// default unit tier (`go test -race ./internal/skills/`) under CI's no-skip-as-green
// discipline — the live audit-tx (db_integration) + the live by-path exec
// (sandbox_integration) are exercised separately by the tagged tiers (11-04..11-07).
//
// What each smoke proves at the skills boundary (the cross-package wire-up —
// messages[1] protection in internal/conversations, the live exec in the sandbox-agent
// — is covered by those packages' own tests; these assert the SKILLS-side contract):
//
//   - TestHaikuFlow: a model-authored always:true skill, once active (the create→approve
//     promotion the resume handler / CLI drive), renders into the messages[1] always-block
//     (RenderAlwaysBlock) — the byte-stable, alphabetical, header-led block the runner
//     injects as the protected seq=2 turn so the next turn carries the haiku instruction.
//   - TestSnippetReuse: a saved snippet, once active, resolves to the SAME stable
//     /skills/<name>/<name>.<ext> by-path invocation on every UseSnippet call (the
//     deterministic reuse the model/CLI exec against — interpreter + path, never the exec
//     bit).

// activateOnDisk simulates the create→approve promotion at the FS level (the loader
// scans the active root; pending→active is a dir move). It writes the SKILL.md + any
// code file directly into the active root so the unit smoke exercises the loader +
// render contract WITHOUT the live audit tx (db_integration) — the activation audit row
// is asserted by TestWriterActivateAuditRow (db_integration).
func activateOnDisk(t *testing.T, activeRoot, name string, fm Frontmatter, body string, codeFile, code string) {
	t.Helper()
	dir := filepath.Join(activeRoot, name)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir active skill dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), skillFileBytes(fm, body), 0o600); err != nil {
		t.Fatalf("write active SKILL.md: %v", err)
	}
	if codeFile != "" {
		if err := os.WriteFile(filepath.Join(dir, codeFile), []byte(code), 0o600); err != nil {
			t.Fatalf("write active code file: %v", err)
		}
	}
}

// TestHaikuFlow is the create→approve→messages[1]→next-turn smoke (D-35). An author
// creates an always:true "haiku" skill; once approved (active on disk), the loader picks
// it up and RenderAlwaysBlock emits its body into the byte-stable messages[1] block — so
// the very next assembled turn steers the model into haiku mode. A non-always skill in
// the same root MUST NOT leak into the block.
func TestHaikuFlow(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	const haikuBody = "Respond ONLY in haiku: three lines, 5-7-5 syllables. No prose."
	activateOnDisk(t, root, "haiku",
		Frontmatter{Name: "haiku", Description: "answer in haiku", Type: TypeInstruction, Always: true},
		haikuBody, "", "")
	// A second, NON-always skill that must stay out of the always-block.
	activateOnDisk(t, root, "verbose",
		Frontmatter{Name: "verbose", Description: "be verbose", Type: TypeInstruction, Always: false},
		"Write long, detailed answers.", "", "")

	loader := NewLoader(Config{Roots: []string{root}})
	loaded := loader.List()

	// The active always:true skill is loaded with Always=true (the create→approve result
	// the loader sees on the next turn).
	var haiku *Skill
	for i := range loaded {
		if loaded[i].Name == "haiku" {
			haiku = &loaded[i]
		}
	}
	if haiku == nil {
		t.Fatalf("haiku skill not loaded from %s; loaded=%v", root, names(loaded))
	}
	if !haiku.Always {
		t.Fatalf("haiku skill must load with Always=true, got %+v", *haiku)
	}

	// RenderAlwaysBlock is the messages[1] assembly seam (D-07): the haiku body is in the
	// block under the frozen header; the non-always skill is excluded.
	block, present := RenderAlwaysBlock(loaded)
	if !present {
		t.Fatal("always-block must be present when an active always:true skill exists")
	}
	if !strings.HasPrefix(block, alwaysBlockHeader) {
		t.Errorf("always-block must lead with the frozen header, got:\n%s", block)
	}
	if !strings.Contains(block, haikuBody) {
		t.Errorf("always-block must carry the haiku body (the next-turn steer):\n%s", block)
	}
	if strings.Contains(block, "Write long, detailed answers.") {
		t.Errorf("non-always skill body leaked into the always-block:\n%s", block)
	}

	// Byte-stable across calls (the CAP-04 invariant the runner's messages[1] depends on):
	// re-rendering the SAME loaded set yields identical bytes.
	block2, _ := RenderAlwaysBlock(loaded)
	if block != block2 {
		t.Errorf("always-block not byte-stable across renders:\n%q\nvs\n%q", block, block2)
	}
}

// TestSnippetReuse is the snippet save→by-path reuse smoke (D-35). A snippet is saved
// (write-boundary validated, the gate decision is RISKY) and, once active, resolves to
// the SAME stable /skills by-path invocation on every UseSnippet call — the
// deterministic reuse the model/CLI exec against (interpreter + path). The live exec +
// usage stamp are TestSnippetExec (sandbox_integration); this asserts the by-path
// resolution + reuse without the container.
func TestSnippetReuse(t *testing.T) {
	t.Parallel()
	w, root := newTestWriter(t)

	// Save (FS round-trip; the nil-pool audit tx is exercised by the db_integration tier,
	// so we save the pending tree directly through writePendingSnippet — the same bytes
	// SaveSnippet writes — to keep this smoke LLM-free AND DB-free).
	const code = "print('reuse-ok 42')\n"
	fm := Frontmatter{Name: "calc", Description: "adds", Type: TypeSnippet, Language: "python"}
	if err := w.writePendingSnippet("calc", fm, renderSnippetDocs(fm, snippetMetaByLang[LangPython]), code, "py"); err != nil {
		t.Fatalf("writePendingSnippet: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "pending", "calc", "calc.py")); err != nil {
		t.Fatalf("pending snippet code file missing: %v", err)
	}

	// Approve → active (the FS promotion the resume handler / CLI drive; the audit row is
	// db_integration). Place the active snippet directly so the unit smoke is DB-free. The
	// Writer's active root is <root>/active (newTestWriter), the same dir UseSnippet reads.
	activateOnDisk(t, filepath.Join(root, "active"), "calc", fm, renderSnippetDocs(fm, snippetMetaByLang[LangPython]), "calc.py", code)

	// First use: the by-path invocation the model execs against.
	u1, err := w.UseSnippet("calc")
	if err != nil {
		t.Fatalf("UseSnippet (first): %v", err)
	}
	if u1.SandboxPath != "/skills/calc/calc.py" || u1.Interpreter != "python3" {
		t.Fatalf("UseSnippet = (%q,%q), want (/skills/calc/calc.py, python3)", u1.SandboxPath, u1.Interpreter)
	}

	// Reuse: a second use returns the SAME stable by-path invocation (deterministic reuse
	// — the model/CLI re-exec the identical interpreter + path).
	u2, err := w.UseSnippet("calc")
	if err != nil {
		t.Fatalf("UseSnippet (reuse): %v", err)
	}
	if u2.SandboxPath != u1.SandboxPath || u2.Interpreter != u1.Interpreter {
		t.Fatalf("by-path invocation not stable across reuse: %+v vs %+v", u1, u2)
	}
}
