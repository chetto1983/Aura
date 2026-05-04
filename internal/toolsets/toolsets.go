package toolsets

import (
	"fmt"
	"strings"
)

const (
	ProfileMemoryRead    = "memory_read"
	ProfileWikiReview    = "wiki_review"
	ProfileSkillsRead    = "skills_read"
	ProfileWebResearch   = "web_research"
	ProfileSandboxCode   = "sandbox_code"
	ProfileSchedulerSafe = "scheduler_safe"
)

var profiles = map[string][]string{
	ProfileMemoryRead: {
		"list_wiki",
		"read_wiki",
		"search_memory",
		"search_wiki",
		"list_sources",
		"read_source",
	},
	ProfileWikiReview: {
		"lint_wiki",
		"list_wiki",
		"read_wiki",
		"search_memory",
		"lint_sources",
		"list_sources",
	},
	ProfileSkillsRead: {
		"list_skills",
		"read_skill",
		"search_skill_catalog",
	},
	ProfileWebResearch: {
		"web_search",
		"web_fetch",
	},
	ProfileSandboxCode: {
		"execute_code",
		"list_tools",
		"read_tool",
	},
	ProfileSchedulerSafe: {
		"list_wiki",
		"read_wiki",
		"search_memory",
		"search_wiki",
		"list_sources",
		"read_source",
		"lint_sources",
		"web_search",
		"web_fetch",
		"propose_wiki_change",
		"propose_skill_change",
	},
}

var rolePresets = map[string][]string{
	"librarian": {
		"list_wiki",
		"read_wiki",
		"search_memory",
		"search_wiki",
		"lint_wiki",
		"list_sources",
		"read_source",
		"lint_sources",
	},
	"critic": {
		"lint_wiki",
		"list_wiki",
		"read_wiki",
		"search_memory",
		"lint_sources",
		"list_sources",
	},
	"researcher": {
		"web_search",
		"web_fetch",
	},
	"skillsmith": {
		"list_skills",
		"read_skill",
		"search_skill_catalog",
	},
	"synthesizer": {
		"list_wiki",
		"read_wiki",
		"search_memory",
		"search_wiki",
		"list_sources",
		"read_source",
	},
}

func Profile(name string) ([]string, bool) {
	tools, ok := profiles[strings.TrimSpace(name)]
	if !ok {
		return nil, false
	}
	return cloneStrings(tools), true
}

func ResolveProfiles(names ...string) ([]string, error) {
	out := []string{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tools, ok := profiles[name]
		if !ok {
			return nil, fmt.Errorf("unknown toolset profile %q", name)
		}
		out = appendUnique(out, tools...)
	}
	return out, nil
}

func MustResolveProfiles(names ...string) []string {
	tools, err := ResolveProfiles(names...)
	if err != nil {
		panic(err)
	}
	return tools
}

func SchedulerSafeTools() []string {
	return cloneStrings(profiles[ProfileSchedulerSafe])
}

func FilterAllowed(requested []string, allowed []string) []string {
	allowedSet := make(map[string]bool, len(allowed))
	for _, tool := range allowed {
		tool = strings.TrimSpace(tool)
		if tool != "" {
			allowedSet[tool] = true
		}
	}
	out := make([]string, 0, len(requested))
	seen := make(map[string]bool, len(requested))
	for _, tool := range requested {
		tool = strings.TrimSpace(tool)
		if tool == "" || seen[tool] || !allowedSet[tool] {
			continue
		}
		seen[tool] = true
		out = append(out, tool)
	}
	return out
}

func RoleTools(role string) ([]string, bool) {
	tools, ok := rolePresets[strings.ToLower(strings.TrimSpace(role))]
	if !ok {
		return nil, false
	}
	return cloneStrings(tools), true
}

func RolePresets() map[string][]string {
	out := make(map[string][]string, len(rolePresets))
	for role, tools := range rolePresets {
		out[role] = cloneStrings(tools)
	}
	return out
}

func appendUnique(out []string, values ...string) []string {
	seen := make(map[string]bool, len(out)+len(values))
	for _, value := range out {
		seen[value] = true
	}
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}

func cloneStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, len(values))
	copy(out, values)
	return out
}
