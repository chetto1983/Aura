package skilladapters

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chetto1983/aura/internal/skills"
)

// writeSkillFile materializes <root>/<dir>/SKILL.md with the given content and
// returns the skill dir. It is the realistic on-disk fixture builder the loader
// adapter scans over (mirrors internal/skills.writeSkill).
func writeSkillFile(t *testing.T, root, dir, content string) {
	t.Helper()
	skillDir := filepath.Join(root, dir)
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatalf("mkdir %q: %v", skillDir, err)
	}
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// instructionMD renders a minimal valid instruction SKILL.md.
func instructionMD(name, desc, body string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\ntype: instruction\n---\n" + body + "\n"
}

// snippetMD renders a minimal valid snippet SKILL.md (the loader resolves its
// language to a by-path interpreter; the body is the docs frame, not the code).
func snippetMD(name, desc, lang, body string) string {
	return "---\nname: " + name + "\ndescription: " + desc + "\ntype: snippet\nlanguage: " + lang + "\n---\n" + body + "\n"
}

// newLoaderAdapter builds a Loader adapter over a real *skills.Loader scanning a
// temp root, with the supplied manifest cap.
func newLoaderAdapter(t *testing.T, manCap int, seed func(root string)) *Loader {
	t.Helper()
	root := t.TempDir()
	if seed != nil {
		seed(root)
	}
	live := skills.NewLoader(skills.Config{Roots: []string{root}})
	return NewLoader(live, manCap)
}

func TestNewLoaderStoresFields(t *testing.T) {
	t.Parallel()
	live := skills.NewLoader(skills.Config{Roots: []string{t.TempDir()}})
	a := NewLoader(live, 4096)
	if a.loader != live {
		t.Error("NewLoader did not retain the live loader")
	}
	if a.manCap != 4096 {
		t.Errorf("manCap = %d, want 4096", a.manCap)
	}
}

func TestLoaderListProjectsToSkillMeta(t *testing.T) {
	t.Parallel()
	a := newLoaderAdapter(t, 0, func(root string) {
		writeSkillFile(t, root, "alpha", instructionMD("alpha", "Alpha skill.", "Do alpha."))
		writeSkillFile(t, root, "beta", instructionMD("beta", "Beta skill.", "Do beta."))
	})

	got := a.List()
	if len(got) != 2 {
		t.Fatalf("List() len = %d, want 2; got %+v", len(got), got)
	}
	// The live loader returns name-sorted order, so alpha precedes beta.
	byName := map[string]string{}
	for _, m := range got {
		byName[m.Name] = m.Description
	}
	if byName["alpha"] != "Alpha skill." {
		t.Errorf("alpha description = %q, want %q", byName["alpha"], "Alpha skill.")
	}
	if byName["beta"] != "Beta skill." {
		t.Errorf("beta description = %q, want %q", byName["beta"], "Beta skill.")
	}
}

func TestLoaderListEmpty(t *testing.T) {
	t.Parallel()
	a := newLoaderAdapter(t, 0, nil)
	got := a.List()
	if len(got) != 0 {
		t.Fatalf("List() over an empty root = %d entries, want 0", len(got))
	}
}

func TestLoaderBodyFoundAndAbsent(t *testing.T) {
	t.Parallel()
	a := newLoaderAdapter(t, 0, func(root string) {
		writeSkillFile(t, root, "present", instructionMD("present", "A present skill.", "The real body line."))
	})

	body, ok := a.Body("present")
	if !ok {
		t.Fatal("Body(present) ok = false, want true")
	}
	if !strings.Contains(body, "The real body line.") {
		t.Errorf("Body(present) = %q, want it to contain the markdown body", body)
	}

	missingBody, ok := a.Body("absent")
	if ok {
		t.Errorf("Body(absent) ok = true, want false")
	}
	if missingBody != "" {
		t.Errorf("Body(absent) = %q, want empty string", missingBody)
	}
}

func TestLoaderManifestDescriptionRendersAndRespectsCap(t *testing.T) {
	t.Parallel()
	a := newLoaderAdapter(t, 0, func(root string) {
		writeSkillFile(t, root, "zeta", instructionMD("zeta", "Zeta does Z.", "Z body."))
		writeSkillFile(t, root, "alpha", instructionMD("alpha", "Alpha does A.", "A body."))
	})

	man := a.ManifestDescription()
	// Alphabetical, name+description block (the byte-stable manifest contract).
	if !strings.Contains(man, "- alpha: Alpha does A.") {
		t.Errorf("manifest missing alpha line; got:\n%s", man)
	}
	if !strings.Contains(man, "- zeta: Zeta does Z.") {
		t.Errorf("manifest missing zeta line; got:\n%s", man)
	}
	ai := strings.Index(man, "alpha")
	zi := strings.Index(man, "zeta")
	if ai < 0 || zi < 0 || ai > zi {
		t.Errorf("manifest not alphabetical: alpha at %d, zeta at %d", ai, zi)
	}
}

