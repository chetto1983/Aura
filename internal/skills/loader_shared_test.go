package skills

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// loader_shared_test.go covers the reading half of amendment #214 criterion 5: a skill
// somebody shared is loadable by name from the reader's own Loader, and it loses to every
// skill the reader already had.

// sharedLoaderFixture builds a loader over one identity root, one global root, and the shared
// dirs handed in.
func sharedLoaderFixture(t *testing.T, sharedDirs ...string) (*Loader, string, string) {
	t.Helper()
	identityRoot := t.TempDir()
	globalRoot := t.TempDir()
	return NewLoader(Config{
		Roots:      []string{identityRoot, globalRoot},
		SharedDirs: sharedDirs,
	}), identityRoot, globalRoot
}

// TestLoaderReadsASharedSkill is the listing half of criterion 5: the body under ANOTHER
// identity's export is readable by name from this reader's loader.
func TestLoaderReadsASharedSkill(t *testing.T) {
	t.Parallel()
	ownerExport := t.TempDir()
	shared := writeSkillTree(t, ownerExport, "borrowed", "BORROWED BODY")
	// The owner's neighbouring skill is NOT in the shared list and must not become readable
	// just because its sibling was shared.
	writeSkillTree(t, ownerExport, "not-borrowed", "PRIVATE BODY")

	loader, _, _ := sharedLoaderFixture(t, shared)
	got, ok := loader.Get("borrowed")
	if !ok {
		t.Fatal("a shared skill is not readable — criterion 5's listing half is open")
	}
	if got.Body != "BORROWED BODY\n" || got.Dir != shared {
		t.Fatalf("shared skill = %+v, want the owner's body from the owner's dir", got)
	}
	if _, leaked := loader.Get("not-borrowed"); leaked {
		t.Fatal("a skill BESIDE the shared one became readable — the source is a tree, not a skill")
	}
	names := make([]string, 0, 1)
	for _, s := range loader.List() {
		names = append(names, s.Name)
	}
	if len(names) != 1 || names[0] != "borrowed" {
		t.Fatalf("List = %v, want only the shared skill", names)
	}
}

// TestLoaderSharedSkillLosesEveryCollision is the precedence rule. A share is the weakest
// claim on a name: the reader's own skill wins, and so does the deployment's, because a
// share that could shadow either is a way to replace the instructions somebody already
// relies on without touching their library.
func TestLoaderSharedSkillLosesEveryCollision(t *testing.T) {
	t.Parallel()
	ownerExport := t.TempDir()
	shared := writeSkillTree(t, ownerExport, "deploy", "SHARED BODY")

	t.Run("the reader's own skill wins", func(t *testing.T) {
		t.Parallel()
		loader, identityRoot, _ := sharedLoaderFixture(t, shared)
		writeSkillTree(t, identityRoot, "deploy", "MY BODY")
		got, _ := loader.Get("deploy")
		if got.Body != "MY BODY\n" {
			t.Fatalf("deploy resolved to %q, want the reader's own body", got.Body)
		}
	})

	t.Run("the deployment's skill wins", func(t *testing.T) {
		t.Parallel()
		loader, _, globalRoot := sharedLoaderFixture(t, shared)
		writeSkillTree(t, globalRoot, "deploy", "HOUSE BODY")
		got, _ := loader.Get("deploy")
		if got.Body != "HOUSE BODY\n" {
			t.Fatalf("deploy resolved to %q, want house policy (D-214-3)", got.Body)
		}
	})
}

// TestLoaderSharedSkillIsNeverAlwaysOn is the boundary a share must not cross. always:true
// means "prepend this to every turn"; it is a statement its owner makes about THEIR turns.
// Carrying it across a grant would let one identity — with a single public grant — prepend
// their text to every turn of everybody in the deployment, which is the exact injection
// amendment #214 exists to close, re-opened by a verb that sounds harmless.
func TestLoaderSharedSkillIsNeverAlwaysOn(t *testing.T) {
	t.Parallel()
	ownerExport := t.TempDir()
	dir := filepath.Join(ownerExport, "loud")
	writeAlwaysSkill(t, dir, "loud", "LOUD BODY")

	loader, identityRoot, _ := sharedLoaderFixture(t, dir)
	// The reader's OWN always-on skill is the control: the flag still works, it is only the
	// shared one that loses it.
	writeAlwaysSkill(t, filepath.Join(identityRoot, "mine"), "mine", "MY BODY")

	got, ok := loader.Get("loud")
	if !ok {
		t.Fatal("the shared skill did not load at all")
	}
	if got.Always {
		t.Fatal("a shared skill is always-on for its grantee — a public grant would prepend one person's text to everybody's turns")
	}
	if mine, _ := loader.Get("mine"); !mine.Always {
		t.Fatal("the reader's own always:true skill lost its flag — the strip is not scoped to shares")
	}
	block, present := RenderAlwaysBlock(loader.List())
	if !present || !strings.Contains(block, "MY BODY") {
		t.Fatalf("the reader's own always-block is missing:\n%s", block)
	}
	if strings.Contains(block, "LOUD BODY") {
		t.Fatalf("a shared body reached the always-block:\n%s", block)
	}
}

// TestLoaderSharedDirIgnoresNonSkills keeps the degenerate entries harmless: a blank path, a
// directory that has gone away (a revoked share resolved a moment ago), and a directory with
// no SKILL.md are skipped, never fatal.
func TestLoaderSharedDirIgnoresNonSkills(t *testing.T) {
	t.Parallel()
	gone := filepath.Join(t.TempDir(), "revoked")
	empty := t.TempDir()
	loader, _, _ := sharedLoaderFixture(t, "", "   ", gone, empty)
	if got := loader.List(); len(got) != 0 {
		t.Fatalf("List = %+v, want nothing loadable from a blank, missing or bodiless dir", got)
	}
}

// writeAlwaysSkill writes a skill dir whose frontmatter carries always: true.
func writeAlwaysSkill(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatalf("mkdir %q: %v", dir, err)
	}
	md := "---\nname: " + name + "\ndescription: " + name + " description\ntype: instruction\nalways: true\n---\n" + body + "\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(md), 0o600); err != nil {
		t.Fatalf("write %q: %v", name, err)
	}
}
