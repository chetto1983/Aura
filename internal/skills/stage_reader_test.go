package skills

import (
	"os"
	"path/filepath"
	"testing"
)

// writeStageSkill writes a <stageDir>/<name>/SKILL.md with the given frontmatter +
// body, returning nothing — the helper fails the test on any write error.
func writeStageSkill(t *testing.T, stageDir, name, frontmatterBody string) {
	t.Helper()
	dir := filepath.Join(stageDir, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(frontmatterBody), 0o644); err != nil {
		t.Fatalf("write SKILL.md: %v", err)
	}
}

// TestListArchived_Metadata proves ListArchived returns display metadata (name/
// description/type/language + a content hash) for each archived skill, parsed via the
// frontmatter parser — and that it NEVER returns the body (GOV-02 prohibition #1: an
// archived body cannot enter context). The body string is unique so its absence from
// every returned field is observable.
func TestListArchived_Metadata(t *testing.T) {
	archiveDir := filepath.Join(t.TempDir(), "archived")

	const secretBody = "ARCHIVED_BODY_MUST_NEVER_LEAK_INTO_CONTEXT"
	writeStageSkill(t, archiveDir, "alpha",
		"---\nname: alpha\ndescription: First archived skill.\ntype: instruction\n---\n"+secretBody+"\n")
	writeStageSkill(t, archiveDir, "bravo",
		"---\nname: bravo\ndescription: A snippet skill.\ntype: snippet\nlanguage: python\n---\nprint('hi')\n")

	got, err := ListArchived(archiveDir)
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListArchived: want 2 skills, got %d (%+v)", len(got), got)
	}

	byName := map[string]StageSkill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	alpha, ok := byName["alpha"]
	if !ok {
		t.Fatalf("alpha missing from %+v", got)
	}
	if alpha.Description != "First archived skill." || alpha.Type != "instruction" {
		t.Errorf("alpha metadata: got desc=%q type=%q", alpha.Description, alpha.Type)
	}
	if alpha.ContentHash == "" {
		t.Error("alpha: want a non-empty content hash")
	}
	bravo := byName["bravo"]
	if bravo.Type != "snippet" || bravo.Language != "python" {
		t.Errorf("bravo metadata: got type=%q language=%q", bravo.Type, bravo.Language)
	}

	// The body must NEVER appear in any returned field — StageSkill carries no Body by
	// design, but assert no field smuggled it either (the reader never mounts a body).
	for _, s := range got {
		for _, field := range []string{s.Name, s.Description, s.Type, s.Language, s.ContentHash} {
			if field == secretBody {
				t.Fatalf("an archived body leaked into a StageSkill field: %+v", s)
			}
		}
	}
}

// TestListArchived_MissingDirIsEmpty proves a non-existent archive dir is an empty list,
// not an error (a fresh install has archived nothing).
func TestListArchived_MissingDirIsEmpty(t *testing.T) {
	got, err := ListArchived(filepath.Join(t.TempDir(), "archived"))
	if err != nil {
		t.Fatalf("ListArchived(missing dir): want nil error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListArchived(missing dir): want empty, got %+v", got)
	}
}

// TestListArchived_SkipsMalformedAndNonDir proves a dir without a parseable SKILL.md and
// a stray file are skipped (never fatal), mirroring the loader's scan tolerance.
func TestListArchived_SkipsMalformedAndNonDir(t *testing.T) {
	archiveDir := filepath.Join(t.TempDir(), "archived")
	if err := os.MkdirAll(archiveDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A dir with a malformed SKILL.md (no frontmatter name).
	writeStageSkill(t, archiveDir, "broken", "no frontmatter here\n")
	// A dir with no SKILL.md at all.
	if err := os.MkdirAll(filepath.Join(archiveDir, "empty"), 0o755); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}
	// A stray regular file (not a dir).
	if err := os.WriteFile(filepath.Join(archiveDir, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	// A valid one to prove the good entry still surfaces.
	writeStageSkill(t, archiveDir, "good",
		"---\nname: good\ndescription: ok\ntype: instruction\n---\nbody\n")

	got, err := ListArchived(archiveDir)
	if err != nil {
		t.Fatalf("ListArchived: %v", err)
	}
	if len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("want only [good], got %+v", got)
	}
}
