package skills

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	// maxManifestDescChars caps a single skill's description in the
	// system-prompt manifest. The description is the routing signal
	// (Anthropic's skill format embeds TRIGGER/SKIP rules there) so we
	// don't want to truncate it aggressively, but a runaway description
	// shouldn't crowd out the rest of the manifest either.
	maxManifestDescChars = 1500
	// maxSkillsBlockChars bounds the entire manifest. At 8 KiB this
	// fits ~30 skills with full descriptions before truncation kicks in.
	maxSkillsBlockChars = 8000
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
//
// LoadAll caches its result for cacheTTL to avoid re-reading every
// SKILL.md on every conversation turn. Skill files only change when
// admin install/delete runs, which is rare; a sub-second TTL means
// dashboard mutations reflect on the very next turn.
type Loader struct {
	dirs []string

	cacheMu  sync.RWMutex
	cached   []Skill
	cachedAt time.Time
}

// cacheTTL bounds how long a stale skill manifest can be served. 1s is
// short enough that admin operations feel instantaneous in interactive
// use, long enough that back-to-back chat turns hit the cache.
const cacheTTL = 1 * time.Second

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

// Invalidate clears the LoadAll cache. Call after admin install/delete
// when you need the next LoadAll to reflect the change immediately
// instead of after the cacheTTL window.
func (l *Loader) Invalidate() {
	l.cacheMu.Lock()
	l.cached = nil
	l.cachedAt = time.Time{}
	l.cacheMu.Unlock()
}

// LoadAll loads every valid skill across all roots. Invalid skill
// folders are skipped so one broken local draft cannot remove all
// skills from the prompt. When the same skill name appears in two
// roots, the first one wins (matching LoadByName precedence).
//
// Results are memoized for cacheTTL — Aura's hot path (handleConversation
// on every Telegram message) was re-reading and re-parsing every
// SKILL.md per turn even though skills only change on admin actions.
func (l *Loader) LoadAll() ([]Skill, error) {
	l.cacheMu.RLock()
	if l.cached != nil && time.Since(l.cachedAt) < cacheTTL {
		out := append([]Skill(nil), l.cached...)
		l.cacheMu.RUnlock()
		return out, nil
	}
	l.cacheMu.RUnlock()

	loaded, err := l.loadAllUncached()
	if err != nil {
		return nil, err
	}

	l.cacheMu.Lock()
	l.cached = append([]Skill(nil), loaded...)
	l.cachedAt = time.Now()
	l.cacheMu.Unlock()

	return loaded, nil
}

func (l *Loader) loadAllUncached() ([]Skill, error) {
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

// PromptBlock renders a manifest of loaded skills for the system
// prompt. Anthropic's skill format is built around progressive
// disclosure: the description carries the trigger conditions, the
// body carries the full instructions. Dumping every body into every
// turn's system prompt — what Picobot and earlier Aura did — wastes
// 20+ KiB of context per turn even on small-talk turns where no skill
// applies.
//
// Instead we emit a tiny manifest (`- name — description`) plus a
// directive telling the LLM to call `read_skill(name)` when a
// description matches before acting on its instructions. The body
// only enters the conversation context on turns that actually need
// it, and stays cached for the remainder of the tool loop.
func PromptBlock(loaded []Skill) string {
	if len(loaded) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("## Available Skills\n\n")
	sb.WriteString("Aura has the local skills listed below. Each entry's description states when it applies. ")
	sb.WriteString("Before following a skill's guidance, call the `read_skill` tool with the skill name to load its full instructions, then act on them. ")
	sb.WriteString("Skip skills whose description does not match the user's request.\n\n")
	for _, skill := range loaded {
		if skill.Name == "" {
			continue
		}
		desc := strings.TrimSpace(skill.Description)
		if desc == "" {
			desc = "(no description)"
		} else if len(desc) > maxManifestDescChars {
			desc = desc[:maxManifestDescChars] + "…[truncated]"
		}
		fmt.Fprintf(&sb, "- **%s** — %s\n", skill.Name, desc)
		if sb.Len() > maxSkillsBlockChars {
			return sb.String()[:maxSkillsBlockChars] + "\n[manifest truncated]"
		}
	}
	return strings.TrimSpace(sb.String())
}
