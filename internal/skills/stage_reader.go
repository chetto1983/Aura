package skills

import (
	"fmt"
	"os"
	"path/filepath"
)

// stage_reader.go is the GOV-02 metadata reader for the skills board's archived tab.
// It is DELIBERATELY separate from the Loader: the Loader only scans the active roots
// and is the one path that mounts a body into LLM context (loader.go loadSkillDir).
// This reader parses frontmatter for display ONLY and NEVER returns or mounts a body —
// an archived skill cannot cross into context by construction (T-28-01-03 / GOV-02
// prohibition #1). The board renders StageSkill metadata; the body stays on disk.
//
// Archived is the only stage left to read: amendment #97 deleted the pending one.

// StageArchived is the wire value of the archived tab's ?stage= query parameter.
const StageArchived = "archived"

// StageSkill is the display metadata for one archived skill — never its body.
// ContentHash is Aura's canonical content hash over the skill's files (the same hash
// recorded on audit rows); empty when the dir cannot be hashed (logged-skip upstream).
type StageSkill struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Type        string `json:"type"`
	Language    string `json:"language,omitempty"`
	ContentHash string `json:"content_hash,omitempty"`
}

// ListArchived lists the metadata of every archived skill. It os.ReadDir's the archive
// dir, parses each <name>/SKILL.md via parseFrontmatter, and projects display metadata +
// a content hash. A non-existent archive dir is an empty list (not an error — a fresh
// install has archived nothing). A malformed or symlinked entry is skipped (never fatal),
// mirroring the loader's scan safety. It NEVER calls the loader mount path, so an
// archived body never enters an LLM context.
func ListArchived(dir string) ([]StageSkill, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []StageSkill{}, nil
		}
		return nil, fmt.Errorf("read archive dir %q: %w", dir, err)
	}

	out := make([]StageSkill, 0, len(entries))
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		// Symlink strip (loader.go Pitfall 4): Lstat does NOT follow — a symlinked
		// entry is skipped so it cannot redirect the read outside the stage dir.
		info, lerr := os.Lstat(full)
		if lerr != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			continue
		}
		skill, ok := readStageSkill(full)
		if ok {
			out = append(out, skill)
		}
	}
	return out, nil
}

// readStageSkill parses <dir>/SKILL.md into display metadata + a content hash. A
// missing/symlinked/malformed SKILL.md yields ok=false (skip, never fatal — a dir
// without a parseable SKILL.md is not a skill). The body is read only to parse the
// frontmatter and compute the hash; it is NEVER returned (GOV-02 prohibition #1).
func readStageSkill(dir string) (StageSkill, bool) {
	path := filepath.Join(dir, "SKILL.md")
	info, lerr := os.Lstat(path)
	if lerr != nil || info.Mode()&os.ModeSymlink != 0 {
		return StageSkill{}, false
	}
	raw, err := os.ReadFile(path) // #nosec G304 -- path is <operator stage dir>/<dir>/SKILL.md, symlink-stripped above
	if err != nil {
		return StageSkill{}, false
	}
	fm, _, err := parseFrontmatter(raw)
	if err != nil || fm.Name == "" {
		return StageSkill{}, false
	}
	skill := StageSkill{
		Name:        fm.Name,
		Description: fm.Description,
		Type:        fm.Type,
		Language:    fm.Language,
	}
	if hash, herr := HashSkillDir(dir); herr == nil {
		skill.ContentHash = hash
	}
	return skill, true
}
