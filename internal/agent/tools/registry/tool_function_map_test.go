package tools

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/aura/aura/internal/storage/memoryindex"
)

func TestToolFunctionMapMatchesRuntimeAllowlist(t *testing.T) {
	root := repoRootForToolMap(t)
	doc := readToolFunctionMap(t, root)
	allowlist := runtimeToolAllowlist(t, root)
	expected := append([]string(nil), allowlist...)
	expected = append(expected, "delegate_*")
	sort.Strings(expected)

	got := toolMapHeadings(doc)
	sort.Strings(got)

	if strings.Join(got, "\n") != strings.Join(expected, "\n") {
		t.Fatalf("tool map headings do not match active runtime tool surface\nwant:\n%s\n\ngot:\n%s", strings.Join(expected, "\n"), strings.Join(got, "\n"))
	}

	for _, retired := range []string{
		"execute_code",
		"execute_shell",
		"doc",
		"dev_tool",
		"daily_briefing",
		"subagent_dispatch",
		"read_tool_result",
		"request_dashboard_token",
		"recall_operational",
		"recall_user_memory",
		"recall_god_nodes",
		"wiki_subgraph",
		"wiki_path",
		"ask_user_clarification",
	} {
		if sectionForTool(doc, retired) != "" {
			t.Fatalf("retired tool %q has an active map section", retired)
		}
	}
}

func TestToolFunctionMapDocumentsActionEnums(t *testing.T) {
	root := repoRootForToolMap(t)
	doc := readToolFunctionMap(t, root)
	cases := []struct {
		tool    string
		actions []string
	}{
		{tool: "ask_user", actions: []string{"clarification", "approval", "choice"}},
		{tool: "search", actions: searchToolValidActions},
		{tool: "web", actions: webValidActions},
		{tool: "agent_note", actions: agentNoteActions},
		{tool: "wiki_page", actions: wikiValidActions},
		{tool: "source", actions: sourceValidActions},
		{tool: "create_document", actions: []string{"pdf", "xlsx", "docx"}},
		{tool: "skill", actions: skillActions},
		{tool: "task", actions: taskValidActions},
		{tool: "propose_patch", actions: proposePatchActions},
		{tool: "file", actions: fileValidActions},
	}

	for _, tc := range cases {
		section := sectionForTool(doc, tc.tool)
		if section == "" {
			t.Fatalf("missing section for %q", tc.tool)
		}
		got := actionTokens(section)
		if strings.Join(got, ",") != strings.Join(tc.actions, ",") {
			t.Fatalf("%s actions mismatch\nwant: %v\n got: %v", tc.tool, tc.actions, got)
		}
	}
}

func TestToolFunctionMapDocumentsMemoryCollections(t *testing.T) {
	root := repoRootForToolMap(t)
	doc := readToolFunctionMap(t, root)
	for collection := range memoryindex.Registry {
		name := string(collection)
		if !strings.Contains(doc, "`"+name+"`") && !strings.Contains(doc, "'"+name+"'") {
			t.Fatalf("tool map does not document memory collection %q", name)
		}
	}
	for _, required := range []string{
		"conversation_compactions",
		"agent_notes",
		"proposed_updates",
		"projection_state",
		"/workspace/wiki",
		"/workspace/sources",
		"/workspace/skills",
		"/workspace/mcp.json",
	} {
		if !strings.Contains(doc, required) {
			t.Fatalf("tool map missing memory/graph location %q", required)
		}
	}
}

func repoRootForToolMap(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	dir := filepath.Dir(file)
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("go.mod not found while locating repo root")
		}
		dir = next
	}
}

func readToolFunctionMap(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "docs", "tool-and-function-map.md"))
	if err != nil {
		t.Fatalf("read tool map: %v", err)
	}
	return string(data)
}

func runtimeToolAllowlist(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "compose.yaml"))
	if err != nil {
		t.Fatalf("read compose.yaml: %v", err)
	}
	re := regexp.MustCompile(`AURA_TOOL_ALLOWLIST:\s*"\$\{AURA_TOOL_ALLOWLIST:-([^}]*)\}"`)
	match := re.FindStringSubmatch(string(data))
	if len(match) != 2 {
		t.Fatal("compose.yaml AURA_TOOL_ALLOWLIST default not found")
	}
	var out []string
	for _, raw := range strings.Split(match[1], ",") {
		name := strings.TrimSpace(raw)
		if name != "" {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

func toolMapHeadings(doc string) []string {
	re := regexp.MustCompile("(?m)^### `([^`]+)`$")
	matches := re.FindAllStringSubmatch(doc, -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match[1])
	}
	return out
}

func sectionForTool(doc, tool string) string {
	header := "### `" + tool + "`"
	start := strings.Index(doc, header)
	if start < 0 {
		return ""
	}
	rest := doc[start+len(header):]
	end := strings.Index(rest, "\n### `")
	if end < 0 {
		end = strings.Index(rest, "\n## ")
	}
	if end < 0 {
		return rest
	}
	return rest[:end]
}

func actionTokens(section string) []string {
	idx := strings.Index(section, "- Actions:")
	if idx < 0 {
		return nil
	}
	lines := strings.Split(section[idx:], "\n")
	var block strings.Builder
	for i, line := range lines {
		if i > 0 && strings.HasPrefix(line, "- ") {
			break
		}
		block.WriteString(line)
		block.WriteByte('\n')
	}
	re := regexp.MustCompile("`([^`]+)`")
	matches := re.FindAllStringSubmatch(block.String(), -1)
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		out = append(out, match[1])
	}
	return out
}
