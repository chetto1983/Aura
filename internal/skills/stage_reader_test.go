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

// TestStageReader_ListsPendingMetadata proves ListStage("pending") returns display
// metadata (name/description/type/language + a content hash) for each pending skill,
// parsed via the frontmatter parser — and that it NEVER returns the body (GOV-02
// prohibition #1: a pending body cannot enter context). The body string is unique so
// its absence from every returned field is observable.
func TestStageReader_ListsPendingMetadata(t *testing.T) {
	root := t.TempDir()
	pendingDir := filepath.Join(root, "pending")
	archiveDir := filepath.Join(root, "archived")

	const secretBody = "PENDING_BODY_MUST_NEVER_LEAK_INTO_CONTEXT"
	writeStageSkill(t, pendingDir, "alpha",
		"---\nname: alpha\ndescription: First pending skill.\ntype: instruction\n---\n"+secretBody+"\n")
	writeStageSkill(t, pendingDir, "bravo",
		"---\nname: bravo\ndescription: A snippet skill.\ntype: snippet\nlanguage: python\n---\nprint('hi')\n")

	got, err := ListStage(pendingDir, archiveDir, StagePending)
	if err != nil {
		t.Fatalf("ListStage(pending): %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ListStage(pending): want 2 skills, got %d (%+v)", len(got), got)
	}

	byName := map[string]StageSkill{}
	for _, s := range got {
		byName[s.Name] = s
	}
	alpha, ok := byName["alpha"]
	if !ok {
		t.Fatalf("alpha missing from %+v", got)
	}
	if alpha.Description != "First pending skill." || alpha.Type != "instruction" {
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
				t.Fatalf("a pending body leaked into a StageSkill field: %+v", s)
			}
		}
	}
}

// TestStageReader_ListsArchived proves the archived stage reads from archiveDir.
func TestStageReader_ListsArchived(t *testing.T) {
	root := t.TempDir()
	pendingDir := filepath.Join(root, "pending")
	archiveDir := filepath.Join(root, "archived")
	writeStageSkill(t, archiveDir, "oldskill",
		"---\nname: oldskill\ndescription: An archived skill.\ntype: instruction\n---\nbody\n")

	got, err := ListStage(pendingDir, archiveDir, StageArchived)
	if err != nil {
		t.Fatalf("ListStage(archived): %v", err)
	}
	if len(got) != 1 || got[0].Name != "oldskill" {
		t.Fatalf("ListStage(archived): want [oldskill], got %+v", got)
	}
}

// TestStageReader_MissingDirIsEmpty proves a non-existent stage dir is an empty list,
// not an error (a fresh install has no pending/archived skills).
func TestStageReader_MissingDirIsEmpty(t *testing.T) {
	root := t.TempDir() // exists but has no pending/ or archived/ subdirs
	got, err := ListStage(filepath.Join(root, "pending"), filepath.Join(root, "archived"), StagePending)
	if err != nil {
		t.Fatalf("ListStage(missing dir): want nil error, got %v", err)
	}
	if len(got) != 0 {
		t.Errorf("ListStage(missing dir): want empty, got %+v", got)
	}
}

// TestStageReader_UnknownStageErrors proves an unknown stage name is a clean error.
func TestStageReader_UnknownStageErrors(t *testing.T) {
	root := t.TempDir()
	if _, err := ListStage(filepath.Join(root, "pending"), filepath.Join(root, "archived"), "active"); err == nil {
		t.Error("ListStage(unknown stage): want error, got nil")
	}
}

// TestStageReader_SkipsMalformedAndNonDir proves a dir without a parseable SKILL.md and
// a stray file are skipped (never fatal), mirroring the loader's scan tolerance.
func TestStageReader_SkipsMalformedAndNonDir(t *testing.T) {
	root := t.TempDir()
	pendingDir := filepath.Join(root, "pending")
	if err := os.MkdirAll(pendingDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A dir with a malformed SKILL.md (no frontmatter name).
	writeStageSkill(t, pendingDir, "broken", "no frontmatter here\n")
	// A dir with no SKILL.md at all.
	if err := os.MkdirAll(filepath.Join(pendingDir, "empty"), 0o755); err != nil {
		t.Fatalf("mkdir empty: %v", err)
	}
	// A stray regular file (not a dir).
	if err := os.WriteFile(filepath.Join(pendingDir, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write stray: %v", err)
	}
	// A valid one to prove the good entry still surfaces.
	writeStageSkill(t, pendingDir, "good",
		"---\nname: good\ndescription: ok\ntype: instruction\n---\nbody\n")

	got, err := ListStage(pendingDir, filepath.Join(root, "archived"), StagePending)
	if err != nil {
		t.Fatalf("ListStage: %v", err)
	}
	if len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("want only [good], got %+v", got)
	}
}
