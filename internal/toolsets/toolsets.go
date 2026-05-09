package toolsets

import (
	"fmt"
	"strings"
)

const (
	ToolsetMemoryRead    = "memory_read"
	ToolsetWikiReview    = "wiki_review"
	ToolsetSkillsRead    = "skills_read"
	ToolsetWebResearch   = "web_research"
	ToolsetSandboxCode   = "sandbox_code"
	ToolsetSchedulerSafe = "scheduler_safe"
)

var toolsets = map[string][]string{
	ToolsetMemoryRead: {
		"search_memory",
		"list_files",
		"read_file",
		"search_files",
		"list_sources",
		"read_source",
	},
	ToolsetWikiReview: {
		"search_memory",
		"list_files",
		"read_file",
		"search_files",
		"lint_sources",
		"list_sources",
	},
	ToolsetSkillsRead: {
		"list_files",
		"read_file",
		"search_files",
	},
	ToolsetWebResearch: {
		"web_search",
		"web_fetch",
	},
	ToolsetSandboxCode: {
		"execute_code",
		"execute_shell",
		"list_tools",
		"read_tool",
	},
	ToolsetSchedulerSafe: {
		"search_memory",
		"list_files",
		"read_file",
		"search_files",
		"list_sources",
		"read_source",
		"lint_sources",
		"web_search",
		"web_fetch",
	},
}

var rolePresets = map[string][]string{
	"librarian": {
		"search_memory",
		"list_files",
		"read_file",
		"search_files",
		"list_sources",
		"read_source",
		"lint_sources",
	},
	"critic": {
		"search_memory",
		"list_files",
		"read_file",
		"search_files",
		"lint_sources",
		"list_sources",
	},
	"researcher": {
		"web_search",
		"web_fetch",
	},
	"skillsmith": {
		"list_files",
		"read_file",
		"search_files",
	},
	"synthesizer": {
		"search_memory",
		"list_files",
		"read_file",
		"search_files",
		"list_sources",
		"read_source",
	},
}

func Toolset(name string) ([]string, bool) {
	tools, ok := toolsets[strings.TrimSpace(name)]
	if !ok {
		return nil, false
	}
	return cloneStrings(tools), true
}

func ResolveToolsets(names ...string) ([]string, error) {
	out := []string{}
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		tools, ok := toolsets[name]
		if !ok {
			return nil, fmt.Errorf("unknown toolset %q", name)
		}
		out = appendUnique(out, tools...)
	}
	return out, nil
}

func SchedulerSafeTools() []string {
	return cloneStrings(toolsets[ToolsetSchedulerSafe])
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
