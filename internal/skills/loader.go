package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	maxSkillPromptChars = 4000
	maxSkillsBlockChars = 12000
)

var skillNameRE = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// Skill is a local SKILL.md instruction package.
type Skill struct {
	Name        string
	Description string
	Content     string
}

// Loader reads local skills from one or more configured directories.
// Multiple roots let hand-written skills (under SKILLS_PATH, e.g.
// ./skills) coexist with catalog-installed skills, which the skills.sh
// CLI writes to <project>/.claude/skills/. Earlier dirs win when the
// same skill name appears in two roots.
type Loader struct {
	dirs []string
}

// NewLoader creates a loader rooted at dir plus any extra search
// paths. Empty strings are skipped; an empty primary dir falls back to
// "./skills". The order matters: LoadByName / LoadAll prefer the first
// directory that contains a given skill name.
func NewLoader(dir string, extra ...string) *Loader {
	roots := make([]string, 0, 1+len(extra))
	primary := strings.TrimSpace(dir)
	if primary == "" {
		primary = "./skills"
	}
	roots = append(roots, primary)
	for _, p := range extra {
		if t := strings.TrimSpace(p); t != "" && t != primary {
			roots = append(roots, t)
		}
	}
	return &Loader{dirs: roots}
}

// Dirs returns the configured search roots in priority order. Used by
// the deleter so it can resolve a skill name to the same root the
// loader read it from.
func (l *Loader) Dirs() []string { return append([]string(nil), l.dirs...) }

// LoadAll loads every valid skill across all roots. Invalid skill
// folders are skipped so one broken local draft cannot remove all
// skills from the prompt. When the same skill name appears in two
// roots, the first one wins (matching LoadByName precedence).
func (l *Loader) LoadAll() ([]Skill, error) {
	seen := make(map[string]struct{})
	loaded := make([]Skill, 0)
	for _, dir := range l.dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			skill, err := loadPathFile(filepath.Join(dir, entry.Name(), "SKILL.md"))
			if err != nil {
				continue
			}
			if _, dup := seen[skill.Name]; dup {
				continue
			}
			seen[skill.Name] = struct{}{}
			loaded = append(loaded, skill)
		}
	}
	sort.Slice(loaded, func(i, j int) bool {
		return loaded[i].Name < loaded[j].Name
	})
	return loaded, nil
}

// LoadByName reads one skill by directory name, returning the first
// match in root-priority order.
func (l *Loader) LoadByName(name string) (Skill, error) {
	name = strings.TrimSpace(name)
	if !skillNameRE.MatchString(name) {
		return Skill{}, fmt.Errorf("invalid skill name %q", name)
	}
	var lastErr error
	for _, dir := range l.dirs {
		skill, err := loadPathFile(filepath.Join(dir, name, "SKILL.md"))
		if err == nil {
			return skill, nil
		}
		if !os.IsNotExist(err) {
			lastErr = err
		}
	}
	if lastErr != nil {
		return Skill{}, lastErr
	}
	return Skill{}, fmt.Errorf("skill %q: %w", name, os.ErrNotExist)
}

func loadPathFile(path string) (Skill, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Skill{}, err
	}
	return parseSkill(data)
}

func parseSkill(data []byte) (Skill, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if len(lines) < 3 || strings.TrimSpace(lines[0]) != "---" {
		return Skill{}, fmt.Errorf("invalid SKILL.md: missing frontmatter")
	}

	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			end = i
			break
		}
	}
	if end == -1 {
		return Skill{}, fmt.Errorf("invalid SKILL.md: unterminated frontmatter")
	}

	var meta struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal([]byte(strings.Join(lines[1:end], "\n")), &meta); err != nil {
		return Skill{}, fmt.Errorf("invalid SKILL.md frontmatter: %w", err)
	}
	meta.Name = strings.TrimSpace(meta.Name)
	meta.Description = strings.TrimSpace(meta.Description)
	if meta.Name == "" {
		return Skill{}, fmt.Errorf("invalid SKILL.md: missing name")
	}
	if !skillNameRE.MatchString(meta.Name) {
		return Skill{}, fmt.Errorf("invalid SKILL.md: invalid name %q", meta.Name)
	}

	return Skill{
		Name:        meta.Name,
		Description: meta.Description,
		Content:     strings.TrimSpace(strings.Join(lines[end+1:], "\n")),
	}, nil
}

// PromptBlock renders loaded skills for the system prompt.
func PromptBlock(loaded []Skill) string {
	if len(loaded) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Skills\n\n")
	sb.WriteString("These are local user-authored operating instructions. Apply the relevant skill when the user's request matches its description.\n")
	for _, skill := range loaded {
		if skill.Name == "" {
			continue
		}
		fmt.Fprintf(&sb, "\n### %s\n", skill.Name)
		if skill.Description != "" {
			fmt.Fprintf(&sb, "Description: %s\n\n", skill.Description)
		}
		content := skill.Content
		if len(content) > maxSkillPromptChars {
			content = content[:maxSkillPromptChars] + "\n[skill content truncated]"
		}
		sb.WriteString(content)
		sb.WriteString("\n")
		if sb.Len() > maxSkillsBlockChars {
			out := sb.String()
			return out[:maxSkillsBlockChars] + "\n[skills block truncated]"
		}
	}
	return strings.TrimSpace(sb.String())
}