func TestLoaderManifestDescriptionTinyCapOverflows(t *testing.T) {
	t.Parallel()
	// A 1-byte cap forces the overflow tail after the first (always-rendered) line.
	a := newLoaderAdapter(t, 1, func(root string) {
		writeSkillFile(t, root, "alpha", instructionMD("alpha", "Alpha does A.", "A body."))
		writeSkillFile(t, root, "beta", instructionMD("beta", "Beta does B.", "B body."))
	})
	man := a.ManifestDescription()
	if !strings.Contains(man, "more — search with skill action=list") {
		t.Errorf("tiny manCap should produce the overflow tail; got:\n%s", man)
	}
}

// The adapter resolves the IN-BOX path only. It used to return the host export-dir path too and
// action=use chose between them; shell_exec runs in one place now, so there is one path to render.
func TestLoaderSnippetResolvesSandboxInvocation(t *testing.T) {
	t.Parallel()
	a := newLoaderAdapter(t, 0, func(root string) {
		writeSkillFile(t, root, "fetcher", snippetMD("fetcher", "Fetch a URL.", "python", "Run the fetcher."))
	})

	instructions, sandboxPath, interpreter, ok := a.Snippet("fetcher")
	if !ok {
		t.Fatal("Snippet(fetcher) ok = false, want true for an active python snippet")
	}
	if !strings.Contains(instructions, "Run the fetcher.") {
		t.Errorf("instructions = %q, want the SKILL.md body", instructions)
	}
	if interpreter != "python3" {
		t.Errorf("interpreter = %q, want python3", interpreter)
	}
	// Sandbox path is the in-box /skills root MaterializeIn lands the snippet at (D-10).
	if sandboxPath != "/skills/fetcher/fetcher.py" {
		t.Errorf("sandboxPath = %q, want /skills/fetcher/fetcher.py", sandboxPath)
	}
}

func TestLoaderSnippetResolvesShellAndJS(t *testing.T) {
	t.Parallel()
	a := newLoaderAdapter(t, 0, func(root string) {
		writeSkillFile(t, root, "cleaner", snippetMD("cleaner", "Clean tmp.", "shell", "Run the cleaner."))
		writeSkillFile(t, root, "builder", snippetMD("builder", "Build it.", "js", "Run the builder."))
	})

	_, shSandbox, shInterp, ok := a.Snippet("cleaner")
	if !ok || shInterp != "sh" {
		t.Fatalf("Snippet(cleaner) ok=%v interp=%q, want ok + sh", ok, shInterp)
	}
	if shSandbox != "/skills/cleaner/cleaner.sh" {
		t.Errorf("shell sandbox path = %q", shSandbox)
	}

	_, jsSandbox, jsInterp, ok := a.Snippet("builder")
	if !ok || jsInterp != "node" {
		t.Fatalf("Snippet(builder) ok=%v interp=%q, want ok + node", ok, jsInterp)
	}
	if jsSandbox != "/skills/builder/builder.js" {
		t.Errorf("js sandbox path = %q", jsSandbox)
	}
}

func TestLoaderSnippetAbsentSkill(t *testing.T) {
	t.Parallel()
	a := newLoaderAdapter(t, 0, nil)
	instructions, sandboxPath, interpreter, ok := a.Snippet("ghost")
	if ok {
		t.Error("Snippet(ghost) ok = true, want false for an absent skill")
	}
	if instructions != "" || sandboxPath != "" || interpreter != "" {
		t.Errorf("absent snippet should return all-empty, got (%q,%q,%q)", instructions, sandboxPath, interpreter)
	}
}

func TestLoaderSnippetNonSnippetSkill(t *testing.T) {
	t.Parallel()
	a := newLoaderAdapter(t, 0, func(root string) {
		// An instruction skill is NOT a snippet → ok=false (use falls back to the
		// instruction authority-frame path).
		writeSkillFile(t, root, "guide", instructionMD("guide", "A guide.", "Just instructions."))
	})
	_, _, _, ok := a.Snippet("guide")
	if ok {
		t.Error("Snippet(guide) ok = true, want false for a non-snippet skill")
	}
}

func TestLoaderSnippetInvalidLanguageFails(t *testing.T) {
	t.Parallel()
	// A snippet whose language is outside {python,shell,js} cannot resolve a
	// by-path interpreter, so SnippetInvocation errors and ok=false.
	a := newLoaderAdapter(t, 0, func(root string) {
		writeSkillFile(t, root, "weird", snippetMD("weird", "Weird lang.", "ruby", "Run weird."))
	})
	instructions, sandboxPath, interpreter, ok := a.Snippet("weird")
	if ok {
		t.Error("Snippet(weird) ok = true, want false for an unsupported language")
	}
	if instructions != "" || sandboxPath != "" || interpreter != "" {
		t.Errorf("invalid-language snippet should return all-empty, got (%q,%q,%q)", instructions, sandboxPath, interpreter)
	}
}
