package tools

import (
	"fmt"
	"path"
	"strings"

	"github.com/aura/aura/internal/skills"
	"github.com/aura/aura/internal/wiki"
)

func validateWorkspaceWrite(rel string, content []byte) (string, error) {
	clean := path.Clean(strings.ReplaceAll(strings.TrimSpace(rel), "\\", "/"))
	if isWorkspaceWikiPage(clean) {
		if err := validateWorkspaceWikiPage(clean, content); err != nil {
			return "", err
		}
		return "wiki", nil
	}
	if isWorkspaceSkillFile(clean) {
		if err := validateWorkspaceSkillFile(clean, content); err != nil {
			return "", err
		}
		return "skill", nil
	}
	return "", nil
}

func isWorkspaceWikiPage(rel string) bool {
	if !strings.HasPrefix(rel, "wiki/") || path.Ext(rel) != ".md" {
		return false
	}
	switch rel {
	case "wiki/SCHEMA.md", "wiki/index.md", "wiki/log.md":
		return false
	default:
		return true
	}
}

func validateWorkspaceWikiPage(rel string, content []byte) error {
	page, err := wiki.ParseMD(content)
	if err != nil {
		return fmt.Errorf("wiki validation failed: %w", err)
	}
	if err := wiki.Validate(page); err != nil {
		return fmt.Errorf("wiki validation failed: %w", err)
	}
	want := wiki.Slug(page.Title) + ".md"
	if got := path.Base(rel); got != want {
		return fmt.Errorf("wiki validation failed: filename %q does not match title slug %q", got, want)
	}
	return nil
}

func isWorkspaceSkillFile(rel string) bool {
	return path.Base(rel) == "SKILL.md" && path.Base(path.Dir(rel)) != "."
}

func validateWorkspaceSkillFile(rel string, content []byte) error {
	skill, err := skills.ParseSkill(content)
	if err != nil {
		return fmt.Errorf("skill validation failed: %w", err)
	}
	dirName := path.Base(path.Dir(rel))
	if skill.Name != dirName {
		return fmt.Errorf("skill validation failed: SKILL.md name %q does not match directory %q", skill.Name, dirName)
	}
	return nil
}
